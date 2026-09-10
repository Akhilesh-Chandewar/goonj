package audio

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/live"
)

// Audio statuses.
const (
	StatusUploading  = "UPLOADING"
	StatusProcessing = "PROCESSING"
	StatusReady      = "READY"
	StatusFailed     = "FAILED"
	StatusScheduled  = "SCHEDULED"
	StatusPrivate    = "PRIVATE"
)

// Audio is a piece of published or in-progress audio content.
type Audio struct {
	ID              string     `json:"id"`
	CreatorID       string     `json:"creator_id"`
	CreatorName     string     `json:"creator_name,omitempty"`
	Handle          string     `json:"handle,omitempty"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Category        string     `json:"category"`
	Language        string     `json:"language"`
	Status          string     `json:"status"`
	Visibility      string     `json:"visibility"`
	Source          string     `json:"source"`
	SourceSessionID *string    `json:"source_session_id,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	DurationMs      int        `json:"duration_ms"`
	CreatedAt       time.Time  `json:"created_at"`
}

// AudioFile is one quality variant of the processed audio.
type AudioFile struct {
	Quality     string `json:"quality"`
	StorageKey  string `json:"-"`
	BitrateKbps int    `json:"bitrate_kbps"`
	SizeBytes   int64  `json:"size_bytes"`
	MimeType    string `json:"mime_type"`
}

// ErrNotFound when the audio does not exist.
var ErrNotFound = errors.New("audio not found")

// Store is the Postgres repository for audio content.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const audioColumns = `
	a.id, a.creator_id, a.title, a.description, a.category, a.language,
	a.status, a.visibility, a.source, a.source_session_id, a.published_at,
	COALESCE(m.duration_ms, 0), a.created_at,
	COALESCE(c.channel_name, ''), COALESCE(p.username, '')`

func scanAudio(row pgx.Row) (*Audio, error) {
	var a Audio
	err := row.Scan(&a.ID, &a.CreatorID, &a.Title, &a.Description, &a.Category,
		&a.Language, &a.Status, &a.Visibility, &a.Source, &a.SourceSessionID,
		&a.PublishedAt, &a.DurationMs, &a.CreatedAt, &a.CreatorName, &a.Handle)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Create inserts an audio row in UPLOADING state.
func (st *Store) Create(ctx context.Context, creatorID, title, description, category, language, source string, sourceSessionID *string) (*Audio, error) {
	row := st.pool.QueryRow(ctx, `
		INSERT INTO audio (creator_id, title, description, category, language, source, source_session_id, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, creator_id, title, description, category, language,
			status, visibility, source, source_session_id, published_at,
			0, created_at, '', ''`,
		creatorID, title, description, category, language, source, sourceSessionID, StatusUploading)
	return scanAudio(row)
}

// CreateLiveRecording inserts a draft (PRIVATE visibility) audio row sourced
// from an ended live session. The creator publishes it explicitly.
func (st *Store) CreateLiveRecording(ctx context.Context, creatorID, sessionID, title, description, category string) (*Audio, error) {
	sid := sessionID
	row := st.pool.QueryRow(ctx, `
		INSERT INTO audio (creator_id, title, description, category, source, source_session_id, status, visibility)
		VALUES ($1, $2, $3, $4, 'live_recording', $5, $6, 'private')
		RETURNING id, creator_id, title, description, category, language,
			status, visibility, source, source_session_id, published_at,
			0, created_at, '', ''`,
		creatorID, title, description, category, &sid, StatusProcessing)
	return scanAudio(row)
}

// UpdateMeta rewrites title/description/category (draft editing).
func (st *Store) UpdateMeta(ctx context.Context, id, title, description, category string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE audio
		SET title = $2, description = $3, category = $4, updated_at = now()
		WHERE id = $1`, id, title, description, category)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetVisibility toggles public/private without touching status.
func (st *Store) SetVisibility(ctx context.Context, id, visibility string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE audio SET visibility = $2, updated_at = now() WHERE id = $1`, id, visibility)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Publish flips a READY audio row public and stamps published_at once.
func (st *Store) Publish(ctx context.Context, id string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE audio
		SET status = $2, visibility = 'public',
			published_at = COALESCE(published_at, now()), updated_at = now()
		WHERE id = $1`, id, StatusReady)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Get fetches one audio row.
