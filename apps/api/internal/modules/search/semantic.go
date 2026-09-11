package search

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/ai"
)

// DefaultMinSimilarity is the cosine-similarity floor for semantic hits.
// ANN has no notion of "relevant enough" — without a floor every query
// matches something (one episode in the DB means every query finds it).
// Hash-embedded noise pairs measure ~0.05-0.1; real matches 0.3+.
const DefaultMinSimilarity = 0.15

// SemanticStore embeds the query and runs pgvector ANN search over transcript
// chunks. Vectors are only compared against rows produced by the same model
// (chunk_embeddings.model) — a hash-embedded query must never be ranked
// against OpenAI-embedded chunks.
type SemanticStore struct {
	pool     *pgxpool.Pool
	embedder ai.Embedder
	// minSim drops hits below this cosine similarity (SEMANTIC_MIN_SIMILARITY).
	minSim float64
}

// NewSemanticStore picks the deployment-wide embedder (same helper the
// embedding worker uses, so query and document vectors always agree).
func NewSemanticStore(pool *pgxpool.Pool) *SemanticStore {
	minSim := DefaultMinSimilarity
	if v := os.Getenv("SEMANTIC_MIN_SIMILARITY"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			minSim = f
		} else {
			slog.Warn("invalid SEMANTIC_MIN_SIMILARITY; using default",
				slog.String("value", v), slog.Float64("default", minSim))
		}
	}
	return &SemanticStore{pool: pool, embedder: ai.DefaultEmbedder(), minSim: minSim}
}

// BestChunksPerAudio returns the public READY audio whose best-matching
// transcript chunk is closest to the query vector. DISTINCT ON collapses each
// audio's chunks to its single nearest chunk; the outer query ranks episodes.
func (s *SemanticStore) BestChunksPerAudio(ctx context.Context, qvec []float32, limit, offset int) ([]HitAudio, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT * FROM (
			SELECT DISTINCT ON (a.id)
				a.id, a.title, a.category, a.creator_id,
				COALESCE(c.channel_name, '') AS creator_name,
				COALESCE(p.username, '')     AS handle,
				COALESCE(m.duration_ms, 0)   AS duration_ms,
				a.published_at,
				1 - (e.embedding <=> $2::vector) AS rank
			FROM chunk_embeddings e
			JOIN transcript_chunks ch ON ch.id = e.chunk_id
			JOIN audio a ON a.id = ch.audio_id
			LEFT JOIN audio_metadata m ON m.audio_id = a.id
			LEFT JOIN creators c ON c.id = a.creator_id
			LEFT JOIN profiles p ON p.user_id = c.user_id
			WHERE e.model = $3
				AND 1 - (e.embedding <=> $2::vector) >= $4
				AND a.deleted_at IS NULL
				AND a.status = 'READY'
				AND a.visibility = 'public'
			ORDER BY a.id, e.embedding <=> $2::vector
		) best
		ORDER BY rank DESC, published_at DESC NULLS LAST
		LIMIT $1 OFFSET $5`,
		limit, ai.VectorLiteral(qvec), s.embedder.Name(), s.minSim, offset)
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

// Semantic appends content-based hits (meaning of the transcript) to the
// keyword results. Failures degrade silently: semantic search is additive
// and its absence must never break the main search endpoint.
func (s *Service) Semantic(ctx context.Context, q string, limit, offset int, exclude map[string]bool) ([]HitAudio, error) {
	if s.semantic == nil {
		return nil, nil
	}
	vecs, err := s.semantic.embedder.Embed(ctx, []string{q})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed query: got %d vectors", len(vecs))
	}
	hits, err := s.semantic.BestChunksPerAudio(ctx, vecs[0], limit, offset)
	if err != nil {
		return nil, err
	}
	filtered := hits[:0]
	for _, h := range hits {
		if !exclude[h.ID] {
			filtered = append(filtered, h)
		}
	}
	return filtered, nil
}
