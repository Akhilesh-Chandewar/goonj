package search

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/ai"
)

// ---------- Recommendations (pgvector more-like-this) ----------

// RecoStore finds similar episodes via transcript embeddings. The embedder is
// shared with semantic search so the model filter matches existing rows.
type RecoStore struct {
	pool     *pgxpool.Pool
	embedder ai.Embedder
}

// NewRecoStore builds the recommendation store.
func NewRecoStore(pool *pgxpool.Pool) *RecoStore {
	return &RecoStore{pool: pool, embedder: ai.DefaultEmbedder()}
}

// SimilarTo returns public READY episodes whose best chunk embedding is
// closest to the given episode's centroid, excluding the episode itself.
// Falls back gracefully: episodes without embeddings simply never appear.
func (s *RecoStore) SimilarTo(ctx context.Context, audioID string, limit int) ([]HitAudio, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		WITH src AS (
			SELECT avg(e.embedding) AS centroid
			FROM chunk_embeddings e
			JOIN transcript_chunks c ON c.id = e.chunk_id
			WHERE c.audio_id = $1 AND e.model = $2
		)
		SELECT * FROM (
			SELECT DISTINCT ON (a.id)
				a.id, a.title, a.category, a.creator_id,
				COALESCE(c2.channel_name, '') AS creator_name,
				COALESCE(p.username, '')     AS handle,
				COALESCE(m.duration_ms, 0)   AS duration_ms,
				a.published_at,
				1 - (e.embedding <=> (SELECT centroid FROM src)) AS rank
			FROM chunk_embeddings e
			JOIN transcript_chunks ch ON ch.id = e.chunk_id
			JOIN audio a ON a.id = ch.audio_id
			LEFT JOIN audio_metadata m ON m.audio_id = a.id
			LEFT JOIN creators c2 ON c2.id = a.creator_id
			LEFT JOIN profiles p ON p.user_id = c2.user_id
			WHERE e.model = $2
				AND a.id <> $1
				AND a.deleted_at IS NULL
				AND a.status = 'READY'
				AND a.visibility = 'public'
			ORDER BY a.id, e.embedding <=> (SELECT centroid FROM src)
		) best
		ORDER BY rank DESC
		LIMIT $3`,
		audioID, s.embedder.Name(), limit)
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

// ---------- Trending (Redis ZSET, time-decayed) ----------

// TrendingKeys holds the ZSET keys and score weights.
// Score = likes*4 + comments*2 + recent listens*1, recomputed periodically;
// the ZSET is trimmed to the top 500 so memory stays bounded.
const (
	trendingKey     = "goonj:trending:audio"
	trendingZsetSize = 500
)

// RecomputeTrending rebuilds the trending ZSET from Postgres engagement
// counts, weighting recent activity (recent windows count more).
// Called by a worker job; the API only reads.
func RecomputeTrending(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) error {
	// Weighted score: engagement in the last 3 days counts triple, last 7
	// days double, everything older counts once.
	rows, err := pool.Query(ctx, `
		SELECT a.id::text,
			(l.recent3 * 4 + l.recent7 * 4 + l.old * 4) +
			(c.recent3 * 2 + c.recent7 * 2 + c.old * 2) +
			(h.recent3 + h.recent7 + h.old) AS score
		FROM audio a
		LEFT JOIN LATERAL (
			SELECT
				count(*) FILTER (WHERE l.created_at > now() - interval '3 days') AS recent3,
				count(*) FILTER (WHERE l.created_at > now() - interval '7 days') AS recent7,
				count(*) FILTER (WHERE l.created_at <= now() - interval '7 days') AS old
			FROM audio_likes l WHERE l.audio_id = a.id
		) l ON true
		LEFT JOIN LATERAL (
			SELECT
				count(*) FILTER (WHERE c.created_at > now() - interval '3 days') AS recent3,
				count(*) FILTER (WHERE c.created_at > now() - interval '7 days') AS recent7,
				count(*) FILTER (WHERE c.created_at <= now() - interval '7 days') AS old
			FROM comments c WHERE c.audio_id = a.id AND c.deleted_at IS NULL
		) c ON true
		LEFT JOIN LATERAL (
			SELECT
				count(*) FILTER (WHERE h.updated_at > now() - interval '3 days') AS recent3,
				count(*) FILTER (WHERE h.updated_at > now() - interval '7 days') AS recent7,
				count(*) FILTER (WHERE h.updated_at <= now() - interval '7 days') AS old
			FROM listening_history h WHERE h.audio_id = a.id
		) h ON true
		WHERE a.deleted_at IS NULL AND a.status = 'READY' AND a.visibility = 'public'
		ORDER BY score DESC
		LIMIT 500`)
	if err != nil {
		return err
	}
	defer rows.Close()

	members := make([]redis.Z, 0, 128)
	for rows.Next() {
		var id string
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			return err
		}
		if score > 0 {
			members = append(members, redis.Z{Score: score, Member: id})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Swap atomically-ish: replace the whole ZSET in a pipeline.
	pipe := rdb.Pipeline()
	pipe.Del(ctx, trendingKey)
	if len(members) > 0 {
		pipe.ZAdd(ctx, trendingKey, members...)
	}
	pipe.Expire(ctx, trendingKey, 2*time.Hour) // safety TTL if recomputes stop
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	log.Info("trending recomputed", slog.Int("items", len(members)))
	return nil
}

// Trending returns the top audio ids from the ZSET.
func TrendingIDs(ctx context.Context, rdb *redis.Client, limit int) ([]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ids, err := rdb.ZRevRange(ctx, trendingKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// HydrateAudio loads audio rows by ids, preserving the given order.
func HydrateAudio(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]AudioHit, error) {
	if len(ids) == 0 {
		return []AudioHit{}, nil
	}
	out := make([]AudioHit, 0, len(ids))
	// Fetch with a positional IN list (ids come from our own ZSET).
	rows, err := pool.Query(ctx, `
		SELECT a.id::text, a.title, a.category, a.creator_id,
			COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), a.published_at
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.id = ANY($1::uuid[]) AND a.deleted_at IS NULL
			AND a.status = 'READY' AND a.visibility = 'public'`,
		arrayParam(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[string]AudioHit{}
	for rows.Next() {
		var h AudioHit
		if err := rows.Scan(&h.ID, &h.Title, &h.Category, &h.CreatorID,
			&h.CreatorName, &h.Handle, &h.DurationMs, &h.PublishedAt); err != nil {
			return nil, err
		}
		byID[h.ID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if h, ok := byID[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// AudioHit is the trending payload shape (no rank).
type AudioHit struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	CreatorID   string     `json:"creator_id"`
	CreatorName string     `json:"creator_name"`
	Handle      string     `json:"handle"`
	DurationMs  int        `json:"duration_ms"`
	PublishedAt *time.Time `json:"published_at"`
}

func arrayParam(ids []string) string {
	// pgx accepts a Postgres array literal for ::uuid[] casts.
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out + "}"
}

// envDuration reads a duration env var (used for the recompute schedule).
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}


