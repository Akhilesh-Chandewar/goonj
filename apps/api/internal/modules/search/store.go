package search

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HitAudio is one audio search result.
type HitAudio struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	CreatorID   string     `json:"creator_id"`
	CreatorName string     `json:"creator_name"`
	Handle      string     `json:"handle"`
	DurationMs  int        `json:"duration_ms"`
	PublishedAt *time.Time `json:"published_at"`
	Rank        float64    `json:"rank"`
}

// HitCreator is one creator search result.
type HitCreator struct {
	ID              string `json:"id"`
	Handle          string `json:"handle"`
	ChannelName     string `json:"channel_name"`
	Tagline         string `json:"tagline"`
	IsLive          bool   `json:"is_live"`
	SubscriberCount int64  `json:"subscriber_count"`
	Rank            float64 `json:"rank"`
}

// Store runs the search queries.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Audio searches public READY audio: full-text match on title+description
// (websearch syntax, quoted phrases supported) OR trigram similarity on the
// title for fuzzy/typo matching. Ranked by ts_rank + trigram similarity.
func (st *Store) Audio(ctx context.Context, q string, limit, offset int) ([]HitAudio, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.category, a.creator_id,
			COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), a.published_at,
			GREATEST(ts_rank(to_tsvector('simple', a.title || ' ' || a.description),
					websearch_to_tsquery('simple', $1)),
				SIMILARITY(a.title, $1)) AS rank
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.deleted_at IS NULL AND a.status = 'READY' AND a.visibility = 'public'
			AND (
				to_tsvector('simple', a.title || ' ' || a.description) @@ websearch_to_tsquery('simple', $1)
				OR a.title % $1
			)
		ORDER BY rank DESC, a.published_at DESC
		LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HitAudio{}
	for rows.Next() {
		var h HitAudio
		if err := rows.Scan(&h.ID, &h.Title, &h.Category, &h.CreatorID,
			&h.CreatorName, &h.Handle, &h.DurationMs, &h.PublishedAt, &h.Rank); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Creators searches channels by name/handle/tagline with trigram matching.
func (st *Store) Creators(ctx context.Context, q string, limit, offset int) ([]HitCreator, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT c.id, c.handle, c.channel_name, c.tagline, c.is_live, c.subscriber_count,
			GREATEST(SIMILARITY(c.channel_name, $1), SIMILARITY(c.handle, $1)) AS rank
		FROM creators c
		WHERE c.deleted_at IS NULL
			AND (
				(c.channel_name || ' ' || c.handle) % $1
				OR c.channel_name ILIKE '%' || $1 || '%'
				OR c.handle ILIKE '%' || $1 || '%'
			)
		ORDER BY rank DESC, c.subscriber_count DESC
		LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HitCreator{}
	for rows.Next() {
		var h HitCreator
		if err := rows.Scan(&h.ID, &h.Handle, &h.ChannelName, &h.Tagline, &h.IsLive,
			&h.SubscriberCount, &h.Rank); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
