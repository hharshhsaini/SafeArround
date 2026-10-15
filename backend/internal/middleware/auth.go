package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"github.com/redis/go-redis/v9"
)

// AuthRequired validates the Bearer access token on every protected route.
// Refresh tokens are rejected, and so are access tokens revoked at logout
// (checked in rdb when it is non-nil).
// It sets "user_id" and "user_email" in the Gin context for downstream handlers.
func AuthRequired(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header must be 'Bearer <token>'",
			})
			return
		}

		claims, err := utils.ValidateAccessToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
			})
			return
		}

		if claims.UserID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid token claims",
			})
			return
		}

		if rdb != nil && claims.ID != "" {
			revoked, err := rdb.Exists(c.Request.Context(), utils.RevokedAccessTokenKey(claims.ID)).Result()
			if err != nil {
				// Fail open like the rate limiter: a Redis outage must not block
				// SOS requests, and revoked access tokens expire within 15 minutes.
				log.Printf("revoked token check failed: %v", err)
			} else if revoked > 0 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "Token has been revoked",
				})
				return
			}
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Next()
	}
}

// GetCurrentUserID extracts the authenticated userID from the Gin context.
func GetCurrentUserID(c *gin.Context) uint {
	val, _ := c.Get("user_id")
	id, _ := val.(uint)
	return id
}
