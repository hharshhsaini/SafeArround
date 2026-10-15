package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/middleware"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/repository"
	"github.com/obsidiannnn/Safe-Around/backend/internal/routes"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tokenTestSecret = "token-test-secret"

type mockSessionRepo struct {
	sessions map[string]*models.UserSession
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*models.UserSession)}
}

func (m *mockSessionRepo) Create(s *models.UserSession) error {
	m.sessions[s.RefreshToken] = s
	return nil
}

func (m *mockSessionRepo) GetByRefreshToken(token string) (*models.UserSession, error) {
	s, ok := m.sessions[token]
	if !ok || s.IsRevoked || time.Now().After(s.ExpiresAt) {
		return nil, repository.ErrSessionNotFound
	}
	return s, nil
}

func (m *mockSessionRepo) GetByUserID(userID uint) ([]*models.UserSession, error) {
	var out []*models.UserSession
	for _, s := range m.sessions {
		if s.UserID == userID && !s.IsRevoked {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockSessionRepo) Revoke(token string) error {
	if s, ok := m.sessions[token]; ok {
		s.IsRevoked = true
	}
	return nil
}

func (m *mockSessionRepo) RevokeAllForUser(userID uint) error {
	for _, s := range m.sessions {
		if s.UserID == userID {
			s.IsRevoked = true
		}
	}
	return nil
}

func (m *mockSessionRepo) DeleteExpired() error { return nil }

type tokenTestEnv struct {
	router   *gin.Engine
	sessions *mockSessionRepo
	user     *models.User
}

// setupTokenEnv mounts the auth routes plus a protected /api/v1/me route.
func setupTokenEnv(t *testing.T, rdb *redis.Client) *tokenTestEnv {
	t.Helper()
	t.Setenv("JWT_SECRET", tokenTestSecret)
	gin.SetMode(gin.TestMode)

	users := newMockUserRepo()
	user := &models.User{Phone: "+919876543210", IsPhoneVerified: true}
	require.NoError(t, users.Create(user))
	sessions := newMockSessionRepo()

	r := gin.New()
	h := handlers.NewAuthHandler(users, sessions, rdb, twilio.NewClient("mock", "mock"))
	api := r.Group("/api/v1")
	routes.SetupAuthRoutes(api, h, rdb)
	api.GET("/me", middleware.AuthRequired(rdb), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": middleware.GetCurrentUserID(c)})
	})

	return &tokenTestEnv{router: r, sessions: sessions, user: user}
}

// signIn stores a session the way login does and returns its token pair.
func (env *tokenTestEnv) signIn(t *testing.T, fcmToken string) (string, string) {
	t.Helper()
	access, err := utils.GenerateToken(env.user.ID, env.user.Email)
	require.NoError(t, err)
	refresh, err := utils.GenerateRefreshToken(env.user.ID, env.user.Email)
	require.NoError(t, err)
	require.NoError(t, env.sessions.Create(&models.UserSession{
		UserID:       env.user.ID,
		RefreshToken: refresh,
		FCMToken:     fcmToken,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
	}))
	return access, refresh
}

func (env *tokenTestEnv) callMe(token string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	return w.Code
}

func (env *tokenTestEnv) post(path string, body interface{}, accessToken string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	return w
}

// legacyToken signs a token without a token_type claim, like tokens issued
// before token types existed.
func legacyToken(t *testing.T, userID uint, ttl time.Duration) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, utils.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			ID:        "legacy-token",
		},
	}).SignedString([]byte(tokenTestSecret))
	require.NoError(t, err)
	return token
}

func TestProtectedRoutesAcceptOnlyAccessTokens(t *testing.T) {
	env := setupTokenEnv(t, nil)
	access, refresh := env.signIn(t, "")

	assert.Equal(t, http.StatusOK, env.callMe(access))
	assert.Equal(t, http.StatusUnauthorized, env.callMe(refresh))
	assert.Equal(t, http.StatusUnauthorized, env.callMe(legacyToken(t, env.user.ID, 7*24*time.Hour)))
}

func TestRefreshRejectsAccessTokens(t *testing.T) {
	env := setupTokenEnv(t, nil)
	access, _ := env.signIn(t, "")

	w := env.post("/api/v1/auth/refresh", gin.H{"refresh_token": access}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRefreshRotatesSessionAndKeepsPushToken(t *testing.T) {
	env := setupTokenEnv(t, nil)
	_, refresh := env.signIn(t, "device-push-token")

	w := env.post("/api/v1/auth/refresh", gin.H{"refresh_token": refresh}, "")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Tokens struct {
			Access  string `json:"access"`
			Refresh string `json:"refresh"`
		} `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, http.StatusOK, env.callMe(resp.Tokens.Access))

	assert.True(t, env.sessions.sessions[refresh].IsRevoked, "old session should be revoked")
	newSession, err := env.sessions.GetByRefreshToken(resp.Tokens.Refresh)
	require.NoError(t, err)
	assert.Equal(t, "device-push-token", newSession.FCMToken)
}

func TestRefreshAcceptsLegacySessionToken(t *testing.T) {
	env := setupTokenEnv(t, nil)
	legacy := legacyToken(t, env.user.ID, 7*24*time.Hour)
	require.NoError(t, env.sessions.Create(&models.UserSession{
		UserID:       env.user.ID,
		RefreshToken: legacy,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
	}))

	w := env.post("/api/v1/auth/refresh", gin.H{"refresh_token": legacy}, "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLogoutRevokesRefreshSessionWithoutAccessToken(t *testing.T) {
	env := setupTokenEnv(t, nil)
	_, refresh := env.signIn(t, "")

	w := env.post("/api/v1/auth/logout", gin.H{"refresh_token": refresh}, "")
	require.Equal(t, http.StatusOK, w.Code)

	w = env.post("/api/v1/auth/refresh", gin.H{"refresh_token": refresh}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Needs a Redis server: set TEST_REDIS_ADDR (e.g. localhost:6379) to run it.
func TestLogoutRevokesAccessToken(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rdb.Close() })
	require.NoError(t, rdb.Ping(context.Background()).Err())

	env := setupTokenEnv(t, rdb)
	access, refresh := env.signIn(t, "")
	require.Equal(t, http.StatusOK, env.callMe(access))

	w := env.post("/api/v1/auth/logout", gin.H{"refresh_token": refresh}, access)
	require.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, http.StatusUnauthorized, env.callMe(access))
}
