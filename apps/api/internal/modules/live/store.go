package live

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status values for live sessions.
const (
	StatusScheduled = "SCHEDULED"
	StatusLive      = "LIVE"
	StatusEnded     = "ENDED"
	StatusCancelled = "CANCELLED"
)

// Session is a live broadcast owned by a creator.
type Session struct {
	ID             string     `json:"id"`
	CreatorID      string     `json:"creator_id"`
	CreatorName    string     `json:"creator_name,omitempty"`
	Handle         string     `json:"handle,omitempty"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Category       string     `json:"category"`
	Status         string     `json:"status"`
	Visibility     string     `json:"visibility"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"` // planned start (SCHEDULED only)
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	PeakListeners  int        `json:"peak_listeners"`
	TotalListeners int64      `json:"total_listeners"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Stream holds per-session stream credentials metadata.
type Stream struct {
	ID           string
	SessionID    string
	RoomName     string
	IngestToken  string
	TokenExpires time.Time
}

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("live session not found")

// Store is the Postgres repository for live sessions.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const sessionColumns = `
	s.id, s.creator_id, s.title, s.description, s.category, s.status,
	s.visibility, s.scheduled_at, s.started_at, s.ended_at,
	s.peak_listeners, s.total_listeners, s.created_at,
	COALESCE(c.channel_name, ''), COALESCE(p.username, '')`

func scanSession(row pgx.Row) (*Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.CreatorID, &s.Title, &s.Description, &s.Category,
		&s.Status, &s.Visibility, &s.ScheduledAt, &s.StartedAt, &s.EndedAt,
		&s.PeakListeners, &s.TotalListeners, &s.CreatedAt,
		&s.CreatorName, &s.Handle)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create inserts a new SCHEDULED session.
func (st *Store) Create(ctx context.Context, creatorID, title, description, category, visibility string) (*Session, error) {
	row := st.pool.QueryRow(ctx, `
		INSERT INTO live_sessions (creator_id, title, description, category, visibility)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, creator_id, title, description, category, status,
			visibility, scheduled_at, started_at, ended_at,
			peak_listeners, total_listeners, created_at, '', ''`,
		creatorID, title, description, category, visibility)
	return scanSession(row)
}

// Get fetches one session by id.
func (st *Store) Get(ctx context.Context, id string) (*Session, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT `+sessionColumns+`
		FROM live_sessions s
		LEFT JOIN creators c ON c.id = s.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE s.id = $1`, id)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// ListLive returns public sessions currently LIVE, newest first.
func (st *Store) ListLive(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := st.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM live_sessions s
		LEFT JOIN creators c ON c.id = s.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE s.status = 'LIVE' AND s.visibility = 'public'
		ORDER BY s.started_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ListByCreator returns a creator's sessions (studio view).
func (st *Store) ListByCreator(ctx context.Context, creatorID string, limit int) ([]Session, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := st.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM live_sessions s
		LEFT JOIN creators c ON c.id = s.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE s.creator_id = $1
		ORDER BY s.created_at DESC
		LIMIT $2`, creatorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// CreatorIDByUserID resolves the channel id for a user.
func (st *Store) CreatorIDByUserID(ctx context.Context, userID string) (string, error) {
	var id string
	err := st.pool.QueryRow(ctx,
		`SELECT id FROM creators WHERE user_id = $1 AND deleted_at IS NULL`, userID).
		Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// MarkStarted moves a session to LIVE.
func (st *Store) MarkStarted(ctx context.Context, id string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE live_sessions
		SET status = 'LIVE', started_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('SCHEDULED', 'LIVE')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkEnded moves a session to ENDED and persists presence aggregates.
func (st *Store) MarkEnded(ctx context.Context, id string, peak int, total int64) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE live_sessions
		SET status = 'ENDED', ended_at = now(), updated_at = now(),
			peak_listeners = GREATEST(peak_listeners, $2),
			total_listeners = GREATEST(total_listeners, $3)
		WHERE id = $1 AND status = 'LIVE'`, id, peak, total)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetCreatorLive toggles the creators.is_live flag used for avatars/discovery.
func (st *Store) SetCreatorLive(ctx context.Context, creatorID string, isLive bool) error {
	_, err := st.pool.Exec(ctx,
		`UPDATE creators SET is_live = $2, updated_at = now() WHERE id = $1`,
		creatorID, isLive)
	return err
}

// UpsertStream stores stream credential metadata.
func (st *Store) UpsertStream(ctx context.Context, s *Stream) error {
	_, err := st.pool.Exec(ctx, `
		INSERT INTO live_streams (session_id, room_name, ingest_token, token_expires)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (session_id) DO UPDATE
		SET room_name = EXCLUDED.room_name,
			ingest_token = EXCLUDED.ingest_token,
			token_expires = EXCLUDED.token_expires`,
		s.SessionID, s.RoomName, s.IngestToken, s.TokenExpires)
	return err
}

// AppendEvent records a live_event row (audit/analytics trail).
func (st *Store) AppendEvent(ctx context.Context, sessionID, eventType string, payload []byte) {
	if payload == nil {
		payload = []byte(`{}`)
	}
	_, _ = st.pool.Exec(ctx,
		`INSERT INTO live_events (session_id, type, payload) VALUES ($1, $2, $3)`,
		sessionID, eventType, payload)
}

// ChatMessage is a persisted chat row.
type ChatMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Body      string    `json:"body"`
	IsPinned  bool      `json:"is_pinned"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertChat persists one chat message (called by the worker, async from send).
func (st *Store) InsertChat(ctx context.Context, sessionID, userID, body string) error {
	_, err := st.pool.Exec(ctx,
		`INSERT INTO live_chat_messages (session_id, user_id, body) VALUES ($1, $2, $3)`,
		sessionID, userID, body)
	return err
}

// RecentChat returns the latest messages oldest-first for room hydration.
func (st *Store) RecentChat(ctx context.Context, sessionID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := st.pool.Query(ctx, `
		SELECT m.id, m.session_id, m.user_id, COALESCE(p.username, 'listener'),
			m.body, m.is_pinned, m.created_at
		FROM live_chat_messages m
		LEFT JOIN profiles p ON p.user_id = m.user_id
		WHERE m.session_id = $1 AND m.deleted_at IS NULL
		ORDER BY m.created_at DESC
		LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ChatMessage, 0, limit)
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.UserID, &m.Username, &m.Body, &m.IsPinned, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