func (st *Store) Get(ctx context.Context, id string) (*Audio, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT `+audioColumns+`
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.id = $1`, id)
	a, err := scanAudio(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// Feed returns public READY audio, newest first.
func (st *Store) Feed(ctx context.Context, category string, limit, offset int) ([]Audio, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	where := `WHERE a.status = 'READY' AND a.visibility = 'public'`
	args := []any{limit, offset}
	if category != "" && category != "all" {
		where += ` AND a.category = $3`
		args = append(args, category)
	}
	rows, err := st.pool.Query(ctx, `
		SELECT `+audioColumns+`
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		`+where+`
		ORDER BY a.published_at DESC NULLS LAST
		LIMIT $1 OFFSET $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Audio
	for rows.Next() {
		a, err := scanAudio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ListByCreator returns the studio content list.
func (st *Store) ListByCreator(ctx context.Context, creatorID string) ([]Audio, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT `+audioColumns+`
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.creator_id = $1
		ORDER BY a.created_at DESC
		LIMIT 100`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Audio
	for rows.Next() {
		a, err := scanAudio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// SetStatus moves audio through its lifecycle.
func (st *Store) SetStatus(ctx context.Context, id, status string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE audio SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkReady sets status READY, publishes, and stores processing metadata.
func (st *Store) MarkReady(ctx context.Context, id string, durationMs int, waveform []byte, sizeBytes int64, mimeType string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE audio SET status = $2, published_at = COALESCE(published_at, now()), updated_at = now()
		WHERE id = $1`, id, StatusReady)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	_, err = st.pool.Exec(ctx, `
		INSERT INTO audio_metadata (audio_id, duration_ms, waveform, size_bytes, mime_type)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (audio_id) DO UPDATE
		SET duration_ms = EXCLUDED.duration_ms,
			waveform = EXCLUDED.waveform,
			size_bytes = EXCLUDED.size_bytes,
			mime_type = EXCLUDED.mime_type,
			updated_at = now()`,
		id, durationMs, waveform, sizeBytes, mimeType)
	return err
}

// AddFile records a processed quality variant.
func (st *Store) AddFile(ctx context.Context, audioID string, f AudioFile) error {
	_, err := st.pool.Exec(ctx, `
		INSERT INTO audio_files (audio_id, quality, storage_key, bitrate_kbps, size_bytes, mime_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (audio_id, quality) DO UPDATE
		SET storage_key = EXCLUDED.storage_key,
			bitrate_kbps = EXCLUDED.bitrate_kbps,
			size_bytes = EXCLUDED.size_bytes,
			mime_type = EXCLUDED.mime_type`,
		audioID, f.Quality, f.StorageKey, f.BitrateKbps, f.SizeBytes, f.MimeType)
	return err
}

// Files returns all quality variants for an audio row.
func (st *Store) Files(ctx context.Context, audioID string) ([]AudioFile, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT quality, storage_key, bitrate_kbps, size_bytes, mime_type
		FROM audio_files WHERE audio_id = $1
		ORDER BY CASE quality WHEN 'low' THEN 0 WHEN 'medium' THEN 1 WHEN 'high' THEN 2 ELSE 3 END`,
		audioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AudioFile
	for rows.Next() {
		var f AudioFile
		if err := rows.Scan(&f.Quality, &f.StorageKey, &f.BitrateKbps, &f.SizeBytes, &f.MimeType); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Waveform returns the stored peak array for an audio row (nil if absent).
func (st *Store) Waveform(ctx context.Context, audioID string) ([]int, error) {
	var raw []byte
	err := st.pool.QueryRow(ctx,
		`SELECT waveform FROM audio_metadata WHERE audio_id = $1`, audioID).
		Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || raw == nil {
			return nil, nil
		}
		return nil, err
	}
	var peaks []int
	if err := json.Unmarshal(raw, &peaks); err != nil {
		return nil, nil // corrupt waveform is non-fatal
	}
	return peaks, nil
}

// CreatorIDByUserID resolves the channel id for a user (shared with live).
func (st *Store) CreatorIDByUserID(ctx context.Context, userID string) (string, error) {
	var id string
	err := st.pool.QueryRow(ctx,
		`SELECT id FROM creators WHERE user_id = $1 AND deleted_at IS NULL`, userID).
		Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", live.ErrNotFound
	}
	return id, err
}

var _ = StatusScheduled
