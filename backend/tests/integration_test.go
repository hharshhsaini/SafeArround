package tests

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obsidiannnn/Safe-Around/backend/pkg/fcm"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/maps"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServiceAccount returns a Firebase service-account key whose OAuth token
// endpoint is tokenURL.
func fakeServiceAccount(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	raw, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "safearound-test",
		"private_key_id": "test-key",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   "push@safearound-test.iam.gserviceaccount.com",
		"token_uri":      tokenURL,
	})
	require.NoError(t, err)
	return string(raw)
}

func TestFCMHTTPv1(t *testing.T) {
	type sentMessage struct {
		Message struct {
			Token   string `json:"token"`
			Android struct {
				Priority     string `json:"priority"`
				Notification struct {
					ChannelID string `json:"channel_id"`
				} `json:"notification"`
			} `json:"android"`
		} `json:"message"`
	}
	var lastSent sentMessage

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.FormValue("grant_type"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&lastSent))
		if lastSent.Message.Token == "stale-token" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`))
			return
		}
		w.Write([]byte(`{"name":"projects/safearound-test/messages/123"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := fcm.NewClient(fakeServiceAccount(t, srv.URL+"/token"))
	require.NoError(t, err)
	client.OverrideBaseURL(srv.URL + "/send")

	msgID, err := client.SendNotification("fcm-device-token", "Help needed", "Someone near you needs help", map[string]string{"category": "EMERGENCY_ALERT"})
	require.NoError(t, err)
	assert.Equal(t, "projects/safearound-test/messages/123", msgID)
	assert.Equal(t, "fcm-device-token", lastSent.Message.Token)
	assert.Equal(t, "HIGH", lastSent.Message.Android.Priority)
	assert.Equal(t, "emergency-alerts", lastSent.Message.Android.Notification.ChannelID)

	_, err = client.SendNotification("stale-token", "Help needed", "Someone near you needs help", nil)
	assert.ErrorIs(t, err, fcm.ErrUndeliverable)
	assert.Contains(t, err.Error(), "UNREGISTERED")
}

func TestFCMWithoutServiceAccount(t *testing.T) {
	client, err := fcm.NewClient("")
	require.NoError(t, err)

	_, err = client.SendNotification("fcm-device-token", "Title", "Body", nil)
	assert.ErrorIs(t, err, fcm.ErrUndeliverable)
}

func TestFCMRejectsKeysThatAreNotServiceAccounts(t *testing.T) {
	_, err := fcm.NewClient(`{"type":"authorized_user","client_id":"x","client_secret":"y","refresh_token":"z"}`)
	assert.Error(t, err)
}

func TestExpoPushTokensGoThroughExpo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			To string `json:"to"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if req.To == "ExponentPushToken[gone]" {
			w.Write([]byte(`{"data":{"status":"error","message":"not a registered push token","details":{"error":"DeviceNotRegistered"}}}`))
			return
		}
		w.Write([]byte(`{"data":{"status":"ok","id":"ticket-1"}}`))
	}))
	defer srv.Close()

	// Expo delivery needs no FCM credentials.
	client, err := fcm.NewClient("")
	require.NoError(t, err)
	client.OverrideExpoURL(srv.URL)

	ticketID, err := client.SendNotification("ExponentPushToken[abc]", "Title", "Body", nil)
	require.NoError(t, err)
	assert.Equal(t, "ticket-1", ticketID)

	_, err = client.SendNotification("ExponentPushToken[gone]", "Title", "Body", nil)
	assert.ErrorIs(t, err, fcm.ErrUndeliverable)
}

func TestTwilioIntegration(t *testing.T) {
	client := twilio.NewClient("AC_MOCK_SID", "MOCK_TOKEN")
	assert.NotNil(t, client)
}

func TestGeocodingIntegration(t *testing.T) {
	client := maps.NewClient("AIzaMockKey")
	assert.NotNil(t, client)
}

