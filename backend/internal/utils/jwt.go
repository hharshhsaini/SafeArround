package utils

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrMissingSecret = errors.New("JWT_SECRET is not set")
)

// Token types, stored in the token_type claim so an access token can't be
// used to refresh and a refresh token can't be used to call the API.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

func getSecret() ([]byte, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, ErrMissingSecret
	}
	return []byte(secret), nil
}

type Claims struct {
	UserID    uint   `json:"user_id"`
	Email     string `json:"email"`
	TokenType string `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, email string) (string, error) {
	return generateToken(userID, email, TokenTypeAccess, accessTokenTTL)
}

func GenerateRefreshToken(userID uint, email string) (string, error) {
	return generateToken(userID, email, TokenTypeRefresh, refreshTokenTTL)
}

func generateToken(userID uint, email, tokenType string, ttl time.Duration) (string, error) {
	secret, err := getSecret()
	if err != nil {
		return "", err
	}

	claims := &Claims{
		UserID:    userID,
		Email:     email,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ValidateToken checks a token's signature and expiry, whatever its type.
func ValidateToken(tokenStr string) (*Claims, error) {
	secret, err := getSecret()
	if err != nil {
		return nil, err
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ValidateAccessToken accepts only access tokens. Tokens issued before token
// types existed are rejected; the app swaps them using its refresh token.
func ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims, err := ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ValidateRefreshToken rejects access tokens. It still accepts untyped tokens
// issued before token types existed, so signed-in users aren't logged out;
// those expire within 7 days of this change. Callers must also find the token
// in user_sessions, which only ever holds refresh tokens.
func ValidateRefreshToken(tokenStr string) (*Claims, error) {
	claims, err := ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeRefresh && claims.TokenType != "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// RevokedAccessTokenKey is the Redis key that marks an access token as logged
// out until it expires.
func RevokedAccessTokenKey(tokenID string) string {
	return "bl:" + tokenID
}

func ExtractClaims(tokenStr string) (uint, string, error) {
	claims, err := ValidateToken(tokenStr)
	if err != nil {
		return 0, "", err
	}
	return claims.UserID, claims.Email, nil
}
