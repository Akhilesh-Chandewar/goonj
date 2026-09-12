// Package reports implements content moderation: listeners flag audio or
// live sessions, moderators work a PENDING queue and resolve or dismiss.
package reports

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Report statuses.
const (
	StatusPending  = "PENDING"
	StatusResolved = "RESOLVED"
	StatusDismissed = "DISMISSED"
)

// ValidReasons is the accepted report-reason vocabulary.
var ValidReasons = map[string]bool{
	"spam": true, "harassment": true, "copyright": true,
	"explicit": true, "misinformation": true, "other": true,
}

// Report is one moderation flag.
type Report struct {
	ID         string     `json:"id"`
	ReporterID string     `json:"reporter_id"`
	AudioID    *string    `json:"audio_id,omitempty"`
	SessionID  *string    `json:"session_id,omitempty"`
	Reason     string     `json:"reason"`
	Details    string     `json:"details"`
	Status     string     `json:"status"`
	ResolvedBy *string    `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  string     `json:"created_at"`
}

// ErrInvalidReport when the payload is malformed.
var ErrInvalidReport = errors.New("a target (audio_id or session_id) and a valid reason are required")

// Store persists reports.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts a report; re-reporting an already-resolved target is fine,
// duplicates within PENDING collapse via the expression index
// idx_reports_no_dupes (its COALESCE expressions must be repeated verbatim
// here — a plain column list cannot target an expression index).
func (s *Store) Create(ctx context.Context, r Report) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO reports (reporter_id, audio_id, session_id, reason, details)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (reporter_id, COALESCE(audio_id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(session_id, '00000000-0000-0000-0000-000000000000'::uuid), status) DO NOTHING`,
		r.ReporterID, r.AudioID, r.SessionID, r.Reason, r.Details)
	return err
}

// List returns reports by status (queue view).
func (s *Store) List(ctx context.Context, status string, limit int) ([]Report, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if status == "" {
		status = StatusPending
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, reporter_id::text, audio_id::text, session_id::text,
			reason, details, status, resolved_by::text, resolved_at, created_at::text
		FROM reports WHERE status = $1
		ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Report{}
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.AudioID, &r.SessionID,
			&r.Reason, &r.Details, &r.Status, &r.ResolvedBy, &r.ResolvedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Resolve closes a report.
func (s *Store) Resolve(ctx context.Context, id, moderatorID, status string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reports SET status = $2, resolved_by = $3, resolved_at = now()
		WHERE id = $1 AND status = 'PENDING'`, id, status, moderatorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Service implements the moderation use-cases.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// File records a listener's report.
func (s *Service) File(ctx context.Context, reporterID, audioID, sessionID, reason, details string) error {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if !ValidReasons[reason] || (audioID == "" && sessionID == "") {
		return ErrInvalidReport
	}
	if len(details) > 1000 {
		details = details[:1000]
	}
	r := Report{ReporterID: reporterID, Reason: reason, Details: details}
	if audioID != "" {
		r.AudioID = &audioID
	}
	if sessionID != "" {
		r.SessionID = &sessionID
	}
	return s.store.Create(ctx, r)
}

// Queue lists reports for moderators.
func (s *Service) Queue(ctx context.Context, status string, limit int) ([]Report, error) {
	return s.store.List(ctx, status, limit)
}

// Close resolves or dismisses a report.
func (s *Service) Close(ctx context.Context, reportID, moderatorID, status string) error {
	if status != StatusResolved && status != StatusDismissed {
		return ErrInvalidReport
	}
	return s.store.Resolve(ctx, reportID, moderatorID, status)
}

// Handlers expose the reports REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// RegisterRoutes mounts report routes on an existing mux.
func (h *Handlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Post("/", h.file)
	r.With(mw.RequireRole("MODERATOR", "ADMIN")).Get("/", h.queue)
	r.With(mw.RequireRole("MODERATOR", "ADMIN")).Post("/{id}/resolve", h.close(StatusResolved))
	r.With(mw.RequireRole("MODERATOR", "ADMIN")).Post("/{id}/dismiss", h.close(StatusDismissed))
}

// Router mounts on a fresh subrouter (mounted at /reports).
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

func (h *Handlers) file(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		AudioID   string `json:"audio_id"`
		SessionID string `json:"session_id"`
		Reason    string `json:"reason"`
		Details   string `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.File(r.Context(), identity.UserID, req.AudioID, req.SessionID, req.Reason, req.Details); err != nil {
		if errors.Is(err, ErrInvalidReport) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		h.log.Error("report insert failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *Handlers) queue(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	items, err := h.svc.Queue(r.Context(), status, 50)
	if err != nil {
		h.log.Error("report queue failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) close(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity := authmod.FromContext(r.Context())
		if err := h.svc.Close(r.Context(), chi.URLParam(r, "id"), identity.UserID, status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "report not found or already closed")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
