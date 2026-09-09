package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type ctxKey int

const identityKey ctxKey = 1

// FromContext returns the Identity stored by the Authenticate middleware,
// or nil when the request is unauthenticated.
func FromContext(ctx context.Context) *Identity {
	identity, _ := ctx.Value(identityKey).(*Identity)
	return identity
}

func withIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// Middleware authenticates bearer tokens and enforces roles.
type Middleware struct {
	tokens *TokenService
}

func NewMiddleware(tokens *TokenService) *Middleware {
	return &Middleware{tokens: tokens}
}

// Authenticate validates the bearer token and stores the Identity in context.
// Unauthenticated requests continue anonymously; protected routes use
// RequireAuth/RequireRole to reject them.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			if claims, err := m.tokens.ValidateAccess(strings.TrimPrefix(h, "Bearer ")); err == nil {
				identity := &Identity{
					UserID:   claims.UserID,
					Role:     claims.Role,
					Username: claims.Username,
				}
				next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), identity)))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth rejects requests without a valid identity.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects requests whose role is not in the allowed set.
func (m *Middleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity := FromContext(r.Context())
			if identity == nil {
				writeJSONError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			for _, role := range roles {
				if identity.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSONError(w, http.StatusForbidden, "insufficient permissions")
		})
	}
}

// Service interface consumed by handlers (satisfied by *Service).
type API interface {
	Register(ctx context.Context, in RegisterInput) (*Tokens, string, error)
	Login(ctx context.Context, email, password string) (*Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (*Tokens, error)
	Logout(ctx context.Context, refreshToken string)
	IdentityByUserID(ctx context.Context, userID string) (*Identity, error)
}

// Handlers exposes auth endpoints on /auth.
type Handlers struct {
	api API
}

func NewHandlers(api API) *Handlers {
	return &Handlers{api: api}
}

// Router mounts the auth routes.
func (h *Handlers) Router(mw *Middleware) http.Handler {
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	r.With(mw.RequireAuth).Get("/me", h.me)
	return r
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AsCreator   bool   `json:"as_creator"`
}

type authResponse struct {
	Tokens   *Tokens   `json:"tokens"`
	Username string    `json:"username"`
	UserID   string    `json:"user_id"`
}

func (h *Handlers) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Email == "" || req.Password == "" || req.Username == "" {
		writeJSONError(w, http.StatusBadRequest, "email, password and username are required")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	tokens, userID, err := h.api.Register(r.Context(), RegisterInput{
		Email:       req.Email,
		Password:    req.Password,
		Username:    req.Username,
		DisplayName: req.DisplayName,
		AsCreator:   req.AsCreator,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailTaken):
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrUserTaken):
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrWeakPassword), errors.Is(err, ErrBadUsername):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, "registration failed")
		}
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Tokens: tokens, UserID: userID, Username: req.Username})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	tokens, err := h.api.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Tokens: tokens})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handlers) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeJSONError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	tokens, err := h.api.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Tokens: tokens})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		h.api.Logout(r.Context(), req.RefreshToken)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handlers) me(w http.ResponseWriter, r *http.Request) {
	identity := FromContext(r.Context())
	if identity == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, identity)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

