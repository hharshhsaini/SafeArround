package tests

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTokenEnv uses Twilio's mock mode, where the OTP "123456" is valid.

func TestPasswordResetWithValidOTP(t *testing.T) {
	env := setupTokenEnv(t, nil)
	_, refresh := env.signIn(t, "")

	w := env.post("/api/v1/auth/password/reset", gin.H{
		"phone":        "9876543210",
		"otp":          "123456",
		"new_password": "new-secret-123",
	}, "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, utils.ComparePassword(env.user.Password, "new-secret-123"))

	// Every existing session is signed out.
	w = env.post("/api/v1/auth/refresh", gin.H{"refresh_token": refresh}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPasswordResetRejectsWrongOTP(t *testing.T) {
	env := setupTokenEnv(t, nil)

	w := env.post("/api/v1/auth/password/reset", gin.H{
		"phone":        "9876543210",
		"otp":          "000000",
		"new_password": "new-secret-123",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, env.user.Password)
}

func TestPasswordResetForUnknownPhone(t *testing.T) {
	env := setupTokenEnv(t, nil)

	w := env.post("/api/v1/auth/password/reset", gin.H{
		"phone":        "9123456789",
		"otp":          "123456",
		"new_password": "new-secret-123",
	}, "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPasswordResetRequiresEightCharacters(t *testing.T) {
	env := setupTokenEnv(t, nil)

	w := env.post("/api/v1/auth/password/reset", gin.H{
		"phone":        "9876543210",
		"otp":          "123456",
		"new_password": "short",
	}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
