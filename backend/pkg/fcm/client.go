package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ErrUndeliverable marks push failures that retrying won't fix: an unregistered
// or malformed device token, a rejected message, or FCM not being configured.
// Callers should fall back to another channel straight away.
var ErrUndeliverable = errors.New("push notification cannot be delivered")

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// Client sends push notifications. Expo push tokens (what the mobile app
// registers) go through Expo's push service; any other token is treated as an
// FCM registration token and sent with the FCM HTTP v1 API.
type Client struct {
	tokenSource oauth2.TokenSource // nil when no service account is configured
	sendURL     string
	expoURL     string
	httpClient  *http.Client
	cb          *gobreaker.CircuitBreaker
}

// NewClient builds a client from a Firebase service-account key (the JSON key
// file's contents). With an empty key, Expo pushes still work and FCM tokens
// fail with ErrUndeliverable.
func NewClient(serviceAccountJSON string) (*Client, error) {
	// Configure Circuit Breaker protecting FCM API
	cbSettings := gobreaker.Settings{
		Name:        "FCM_API",
		MaxRequests: 5,
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
	}

	c := &Client{
		expoURL:    "https://exp.host/--/api/v2/push/send",
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cb:         gobreaker.NewCircuitBreaker(cbSettings),
	}
	if strings.TrimSpace(serviceAccountJSON) == "" {
		return c, nil
	}

	// JWTConfigFromJSON only accepts service-account keys.
	jwtConfig, err := google.JWTConfigFromJSON([]byte(serviceAccountJSON), fcmScope)
	if err != nil {
		return nil, fmt.Errorf("read FCM service account: %w", err)
	}
	var key struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &key); err != nil || key.ProjectID == "" {
		return nil, errors.New("read FCM service account: project_id is missing")
	}

	// The token source caches each access token until shortly before it expires.
	c.tokenSource = jwtConfig.TokenSource(context.Background())
	c.sendURL = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", key.ProjectID)
	return c, nil
}

// OverrideBaseURL points FCM sends at a mock server for testing.
func (c *Client) OverrideBaseURL(url string) {
	c.sendURL = url
}

// OverrideExpoURL points Expo sends at a mock server for testing.
func (c *Client) OverrideExpoURL(url string) {
	c.expoURL = url
}

func (c *Client) SendNotification(token, title, body string, data map[string]string) (string, error) {
	if IsExpoPushToken(token) {
		return c.sendExpoNotification(token, title, body, data)
	}
	return c.sendFCMNotification(token, title, body, data)
}

// IsExpoPushToken reports whether token is an Expo push token rather than a
// raw FCM registration token.
func IsExpoPushToken(token string) bool {
	return strings.HasPrefix(token, "ExponentPushToken[") || strings.HasPrefix(token, "ExpoPushToken[")
}

func isEmergency(data map[string]string) bool {
	return strings.TrimSpace(data["category"]) == "EMERGENCY_ALERT"
}

// FCM HTTP v1 request body: https://firebase.google.com/docs/reference/fcm/rest/v1/projects.messages
type fcmSendRequest struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *fcmAndroidConfig `json:"android,omitempty"`
	APNS         *fcmAPNSConfig    `json:"apns,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmAndroidConfig struct {
	Priority     string                  `json:"priority"`
	Notification *fcmAndroidNotification `json:"notification,omitempty"`
}

type fcmAndroidNotification struct {
	ChannelID string `json:"channel_id,omitempty"`
	Sound     string `json:"sound,omitempty"`
}

type fcmAPNSConfig struct {
	Headers map[string]string      `json:"headers,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type fcmHTTPResult struct {
	status int
	body   []byte
}

