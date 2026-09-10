package auth

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

// Identity is the authenticated-user shape shared with other modules.
type Identity struct {
	UserID   string
	Role     string
	Username string
}

// Service handles credential flows. Business logic lives here, not handlers.
type Service struct {
	pool   *pgxpool.Pool
	tokens *TokenService
	log    *slog.Logger
}

func NewService(pool *pgxpool.Pool, tokens *TokenService, log *slog.Logger) *Service {
	return &Service{pool: pool, tokens: tokens, log: log}
}

// RegisterInput is the validated registration payload.
type RegisterInput struct {
	Email       string
	Password    string
	Username    string
	DisplayName string
	// AsCreator additionally creates a channel for the new user.
	AsCreator bool
}

var (
	ErrEmailTaken   = errors.New("email already registered")
	ErrUserTaken    = errors.New("username already taken")
	ErrWeakPassword = errors.New("password must be at least 8 characters")
	ErrBadUsername  = errors.New("username must be 3-30 chars: a-z, 0-9, underscore")
)

// Register creates a user (+ optional creator channel) and issues tokens.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Tokens, string, error) {
	if len(in.Password) < 8 {
		return nil, "", ErrWeakPassword
	}
	if !usernamePattern.MatchString(in.Username) {
		return nil, "", ErrBadUsername
	}

	hash, err := hashPassword(in.Password)
	if err != nil {
		return nil, "", err
	}

	role := "USER"
	if in.AsCreator {
		role = "CREATOR"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING id`, in.Email, hash, role).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", ErrEmailTaken
		}
		return nil, "", err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO profiles (user_id, username, display_name)
		VALUES ($1, $2, $3)`, userID, in.Username, in.DisplayName)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", ErrUserTaken
		}
		return nil, "", err
	}

	if in.AsCreator {
		_, err = tx.Exec(ctx, `
			INSERT INTO creators (user_id, handle, channel_name)
			VALUES ($1, $2, $3)`, userID, in.Username, in.DisplayName)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, "", ErrUserTaken
			}
			return nil, "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}

	tokens, err := s.tokens.Issue(ctx, userID, role, in.Username)
	if err != nil {
		return nil, "", err
	}
	return tokens, userID, nil
}

// Login verifies credentials and issues tokens.
func (s *Service) Login(ctx context.Context, email, password string) (*Tokens, error) {
	var (
		id   string
		hash string
		role string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, password_hash, role FROM users
		WHERE email = $1 AND deleted_at IS NULL AND is_active`, email).
		Scan(&id, &hash, &role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !checkPassword(hash, password) {
		return nil, ErrInvalidCredentials
	}

	var username string
	_ = s.pool.QueryRow(ctx, `
		SELECT username FROM profiles WHERE user_id = $1`, id).Scan(&username)

	return s.tokens.Issue(ctx, id, role, username)
}

// Refresh rotates a refresh token into a new pair.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	return s.tokens.RotateRefresh(ctx, refreshToken, s.lookupRoleAndUsername)
}

func (s *Service) lookupRoleAndUsername(ctx context.Context, userID string) (string, string, error) {
	var role, username string
	err := s.pool.QueryRow(ctx, `
		SELECT u.role, p.username
		FROM users u
		JOIN profiles p ON p.user_id = u.id
		WHERE u.id = $1 AND u.deleted_at IS NULL AND u.is_active`, userID).
		Scan(&role, &username)
	if err != nil {
		return "", "", err
	}
	return role, username, err
}

// Logout revokes the refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) {
	s.tokens.RevokeRefresh(ctx, refreshToken)
}

// IdentityByUserID resolves a user's identity for gateways (ws-gateway auth).
func (s *Service) IdentityByUserID(ctx context.Context, userID string) (*Identity, error) {
	role, username, err := s.lookupRoleAndUsername(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Identity{UserID: userID, Role: role, Username: username}, nil
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
