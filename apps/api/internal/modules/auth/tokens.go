// Package auth — token issuance and validation. Access tokens are short-lived
// JWTs carrying user identity + role; refresh tokens are opaque, stored hashed
// in Redis with a TTL. Rotation on refresh prevents silent reuse.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
	// refreshKeyPrefix stores SHA-256 hashes of refresh tokens, never raw values.
	refreshKeyPrefix = "goonj:refresh:"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenRevoked       = errors.New("token revoked")
)

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Tokens bundles an access token with its expiry for the API response.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// TokenService issues and validates tokens.
//
// Secrets support zero-downtime rotation: JWT_SECRET is the primary key
// used for signing; JWT_SECRETS_PREVIOUS (comma-separated) are still
// accepted for validation only, so tokens minted before a rotation stay
// valid until they naturally expire and clients re-login seamlessly.
type TokenService struct {
	secret  []byte // primary (signing) key
	prior   [][]byte // previous keys (validation only)
	redis   *redis.Client
}

func NewTokenService(secret string, rdb *redis.Client) *TokenService {
	return &TokenService{secret: []byte(secret), redis: rdb}
}

// AddPriorSecret registers a previous signing key (oldest last). Validation
// falls back to these after the primary key fails; signing always uses the
// primary. Call once per previous key, newest previous first.
func (s *TokenService) AddPriorSecret(secret string) {
	if secret == "" {
		return
	}
	s.prior = append(s.prior, []byte(secret))
}

// Claims are embedded in every access token.
type Claims struct {
	UserID   string `json:"uid"`
	Role     string `json:"role"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func (s *TokenService) issueAccessToken(userID, role, username string, now time.Time) (string, error) {
	claims := Claims{
		UserID:   userID,
		Role:     role,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    "goonj",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// issueRefreshToken creates an opaque token and stores its SHA-256 hash.
func (s *TokenService) issueRefreshToken(ctx context.Context, userID string, now time.Time) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	sum := sha256Sum(token)
	if err := s.redis.Set(ctx, refreshKeyPrefix+sum, userID, refreshTokenTTL).Err(); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return token, nil
}

// Issue creates a fresh token pair for a user.
func (s *TokenService) Issue(ctx context.Context, userID, role, username string) (*Tokens, error) {
	now := time.Now()
	access, err := s.issueAccessToken(userID, role, username, now)
	if err != nil {
		return nil, err
	}
	refresh, err := s.issueRefreshToken(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	return &Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
	}, nil
}

// Validate parses an access token and returns its claims. The signature is
// checked against the primary secret first, then any prior secrets (rotation
// grace window).
func (s *TokenService) Validate(tokenString string) (*Claims, error) {
	keys := make([][]byte, 0, 1+len(s.prior))
	keys = append(keys, s.secret)
	keys = append(keys, s.prior...)

	for _, key := range keys {
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return key, nil
		})
		if err == nil && token.Valid {
			return claims, nil
		}
	}
	return nil, ErrInvalidToken
}

// ValidateAccess validates an access token string into claims.
func (s *TokenService) ValidateAccess(token string) (*Claims, error) {
	return s.Validate(token)
}

// RotateRefresh exchanges a valid refresh token for a new pair, revoking the
// old one (single-use refresh tokens).
func (s *TokenService) RotateRefresh(ctx context.Context, refreshToken string, lookupUser func(ctx context.Context, userID string) (role, username string, err error)) (*Tokens, error) {
	sum := sha256Sum(refreshToken)
	userID, err := s.redis.GetDel(ctx, refreshKeyPrefix+sum).Result()
	if err != nil {
		return nil, ErrTokenRevoked
	}

	role, username, err := lookupUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.Issue(ctx, userID, role, username)
}

// RevokeRefresh deletes a refresh token (logout).
func (s *TokenService) RevokeRefresh(ctx context.Context, refreshToken string) {
	_ = s.redis.Del(ctx, refreshKeyPrefix+sha256Sum(refreshToken)).Err()
}

func sha256Sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
