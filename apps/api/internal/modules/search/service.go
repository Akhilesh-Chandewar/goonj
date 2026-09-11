package search

import (
	"context"
	"log/slog"
	"strings"
)

// Service validates queries and combines result sets: keyword (FTS +
// trigram) plus optional semantic hits (pgvector over transcript chunks).
type Service struct {
	store    *Store
	semantic *SemanticStore
	log      *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// NewServiceWithSemantic attaches pgvector semantic search. The semantic
// store's embedder must match the one the embedding worker uses (both call
// ai.DefaultEmbedder, so they agree by construction).

// Results bundles audio + creator hits for the unified /search endpoint.
type Results struct {
	Query    string       `json:"query"`
	Audio    []HitAudio   `json:"audio"`
	Creators []HitCreator `json:"creators"`
}

// All runs both searches. Whitespace-only queries return empty results
// rather than a full-table scan disguised as "everything".
func (s *Service) All(ctx context.Context, q string, limit, offset int) (*Results, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return &Results{Query: "", Audio: []HitAudio{}, Creators: []HitCreator{}}, nil
	}
	if len(q) > 200 {
		q = q[:200]
	}

	audio, err := s.store.Audio(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	creators, err := s.store.Creators(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}

	// Semantic pass: fetch extra candidates, drop audio already found by
	// the keyword pass, and blend the remainder after the keyword hits.
	exclude := make(map[string]bool, len(audio))
	for _, a := range audio {
		exclude[a.ID] = true
	}
	semantic, err := s.Semantic(ctx, q, limit, offset, exclude)
	if err != nil {
		s.log.Warn("semantic search failed (continuing with keyword results)",
			slog.Any("error", err))
		semantic = nil
	}

	return &Results{Query: q, Audio: append(audio, semantic...), Creators: creators}, nil
}
