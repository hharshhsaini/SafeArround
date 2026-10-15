package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/repository"
	"github.com/obsidiannnn/Safe-Around/backend/internal/routes"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

type mockUserRepo struct {
	users        map[uint]*models.User
	usersByPhone map[string]*models.User
	usersByEmail map[string]*models.User
	autoInc      uint
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:        make(map[uint]*models.User),
		usersByPhone: make(map[string]*models.User),
		usersByEmail: make(map[string]*models.User),
		autoInc:      1,
	}
}

func (m *mockUserRepo) Create(u *models.User) error {
	u.ID = m.autoInc
	m.autoInc++
	m.users[u.ID] = u
	m.usersByPhone[u.Phone] = u
	m.usersByEmail[u.Email] = u
	return nil
}

func (m *mockUserRepo) GetByID(id uint) (*models.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (m *mockUserRepo) GetByPhone(phone string) (*models.User, error) {
	if u, ok := m.usersByPhone[phone]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (m *mockUserRepo) GetByEmail(email string) (*models.User, error) {
	if u, ok := m.usersByEmail[email]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (m *mockUserRepo) Update(u *models.User) error {
	m.users[u.ID] = u
	m.usersByPhone[u.Phone] = u
	m.usersByEmail[u.Email] = u
	return nil
}

func (m *mockUserRepo) Delete(id uint) error {
	if u, ok := m.users[id]; ok {
		delete(m.usersByPhone, u.Phone)
		delete(m.usersByEmail, u.Email)
		delete(m.users, id)
	}
	return nil
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()

	mockRepo := newMockUserRepo()
	rdb := redis.NewClient(&redis.Options{})
	twClient := twilio.NewClient("mock", "mock")

	h := handlers.NewAuthHandler(mockRepo, nil, rdb, twClient)
	api := r.Group("/api/v1")
	routes.SetupAuthRoutes(api, h, rdb)

	return r
}

func TestSendOTP(t *testing.T) {
	router := setupRouter()

	t.Run("Valid Phone", func(t *testing.T) {
		body := map[string]string{
			"phone": "1234567890",
		}
		
		jsonData, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", "/api/v1/auth/otp/send", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		
		assert.Contains(t, resp["message"], "otp sent successfully")
	})

	t.Run("Missing Phone", func(t *testing.T) {
		body := map[string]string{}
		
		jsonData, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", "/api/v1/auth/otp/send", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "phone number is required", resp["error"])
	})
}

func TestTokenValidation(t *testing.T) {
	t.Setenv("JWT_SECRET", "token-validation-secret")

	token, err := utils.GenerateToken(1, "test@example.com")
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := utils.ValidateToken(token)
	assert.NoError(t, err)
	assert.Equal(t, uint(1), claims.UserID)
	assert.Equal(t, "test@example.com", claims.Email)
}
