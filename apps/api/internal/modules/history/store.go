package history

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one listening-history row.
type Entry struct {
	AudioID    string     `json:"audio_id"`
	Title      string     `json:"title"`
	Category   string     `json:"category"`
	CreatorID  string     `json:"creator_id"`
	CreatorName string    `json:"creator_name"`
	Handle     string     `json:"handle"`
	DurationMs int        `json:"duration_ms"`
	PositionMs int        `json:"position_ms"`
	Completed  bool       `json:"completed"`
	PlayCount  int        `json:"play_count"`
	UpdatedAt  time.Time  `json:"updated_at"`
	PublishedAt *time.Time `json:"published_at"`
}

// Store is the Postgres repository for listening history.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Upsert records progress. One row per (user, audio): position and counts
// merge instead of append, so a binge session writes rows, not event spam.
func (st *Store) Upsert(ctx context.Context, userID, audioID string, positionMs int, completed bool) error {
	_, err := st.pool.Exec(ctx, `
		INSERT INTO listening_history (user_id, audio_id, position_ms, completed)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, audio_id) DO UPDATE
		SET position_ms = EXCLUDED.position_ms,
			completed = EXCLUDED.completed OR listening_history.completed,
			play_count = CASE WHEN EXCLUDED.position_ms < listening_history.position_ms - 60000
				THEN listening_history.play_count + 1
				ELSE listening_history.play_count END,
			updated_at = now()`,
		userID, audioID, positionMs, completed)
	return err
}

// Recent lists the user's history, most recent first.
func (st *Store) Recent(ctx context.Context, userID string, limit, offset int) ([]Entry, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.category, a.creator_id,
			COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), h.position_ms, h.completed, h.play_count,
			h.updated_at, a.published_at
		FROM listening_history h
		JOIN audio a ON a.id = h.audio_id
			AND a.status = 'READY' AND a.visibility = 'public' AND a.deleted_at IS NULL
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE h.user_id = $1
		ORDER BY h.updated_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.AudioID, &e.Title, &e.Category, &e.CreatorID,
			&e.CreatorName, &e.Handle, &e.DurationMs, &e.PositionMs, &e.Completed,
			&e.PlayCount, &e.UpdatedAt, &e.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Resume returns the caller's saved position for one audio row (0 if none).
func (st *Store) Resume(ctx context.Context, userID, audioID string) (int, bool, error) {
	var pos int
	var completed bool
	err := st.pool.QueryRow(ctx,
		`SELECT position_ms, completed FROM listening_history WHERE user_id = $1 AND audio_id = $2`,
		userID, audioID).Scan(&pos, &completed)
	if err != nil {
		// No row yet is the common fresh-start case.
		return 0, false, nil
	}
	return pos, completed, nil
}
