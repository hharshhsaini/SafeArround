package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	customWS "github.com/obsidiannnn/Safe-Around/backend/internal/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Origin is not the security boundary here: the route requires a
		// bearer token, which browsers cannot attach to cross-site sockets.
		return true
	},
}

// HandleWebSocket must be mounted behind middleware.AuthRequired so every
// connection is tied to a user and only receives events meant for them.
func HandleWebSocket(hub *customWS.CrimeHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		userID := userIDVal.(uint)

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}

		hub.Serve(conn, userID)
	}
}
