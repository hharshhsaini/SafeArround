package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
)

var crimeHubWelcome = []byte(`{"event":"connected","message":"Connected to SafeAround real-time updates"}`)

// WebSocketMessage is the envelope of every event sent to clients.
type WebSocketMessage struct {
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
}

// AlertBroadcaster pushes SOS events over WebSockets. These payloads include a
// victim's location, so implementations must deliver each event only to the
// users passed in, never to every connection.
type AlertBroadcaster interface {
	BroadcastEmergencyAlert(alert *models.EmergencyAlert, recipientUserIDs []uint)
	BroadcastResponderAccepted(alertID uuid.UUID, response *models.AlertResponse, targetUserID uint)
	BroadcastRadiusExpanded(alertID uuid.UUID, oldRadius, newRadius, usersNotified int, recipientUserIDs []uint)
	CloseRoom(roomID string, reason string, participantUserIDs []uint)
}

const (
	// Time allowed to write a message to a client.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong from a client.
	pongWait = 30 * time.Second

	// How often to ping clients. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// clientSendBuffer is how many messages may wait for one client. It is
	// sized to absorb a crime-pipeline batch (one notification per inserted
	// row); a client that falls this far behind is disconnected rather than
	// slowing others. Payloads are shared, so each slot costs only a pointer.
	clientSendBuffer = 512

	// maxClientMessageBytes caps what clients may send; the app sends nothing.
	maxClientMessageBytes = 4096

	maxListenBackoff = 30 * time.Second
)

// crimeClient is an authenticated connection, the user it belongs to, and its
// queue of messages waiting to be written.
type crimeClient struct {
	conn   *websocket.Conn
	userID uint
	send   chan []byte
}

// outboundMessage is a payload waiting to be written. A nil recipients set
// means every connected client; otherwise only the listed users receive it.
type outboundMessage struct {
	payload    []byte
	recipients map[uint]struct{}
}

type CrimeHub struct {
	db *pgxpool.Pool
	// clients is owned by Run; no other goroutine may touch it. Each client's
	// writePump is the only goroutine that writes to its connection.
	clients    map[*crimeClient]struct{}
	broadcast  chan outboundMessage
	register   chan *crimeClient
	unregister chan *crimeClient
}

func NewCrimeHub(db *pgxpool.Pool) *CrimeHub {
	hub := &CrimeHub{
		db:         db,
		clients:    make(map[*crimeClient]struct{}),
		broadcast:  make(chan outboundMessage, 256),
		register:   make(chan *crimeClient),
		unregister: make(chan *crimeClient),
	}

	go hub.Run()
	if db != nil {
		go hub.ListenPostgres()
	}

	return hub
}

// Run routes messages to clients. It never blocks on a connection: messages
// are queued per client, and a client whose queue is full is dropped.
func (h *CrimeHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = struct{}{}
			client.send <- crimeHubWelcome
			log.Printf("Client connected. Total: %d", len(h.clients))

		case client := <-h.unregister:
			h.remove(client)
			log.Printf("Client disconnected. Total: %d", len(h.clients))

		case message := <-h.broadcast:
			for client := range h.clients {
				if message.recipients != nil {
					if _, ok := message.recipients[client.userID]; !ok {
						continue
					}
				}
				select {
				case client.send <- message.payload:
				default:
					log.Printf("Dropping websocket client for user %d: too far behind", client.userID)
					h.remove(client)
				}
			}
		}
	}
}

// remove forgets a client and closes its queue, which makes its writePump
// close the connection. It is safe to call more than once.
func (h *CrimeHub) remove(client *crimeClient) {
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.send)
	}
}

// Serve takes over an authenticated connection: it registers it for userID and
// starts the goroutines that read from and write to it.
func (h *CrimeHub) Serve(conn *websocket.Conn, userID uint) {
	client := &crimeClient{conn: conn, userID: userID, send: make(chan []byte, clientSendBuffer)}
	h.register <- client
	go h.writePump(client)
	go h.readPump(client)
}

// writePump writes queued messages and keep-alive pings. Every write has a
// deadline, so a dead peer is detected instead of blocking forever.
func (h *CrimeHub) writePump(client *crimeClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case payload, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				log.Printf("Write error: %v", err)
				return
			}

		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump keeps the read side open so pongs and close frames are processed,
