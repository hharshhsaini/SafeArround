package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/routes"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	customWS "github.com/obsidiannnn/Safe-Around/backend/internal/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrimeSocketRequiresAuthentication(t *testing.T) {
	t.Setenv("JWT_SECRET", "websocket-test-secret")
	gin.SetMode(gin.TestMode)

	hub := customWS.NewCrimeHub(nil)
	router := routes.SetupRouter(nil, nil, nil, nil, nil, handlers.HandleWebSocket(hub), nil, nil, nil, nil, nil, nil, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()
	socketURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/crime"

	_, resp, err := websocket.DefaultDialer.Dial(socketURL, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	_, resp, err = websocket.DefaultDialer.Dial(socketURL, http.Header{"Authorization": {"Bearer not-a-real-token"}})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	token, err := utils.GenerateToken(42, "")
	require.NoError(t, err)
	conn, _, err := websocket.DefaultDialer.Dial(socketURL, http.Header{"Authorization": {"Bearer " + token}})
	require.NoError(t, err)
	defer conn.Close()

	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var welcome struct {
		Event string `json:"event"`
	}
	require.NoError(t, json.Unmarshal(raw, &welcome))
	assert.Equal(t, "connected", welcome.Event)
}
