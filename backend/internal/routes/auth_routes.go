package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/handlers"
	"github.com/obsidiannnn/Safe-Around/backend/internal/middleware"
	"github.com/redis/go-redis/v9"
)

func SetupAuthRoutes(r *gin.RouterGroup, h *handlers.AuthHandler, rdb *redis.Client) {
	auth := r.Group("/auth")
	auth.POST("/otp/send", h.SendOTP)
	auth.POST("/otp/verify", h.VerifyOTP)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)
	auth.POST("/password/reset", h.ResetPassword)
	// Logout is authorised by the refresh token in its body, so it still works
	// after the access token has expired.
	auth.POST("/logout", h.Logout)

	protected := auth.Group("")
	protected.Use(middleware.AuthRequired(rdb))
	protected.POST("/password/setup", h.SetupProfile)
	protected.POST("/password/change", h.ChangePassword)
}
