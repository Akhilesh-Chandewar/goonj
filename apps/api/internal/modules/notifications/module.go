// Package notifications implements the platform notification system: rows in
// the notifications table, created by domain events (a creator goes live, a
// recording becomes a draft episode, someone follows you, a subscribed
// session is about to start). Delivery is lazy — clients poll — which keeps
// the write path a single INSERT per event.
package notifications

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Notification types.
const (
	TypeLiveStarted     = "live_started"
	TypeRecordingReady  = "recording_ready"
	TypeNewFollower     = "new_follower"
	TypeSessionReminder = "session_reminder"
)

// Notification is one user-facing notification row.
type Notification struct {
	ID        string  `json:"id"`
	UserID    string  `json:"-"` // set by Notify, never echoed
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	SessionID *string `json:"session_id,omitempty"`
	AudioID   *string `json:"audio_id,omitempty"`
	ActorID   *string `json:"actor_id,omitempty"`
	Read      bool    `json:"read"`
	CreatedAt string  `json:"created_at"`
}

// Store reads and writes notifications.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts one notification. Fan-out to many users is done by callers
// looping over recipient ids (follower counts are small at this scale; a
// COPY-based bulk path can come later).
func (s *Store) Create(ctx context.Context, n Notification) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notifications (user_id, type, title, body, session_id, audio_id, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		n.UserID, n.Type, n.Title, n.Body, n.SessionID, n.AudioID, n.ActorID)
	return err
}

// List returns the caller's notifications, newest first.
func (s *Store) List(ctx context.Context, userID string, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, type, title, body, session_id::text, audio_id::text,
			actor_id::text, read_at IS NOT NULL, created_at::text
		FROM notifications WHERE user_id = $1
		ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.SessionID,
			&n.AudioID, &n.ActorID, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount returns the unread badge number.
func (s *Store) UnreadCount(ctx context.Context, userID string) (int, error) {
	var c int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&c)
	return c, err
}

// MarkRead marks one notification read (owner-checked).
func (s *Store) MarkRead(ctx context.Context, userID, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		id, userID)
	return err
}

// MarkAllRead clears the badge.
func (s *Store) MarkAllRead(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

// FollowerIDs returns the user ids following a creator (fan-out source).
func (s *Store) FollowerIDs(ctx context.Context, creatorID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.follower_id::text FROM follows f
		JOIN creators c ON c.id = f.creator_id
		WHERE f.creator_id = $1`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Module bundles the notifications module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// Router returns the notification routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}

// Service creates and lists notifications.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// Notify fans one notification out to many users, tolerating individual
// failures (notifications are never business-critical).
func (s *Service) Notify(ctx context.Context, userIDs []string, n Notification) {
	for _, uid := range userIDs {
		n.UserID = uid
		if err := s.store.Create(ctx, n); err != nil {
			s.log.Warn("notification insert failed",
				slog.String("user", uid), slog.String("type", n.Type), slog.Any("error", err))
		}
	}
}

// NotifyLiveStarted tells all followers a creator went live. Called by the
// live module on session Start.
func (s *Service) NotifyLiveStarted(ctx context.Context, creatorID, creatorName, sessionID, title string) {
	ids, err := s.store.FollowerIDs(ctx, creatorID)
	if err != nil || len(ids) == 0 {
		return
	}
	sid := sessionID
	s.Notify(ctx, ids, Notification{
		Type: TypeLiveStarted, Title: creatorName + " is live now",
		Body: title, SessionID: &sid,
	})
}

// NotifySessionReminder tells all followers a scheduled show starts soon.
// Called by the worker's reminder loop (~10 minutes before the planned start).
func (s *Service) NotifySessionReminder(ctx context.Context, creatorID, creatorName, sessionID, title string, startsAt time.Time) {
	ids, err := s.store.FollowerIDs(ctx, creatorID)
	if err != nil || len(ids) == 0 {
		return
	}
	sid := sessionID
	s.Notify(ctx, ids, Notification{
		Type: TypeSessionReminder,
		Title: creatorName + " goes live soon",
		Body: title + " starts at " + startsAt.Format("15:04 MST"),
		SessionID: &sid,
	})
}

// Handlers expose the notification REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// RegisterRoutes mounts notification routes on an existing mux.
func (h *Handlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Get("/", h.list)
	r.With(mw.RequireAuth).Get("/unread-count", h.unread)
	r.With(mw.RequireAuth).Post("/read-all", h.readAll)
	r.With(mw.RequireAuth).Post("/{id}/read", h.readOne)
}

// Router mounts on a fresh subrouter (mounted at /notifications).
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.store.List(r.Context(), identity.UserID, limit)
	if err != nil {
		h.log.Error("notification list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) unread(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	c, err := h.svc.store.UnreadCount(r.Context(), identity.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": c})
}

func (h *Handlers) readAll(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.store.MarkAllRead(r.Context(), identity.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) readOne(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.store.MarkRead(r.Context(), identity.UserID, chi.URLParam(r, "id")); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