// and unregisters the client once the peer goes away or stops answering pings.
func (h *CrimeHub) readPump(client *crimeClient) {
	defer func() {
		h.unregister <- client
	}()

	client.conn.SetReadLimit(maxClientMessageBytes)
	client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error {
		return client.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := client.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// ListenPostgres relays crime_channel notifications to every client. If the
// connection drops it reconnects with backoff, rather than exiting the process
// or spinning on a dead connection.
func (h *CrimeHub) ListenPostgres() {
	backoff := time.Second
	for {
		listened, err := h.listenOnce()
		if listened {
			backoff = time.Second
		}
		log.Printf("Crime listener stopped: %v; reconnecting in %s", err, backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxListenBackoff)
	}
}

// listenOnce listens on one dedicated connection until it fails. listened
// reports whether LISTEN succeeded before the failure.
func (h *CrimeHub) listenOnce() (listened bool, err error) {
	ctx := context.Background()
	pooled, err := h.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire connection: %w", err)
	}
	// Take the connection out of the pool so a LISTENing (or broken)
	// connection is never handed to other queries.
	conn := pooled.Hijack()
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "LISTEN crime_channel"); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}

	log.Println("✅ Listening for real-time crime updates on 'crime_channel'...")

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait for notification: %w", err)
		}

		log.Printf("📢 New crime: %s", notification.Payload)

		h.relayCrimeNotification(notification.Payload)
	}
}

// relayCrimeNotification forwards a crime_channel notification to every
// client. A single-incident event goes out only if it says the incident is
// verified, so unverified user reports never appear on other users' screens.
func (h *CrimeHub) relayCrimeNotification(payload string) {
	var event struct {
		Event    string `json:"event"`
		Verified *bool  `json:"verified"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		log.Printf("Ignoring malformed crime notification: %v", err)
		return
	}
	if event.Event == "new_crime" && (event.Verified == nil || !*event.Verified) {
		return
	}
	h.broadcastAll([]byte(payload))
}

// broadcastAll sends a payload to every connected client. Use it only for
// data every signed-in user may see, such as crime incidents.
func (h *CrimeHub) broadcastAll(payload []byte) {
	h.broadcast <- outboundMessage{payload: payload}
}

// sendEvent sends an event to the given users only. With no users it sends
// nothing; it never falls back to broadcasting.
func (h *CrimeHub) sendEvent(event string, data map[string]interface{}, userIDs []uint) {
	if len(userIDs) == 0 {
		return
	}

	payload, err := json.Marshal(WebSocketMessage{Event: event, Data: data})
	if err != nil {
		log.Printf("Failed to marshal websocket event %s: %v", event, err)
		return
	}

	recipients := make(map[uint]struct{}, len(userIDs))
	for _, id := range userIDs {
		recipients[id] = struct{}{}
	}
	h.broadcast <- outboundMessage{payload: payload, recipients: recipients}
}

func (h *CrimeHub) BroadcastEmergencyAlert(alert *models.EmergencyAlert, recipientUserIDs []uint) {
	h.sendEvent("emergency_alert", map[string]interface{}{
		"alert_id": alert.ID.String(),
		"user": map[string]interface{}{
			"full_name": "Distressed User",
			"user_id":   alert.UserID,
		},
		"location": map[string]float64{
			"latitude":  alert.AlertLocation.Latitude,
			"longitude": alert.AlertLocation.Longitude,
		},
		"distance":       0,
		"current_radius": alert.CurrentRadius,
		"created_at":     alert.CreatedAt,
	}, recipientUserIDs)
}

func (h *CrimeHub) BroadcastResponderAccepted(alertID uuid.UUID, response *models.AlertResponse, targetUserID uint) {
	h.sendEvent("responder_accepted", map[string]interface{}{
		"alert_id":       alertID.String(),
		"responder_id":   response.ResponderUserID,
		"distance":       response.DistanceMeters,
		"eta":            response.EstimatedArrivalMinutes,
		"responded_at":   response.RespondedAt,
		"target_user_id": strconv.FormatUint(uint64(targetUserID), 10),
	}, []uint{targetUserID})
}

func (h *CrimeHub) BroadcastRadiusExpanded(alertID uuid.UUID, oldRadius, newRadius, usersNotified int, recipientUserIDs []uint) {
	h.sendEvent("radius_expanded", map[string]interface{}{
		"alert_id":       alertID.String(),
		"old_radius":     oldRadius,
		"new_radius":     newRadius,
		"users_notified": usersNotified,
		"timestamp":      time.Now().UTC(),
	}, recipientUserIDs)
}

func (h *CrimeHub) CloseRoom(roomID string, reason string, participantUserIDs []uint) {
	h.sendEvent("room_closed", map[string]interface{}{
		"room_id": roomID,
		"reason":  reason,
	}, participantUserIDs)
}
