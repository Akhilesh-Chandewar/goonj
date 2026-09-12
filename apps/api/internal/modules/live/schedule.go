package live

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Scheduling: a SCHEDULED session can carry a future start time. Followers
// get a reminder notification ~10 minutes before; the studio shows the
// countdown. The session still goes live via the normal Start flow.

// ErrInvalidScheduleTime when the requested time is not in the future.
var ErrInvalidScheduleTime = errors.New("scheduled_at must be in the future (within 30 days)")

// ScheduleInput is the payload for POST /live/{id}/schedule.
type ScheduleInput struct {
	// ScheduledAt is when the show is planned to start (RFC3339).
	ScheduledAt time.Time `json:"scheduled_at"`
}

// Schedule sets (or clears, with a zero time) a session's planned start.
func (s *Service) Schedule(ctx context.Context, sessionID, userID string, at time.Time) (*Session, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.CreatorID != s.mustCreatorID(ctx, userID) {
		return nil, ErrNotOwner
	}
	if session.Status != StatusScheduled {
		return nil, ErrInvalidStatus
	}
	if !at.IsZero() {
		if at.Before(time.Now()) || at.After(time.Now().Add(30*24*time.Hour)) {
			return nil, ErrInvalidScheduleTime
		}
	}
	if err := s.store.SetScheduledAt(ctx, sessionID, at); err != nil {
		return nil, err
	}
	s.store.AppendEvent(ctx, sessionID, "session.scheduled",
		[]byte(`{"scheduled_at":"`+at.Format(time.RFC3339)+`"}`))
	return s.store.Get(ctx, sessionID)
}

// Upcoming lists public SCHEDULED sessions with a future start time.
func (s *Service) Upcoming(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.store.ListUpcoming(ctx, limit)
}

// SetScheduledAt persists the planned start (NULL clears it).
func (st *Store) SetScheduledAt(ctx context.Context, id string, at time.Time) error {
	var ts *time.Time
	if !at.IsZero() {
		ts = &at
	}
	_, err := st.pool.Exec(ctx,
		`UPDATE live_sessions SET scheduled_at = $2, updated_at = now() WHERE id = $1`, id, ts)
	return err
}

// ListUpcoming returns public scheduled sessions with future times.
func (st *Store) ListUpcoming(ctx context.Context, limit int) ([]Session, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM live_sessions s
		LEFT JOIN creators c ON c.id = s.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE s.status = 'SCHEDULED' AND s.visibility = 'public'
			AND s.scheduled_at IS NOT NULL AND s.scheduled_at > now()
		ORDER BY s.scheduled_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

// ReminderDueSessions returns SCHEDULED sessions starting within the next
// window whose followers have not been reminded yet (the reminders_sent marker
// lives in live_events via a dedicated event type).
func (st *Store) ReminderDueSessions(ctx context.Context, within time.Duration) ([]Session, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM live_sessions s
		LEFT JOIN creators c ON c.id = s.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE s.status = 'SCHEDULED' AND s.visibility = 'public'
			AND s.scheduled_at IS NOT NULL
			AND s.scheduled_at BETWEEN now() AND now() + $1::interval				AND NOT EXISTS (
					SELECT 1 FROM live_events e
					WHERE e.session_id = s.id AND e.type = 'reminder.sent'
				)
		LIMIT 50`, within)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

// MarkReminderSent records that followers were notified (dedupe marker).
func (st *Store) MarkReminderSent(ctx context.Context, sessionID string) error {
	st.AppendEvent(ctx, sessionID, "reminder.sent", []byte(`{}`))
	return nil
}

// schedule handlers are mounted from Router (see handlers.go).
func (h *Handlers) schedule(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var in ScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sess, err := h.svc.Schedule(r.Context(), chi.URLParam(r, "id"), identity.UserID, in.ScheduledAt)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotOwner), errors.Is(err, ErrInvalidStatus), errors.Is(err, ErrInvalidScheduleTime):
			writeErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			writeErr(w, http.StatusNotFound, "live session not found")
		default:
			h.log.Error("schedule failed", slog.Any("error", err))
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handlers) upcoming(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.svc.Upcoming(r.Context(), 20)
	if err != nil {
		h.log.Error("upcoming failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sessions})
}
