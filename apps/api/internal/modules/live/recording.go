package live

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Recording mirrors one live_recordings row.
type Recording struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	EgressID    string     `json:"egress_id"`
	RoomName    string     `json:"room_name"`
	Status      string     `json:"status"`
	StorageKey  string     `json:"storage_key,omitempty"`
	FileSize    int64      `json:"file_size"`
	AudioID     string     `json:"audio_id,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Recording statuses (kept in sync with migration 00004).
const (
	RecordingStatusRecording = "RECORDING"
	RecordingStatusEnding    = "ENDING"
	RecordingStatusCompleted = "COMPLETED"
	RecordingStatusFailed    = "FAILED"
	RecordingStatusConverted = "CONVERTED"
)

// Recorder starts and stops session recordings. Implemented by the LiveKit
// egress client (see infrastructure/livekit); swappable per PLAN §5.
//
// Recordings use audio-only room-composite egress: the egress service renders
// the room itself (no external template page, no participant dependency), so
// the creator's recording does not die with their connection and works on
// egress builds without participant-egress support. Only the room name is
// needed — Goonj is audio-only, so there is nothing else to record.
type Recorder interface {
	// StartRecording begins recording a room and returns the egress id.
	StartRecording(ctx context.Context, roomName string) (egressID string, err error)
	// StopRecording requests a graceful stop and finalize.
	StopRecording(ctx context.Context, egressID string) error
	// RecordingStatus polls the egress outcome.
	RecordingStatus(ctx context.Context, egressID string) (status string, fileSize int64, storageKey string, err error)
}

// RecordingStore is the Postgres repository for live_recordings.
type RecordingStore struct {
	pool *pgxpool.Pool
}

func NewRecordingStore(pool *pgxpool.Pool) *RecordingStore {
	return &RecordingStore{pool: pool}
}

// CreateRecording registers a started recording.
func (st *RecordingStore) CreateRecording(ctx context.Context, sessionID, egressID, roomName string) (*Recording, error) {
	row := st.pool.QueryRow(ctx, `
		INSERT INTO live_recordings (session_id, egress_id, room_name, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, session_id, egress_id, room_name, status,
			COALESCE(storage_key, ''), file_size, COALESCE(audio_id::text, ''),
			COALESCE(error, ''), started_at, completed_at, created_at`,
		sessionID, egressID, roomName, RecordingStatusRecording)
	return scanRecording(row)
}

// RecordingByEgress fetches a recording row by its LiveKit egress id.
func (st *RecordingStore) RecordingByEgress(ctx context.Context, egressID string) (*Recording, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT id, session_id, egress_id, room_name, status,
			COALESCE(storage_key, ''), file_size, COALESCE(audio_id::text, ''),
			COALESCE(error, ''), started_at, completed_at, created_at
		FROM live_recordings WHERE egress_id = $1`, egressID)
	r, err := scanRecording(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// LatestRecording returns the most recent recording for a session.
func (st *RecordingStore) LatestRecording(ctx context.Context, sessionID string) (*Recording, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT id, session_id, egress_id, room_name, status,
			COALESCE(storage_key, ''), file_size, COALESCE(audio_id::text, ''),
			COALESCE(error, ''), started_at, completed_at, created_at
		FROM live_recordings WHERE session_id = $1
		ORDER BY started_at DESC LIMIT 1`, sessionID)
	r, err := scanRecording(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// MarkRecordingEnded flags that a stop was requested; the worker finalizes.
func (st *RecordingStore) MarkRecordingEnded(ctx context.Context, egressID string) error {
	_, err := st.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = $2, updated_at = now()
		WHERE egress_id = $1 AND status = $3`,
		egressID, RecordingStatusEnding, RecordingStatusRecording)
	return err
}

// CompleteRecording stores the egress outcome.
func (st *RecordingStore) CompleteRecording(ctx context.Context, egressID string, fileSize int64, storageKey string) error {
	_, err := st.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = $2, file_size = $3, storage_key = $4, completed_at = now(), updated_at = now()
		WHERE egress_id = $1`,
		egressID, RecordingStatusCompleted, fileSize, storageKey)
	return err
}

// FailRecording stores a terminal failure.
func (st *RecordingStore) FailRecording(ctx context.Context, egressID, cause string) error {
	_, err := st.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = $2, error = $3, completed_at = now(), updated_at = now()
		WHERE egress_id = $1`,
		egressID, RecordingStatusFailed, cause)
	return err
}

// MarkConverted links the draft audio episode produced from this recording.
func (st *RecordingStore) MarkConverted(ctx context.Context, egressID, audioID string) error {
	_, err := st.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = $2, audio_id = $3, updated_at = now()
		WHERE egress_id = $1`,
		egressID, RecordingStatusConverted, audioID)
	return err
}

func scanRecording(row pgx.Row) (*Recording, error) {
	var r Recording
	err := row.Scan(&r.ID, &r.SessionID, &r.EgressID, &r.RoomName, &r.Status,
		&r.StorageKey, &r.FileSize, &r.AudioID, &r.Error,
		&r.StartedAt, &r.CompletedAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
