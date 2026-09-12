package main

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

// claims mirrors auth.Claims without importing the module.
type claims struct {
	UserID   string `json:"uid"`
	Role     string `json:"role"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// parseAccessToken validates a token with the shared JWT secret. Prior
// secrets (JWT_SECRETS_PREVIOUS) are accepted too, matching the API's
// rotation grace window.
func parseAccessToken(raw string) (*claims, error) {
	cfg := platform.Load("goonj-ws")
	secrets := append([]string{cfg.JWTSecret}, cfg.JWTSecretsPrevious...)

	for _, secret := range secrets {
		c := &claims{}
		token, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(secret), nil
		})
		if err == nil && token.Valid {
			return c, nil
		}
	}
	return nil, fmt.Errorf("invalid token")
}
