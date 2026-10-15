package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/models"
	"github.com/obsidiannnn/Safe-Around/backend/internal/repository"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
	"github.com/redis/go-redis/v9"
)

type AuthHandler struct {
	repo        repository.UserRepo
	sessionRepo repository.SessionRepo
	redis       *redis.Client
	twilio      *twilio.Client
}

func NewAuthHandler(repo repository.UserRepo, sessionRepo repository.SessionRepo, rdb *redis.Client, twClient *twilio.Client) *AuthHandler {
	return &AuthHandler{
		repo:        repo,
		sessionRepo: sessionRepo,
		redis:       rdb,
		twilio:      twClient,
	}
}

type authInput struct {
	Phone    string `json:"phone" binding:"required"`
	OTP      string `json:"otp,omitempty"`
	Password string `json:"password,omitempty"`
}

type setupProfileInput struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type changePasswordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
}

type resetPasswordInput struct {
	Phone       string `json:"phone" binding:"required"`
	OTP         string `json:"otp" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// verifySID is the Twilio Verify Service SID
const verifySID = "VAec453b0a41cbb20d1577c8c7ffe8ce64"

// saveSession persists a refresh token to PostgreSQL for durability. When it
// replaces a rotated-out session, that session's push tokens carry over:
// notifications are sent to the device token on the user's newest session.
func (h *AuthHandler) saveSession(c *gin.Context, userID uint, refreshToken string, replaces *models.UserSession) error {
	session := &models.UserSession{
		UserID:       userID,
		RefreshToken: refreshToken,
		UserAgent:    c.GetHeader("User-Agent"),
		ClientIP:     c.ClientIP(),
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour), // 7 days
	}
	if replaces != nil {
		session.FCMToken = replaces.FCMToken
		session.APNsToken = replaces.APNsToken
	}
	return h.sessionRepo.Create(session)
}

// issueTokens creates an access/refresh token pair and stores the refresh
// token as a session. replaces is the session being rotated out, if any.
func (h *AuthHandler) issueTokens(c *gin.Context, u *models.User, replaces *models.UserSession) (string, string, error) {
	access, err := utils.GenerateToken(u.ID, u.Email)
	if err != nil {
		return "", "", err
	}
	refresh, err := utils.GenerateRefreshToken(u.ID, u.Email)
	if err != nil {
		return "", "", err
	}

	// ✅ Persist session to PostgreSQL (survives Redis restart)
	_ = h.saveSession(c, u.ID, refresh, replaces)
	return access, refresh, nil
}

// SendOTP handles generating and sending an SMS securely
func (h *AuthHandler) SendOTP(c *gin.Context) {
	var input authInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone number is required"})
		return
	}

	input.Phone = utils.NormalizePhone(input.Phone)
	if err := validatePhoneNumber(input.Phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// If this is explicitly from the Signup flow, block if they already have a password
	if c.Query("type") == "signup" {
		if u, err := h.repo.GetByPhone(input.Phone); err == nil && u.Password != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "User already registered. Please log in."})
			return
		}
	}

	ctx := context.Background()
	rlKey := "rl:otp:send:" + input.Phone

	// Rate Limiting: max 3 OTP requests per 10 minutes
	attempts, _ := h.redis.Get(ctx, rlKey).Int()
	if attempts >= 3 {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, please wait"})
		return
	}

	h.redis.Incr(ctx, rlKey)
	h.redis.Expire(ctx, rlKey, 10*time.Minute)

	_, err := h.twilio.SendOTP(input.Phone, verifySID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send otp via SMS"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "otp sent successfully"})
}

// VerifyOTP validates the OTP and issues JWTs, creating the user if missing
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var input authInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone and otp are required"})
		return
	}

	input.Phone = utils.NormalizePhone(input.Phone)
	if err := validatePhoneNumber(input.Phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isValid, err := h.twilio.VerifyOTP(input.Phone, input.OTP, verifySID)
	if err != nil || !isValid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired otp"})
		return
	}

	// Fetch or Create User
	u, err := h.repo.GetByPhone(input.Phone)
	if err != nil {
		u = &models.User{
			Phone:           input.Phone,
			IsPhoneVerified: true,
		}
		if err := h.repo.Create(u); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user profile"})
			return
		}
	} else {
		if !u.IsPhoneVerified {
			u.IsPhoneVerified = true
			h.repo.Update(u)
		}
	}

	now := time.Now()
	u.LastLogin = &now
	_ = h.repo.Update(u)

	access, refresh, err := h.issueTokens(c, u, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "verified successfully",
		"user":    u,
		"tokens":  gin.H{"access": access, "refresh": refresh},
	})
}

// SetupProfile allows a logged-in OTP user to assign a permanent password and email
func (h *AuthHandler) SetupProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input setupProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name, email, and password required"})
		return
	}

	u, err := h.repo.GetByID(userID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if existing, _ := h.repo.GetByEmail(input.Email); existing != nil && existing.ID != u.ID {
		c.JSON(http.StatusConflict, gin.H{"error": "email already in use"})
		return
	}

	hash, err := utils.HashPassword(input.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not secure password"})
		return
	}

	u.Name = input.Name
	u.Email = input.Email
	u.Password = hash

	if err := h.repo.Update(u); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "profile securely set up", "user": u})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input changePasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new password is required"})
		return
	}

	u, err := h.repo.GetByID(userID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if u.Password != "" {
		if strings.TrimSpace(input.CurrentPassword) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "current password is required"})
			return
		}

		if !utils.ComparePassword(u.Password, input.CurrentPassword) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
			return
		}

		if utils.ComparePassword(u.Password, input.NewPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "please choose a new password different from your current password"})
			return
		}
	}

	hash, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not secure password"})
		return
	}

	u.Password = hash
	if err := h.repo.Update(u); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to change password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed successfully"})
}

// ResetPassword sets a new password for a registered phone number once its
// OTP checks out, then signs the account out on every device. It does not log
// the caller in; they sign in with the new password afterwards.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var input resetPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone, otp and a new password of at least 8 characters are required"})
		return
	}

	input.Phone = utils.NormalizePhone(input.Phone)
	if err := validatePhoneNumber(input.Phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isValid, err := h.twilio.VerifyOTP(input.Phone, input.OTP, verifySID)
	if err != nil || !isValid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired otp"})
		return
	}

	u, err := h.repo.GetByPhone(input.Phone)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no account found for this phone number"})
		return
	}

	hash, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not secure password"})
		return
	}

	u.Password = hash
	if err := h.repo.Update(u); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset password"})
		return
	}

	// Whoever knew the old password may still hold a session.
	_ = h.sessionRepo.RevokeAllForUser(u.ID)

	c.JSON(http.StatusOK, gin.H{"message": "password reset successfully"})
}

// Login permits direct phone/password login skipping OTP for fully set-up users
func (h *AuthHandler) Login(c *gin.Context) {
	var input authInput
	if err := c.ShouldBindJSON(&input); err != nil || input.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone and password required"})
		return
	}

	input.Phone = utils.NormalizePhone(input.Phone)
	if err := validatePhoneNumber(input.Phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	rlKey := "rl:login:" + input.Phone

	attempts, _ := h.redis.Get(ctx, rlKey).Int()
	if attempts >= 5 {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts, chill for a bit"})
		return
	}

	u, err := h.repo.GetByPhone(input.Phone)
	if err != nil || u.Password == "" || !utils.ComparePassword(u.Password, input.Password) {
		h.redis.Incr(ctx, rlKey)
		h.redis.Expire(ctx, rlKey, 15*time.Minute)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	h.redis.Del(ctx, rlKey)
	now := time.Now()
	u.LastLogin = &now
	_ = h.repo.Update(u)

	access, refresh, err := h.issueTokens(c, u, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":   u,
		"tokens": gin.H{"access": access, "refresh": refresh},
	})
}

// Refresh validates the refresh token against PostgreSQL and issues a new access token
func (h *AuthHandler) Refresh(c *gin.Context) {
	var input struct {
		Token string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}

	claims, err := utils.ValidateRefreshToken(input.Token)
	if err != nil {
		// Revoke bad session
		_ = h.sessionRepo.Revoke(input.Token)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	//  Validate against PostgreSQL: only live refresh tokens are stored there
	session, err := h.sessionRepo.GetByRefreshToken(input.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found or expired"})
		return
	}

	u, err := h.repo.GetByID(claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	// Rotate refresh token for security: revoke the old session, create a new one
	_ = h.sessionRepo.Revoke(input.Token)
	newAccess, newRefresh, err := h.issueTokens(c, u, session)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tokens": gin.H{"access": newAccess, "refresh": newRefresh},
	})
}

// Logout revokes the session whose refresh token is in the body; holding that
// token is all the authority needed to revoke it. An access token sent in the
// Authorization header is revoked in Redis too, so it stops working at once.
func (h *AuthHandler) Logout(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&body)

	// Revoke session in PostgreSQL
	if body.RefreshToken != "" {
		if err := h.sessionRepo.Revoke(body.RefreshToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to log out"})
			return
		}
	}

	parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		if claims, err := utils.ValidateAccessToken(parts[1]); err == nil && claims.ID != "" {
			if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
				h.redis.Set(c.Request.Context(), utils.RevokedAccessTokenKey(claims.ID), "1", ttl)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}
