package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/stretchr/testify/require"
)

// startHubServer serves a socket endpoint that registers each connection
// under the user ID given in the "user" query parameter.
func startHubServer(t *testing.T, hub *CrimeHub) string {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.ParseUint(r.URL.Query().Get("user"), 10, 64)
		if err != nil {
			http.Error(w, "bad user", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub.Serve(conn, uint(userID))
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// connectUser dials the hub as userID and waits for the welcome message, so
// the connection is registered before the test sends anything.
func connectUser(t *testing.T, baseURL string, userID uint) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(baseURL+"?user="+strconv.FormatUint(uint64(userID), 10), nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.Equal(t, "connected", nextEvent(t, conn))
	return conn
}

func nextEvent(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var msg struct {
		Event string `json:"event"`
	}
	require.NoError(t, json.Unmarshal(raw, &msg))
	return msg.Event
}

// Each connection receives messages in the order the hub sent them, so a
// trailing public crime event proves which earlier events a user did not get.
const crimePayload = `{"event":"new_crime"}`

func TestCrimeHubDeliversAlertEventsOnlyToListedUsers(t *testing.T) {
	hub := NewCrimeHub(nil)
	baseURL := startHubServer(t, hub)

	victim := connectUser(t, baseURL, 1)
	recipient := connectUser(t, baseURL, 2)
	bystander := connectUser(t, baseURL, 3)

	alert := &models.EmergencyAlert{ID: uuid.New(), UserID: 1}
	hub.BroadcastEmergencyAlert(alert, []uint{2})
	hub.BroadcastResponderAccepted(alert.ID, &models.AlertResponse{ResponderUserID: 2}, 1)
	hub.CloseRoom("alert_"+alert.ID.String(), "resolved", []uint{1, 2})
	hub.broadcastAll([]byte(crimePayload))

	require.Equal(t, "responder_accepted", nextEvent(t, victim))
	require.Equal(t, "room_closed", nextEvent(t, victim))
	require.Equal(t, "new_crime", nextEvent(t, victim))

	require.Equal(t, "emergency_alert", nextEvent(t, recipient))
	require.Equal(t, "room_closed", nextEvent(t, recipient))
	require.Equal(t, "new_crime", nextEvent(t, recipient))

	require.Equal(t, "new_crime", nextEvent(t, bystander))
}

func TestCrimeHubKeepsDeliveringWhenAClientStopsReading(t *testing.T) {
	hub := NewCrimeHub(nil)
	baseURL := startHubServer(t, hub)

	connectUser(t, baseURL, 1) // never reads again after the welcome
	reader := connectUser(t, baseURL, 2)

	// Far more data than the stalled client's socket buffers and queue can
	// hold, sent in step with the reader so only the stalled client falls behind.
	payload := []byte(`{"event":"new_crime","padding":"` + strings.Repeat("x", 32*1024) + `"}`)
	const messages = 1500
	for i := 0; i < messages; i++ {
		hub.broadcastAll(payload)
		require.Equal(t, "new_crime", nextEvent(t, reader))
	}
}

func TestCrimeHubRelaysOnlyVerifiedIncidents(t *testing.T) {
	hub := NewCrimeHub(nil)
	baseURL := startHubServer(t, hub)
	user := connectUser(t, baseURL, 7)

	hub.relayCrimeNotification(`{"event":"new_crime","id":"a","verified":false}`)
	hub.relayCrimeNotification(`{"event":"new_crime","id":"b"}`) // trigger from before verified was added
	hub.relayCrimeNotification(`not json`)
	hub.relayCrimeNotification(`{"event":"crime_batch","count":3}`)
	hub.relayCrimeNotification(`{"event":"new_crime","id":"c","verified":true}`)

	require.Equal(t, "crime_batch", nextEvent(t, user))
	require.Equal(t, "new_crime", nextEvent(t, user))
}

func TestCrimeHubSendsAlertEventsToNobodyWhenNoRecipients(t *testing.T) {
	hub := NewCrimeHub(nil)
	baseURL := startHubServer(t, hub)

	user := connectUser(t, baseURL, 5)

	alert := &models.EmergencyAlert{ID: uuid.New(), UserID: 9}
	hub.BroadcastEmergencyAlert(alert, nil)
	hub.BroadcastRadiusExpanded(alert.ID, 100, 250, 0, []uint{})
	hub.CloseRoom("alert_"+alert.ID.String(), "cancelled", nil)
	hub.broadcastAll([]byte(crimePayload))

	require.Equal(t, "new_crime", nextEvent(t, user))
}