func (c *Client) sendFCMNotification(token, title, body string, data map[string]string) (string, error) {
	if c.tokenSource == nil {
		return "", fmt.Errorf("%w: FCM is not configured (set FCM_SERVICE_ACCOUNT_JSON)", ErrUndeliverable)
	}

	// Same channels and priorities as the Expo path.
	channelID, androidPriority, apnsPriority := "general-alerts", "NORMAL", "5"
	aps := map[string]interface{}{"sound": "default"}
	if isEmergency(data) {
		channelID, androidPriority, apnsPriority = "emergency-alerts", "HIGH", "10"
		aps["category"] = "EMERGENCY_ALERT"
	}

	reqBody, err := json.Marshal(fcmSendRequest{Message: fcmMessage{
		Token:        token,
		Notification: &fcmNotification{Title: title, Body: body},
		Data:         data,
		Android: &fcmAndroidConfig{
			Priority:     androidPriority,
			Notification: &fcmAndroidNotification{ChannelID: channelID, Sound: "default"},
		},
		APNS: &fcmAPNSConfig{
			Headers: map[string]string{"apns-priority": apnsPriority},
			Payload: map[string]interface{}{"aps": aps},
		},
	}})
	if err != nil {
		return "", err
	}

	// Only outages count against the circuit breaker; a rejected token does not.
	result, err := c.cb.Execute(func() (interface{}, error) {
		accessToken, err := c.tokenSource.Token()
		if err != nil {
			return nil, fmt.Errorf("fetch FCM access token: %w", err)
		}

		req, err := http.NewRequest(http.MethodPost, c.sendURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken.AccessToken)
		req.Header.Set("Content-Type", "application/json")

		httpResp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer httpResp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
		if err != nil {
			return nil, err
		}
		if httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("fcm server error: %d", httpResp.StatusCode)
		}
		return &fcmHTTPResult{status: httpResp.StatusCode, body: respBody}, nil
	})
	if err != nil {
		return "", err
	}

	res := result.(*fcmHTTPResult)
	if res.status != http.StatusOK {
		return "", fmt.Errorf("%w: %s", ErrUndeliverable, fcmErrorMessage(res))
	}

	var sent struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(res.body, &sent); err != nil {
		return "", err
	}
	return sent.Name, nil
}

// fcmErrorMessage summarises an FCM v1 error response, e.g.
// "404 UNREGISTERED: Requested entity was not found."
func fcmErrorMessage(res *fcmHTTPResult) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.body, &errResp); err != nil {
		return fmt.Sprintf("fcm returned status %d", res.status)
	}

	code := errResp.Error.Status
	for _, detail := range errResp.Error.Details {
		if detail.ErrorCode != "" {
			code = detail.ErrorCode
			break
		}
	}
	return fmt.Sprintf("%d %s: %s", res.status, code, errResp.Error.Message)
}

type expoPushRequest struct {
	To         string            `json:"to"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	Data       map[string]string `json:"data,omitempty"`
	Sound      string            `json:"sound,omitempty"`
	Priority   string            `json:"priority,omitempty"`
	ChannelID  string            `json:"channelId,omitempty"`
	CategoryID string            `json:"categoryId,omitempty"`
}

type expoPushResponse struct {
	Data struct {
		Status  string `json:"status"`
		ID      string `json:"id"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) sendExpoNotification(token, title, body string, data map[string]string) (string, error) {
	channelID := "general-alerts"
	priority := "default"
	categoryID := ""

	if isEmergency(data) {
		channelID = "emergency-alerts"
		priority = "high"
		categoryID = "EMERGENCY_ALERT"
	}

	payload := expoPushRequest{
		To:         token,
		Title:      title,
		Body:       body,
		Data:       data,
		Sound:      "default",
		Priority:   priority,
		ChannelID:  channelID,
		CategoryID: categoryID,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", c.expoURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("expo push server error: %d", httpResp.StatusCode)
	}
	if httpResp.StatusCode >= 400 {
		return "", fmt.Errorf("%w: expo push rejected the request with status %d", ErrUndeliverable, httpResp.StatusCode)
	}

	var resp expoPushResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return "", err
	}

	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("%w: %s", ErrUndeliverable, resp.Errors[0].Message)
	}
	if resp.Data.Status != "ok" {
		message := resp.Data.Message
		if message == "" {
			message = "expo push rejected the notification"
		}
		// Expo asks senders to back off and retry when rate limited.
		if resp.Data.Details.Error == "MessageRateExceeded" {
			return "", errors.New(message)
		}
		return "", fmt.Errorf("%w: %s (%s)", ErrUndeliverable, message, resp.Data.Details.Error)
	}

	return resp.Data.ID, nil
}
