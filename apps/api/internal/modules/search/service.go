package search

import (
	"context"
	"log/slog"
	"strings"
)

// Service validates queries and combines result sets.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

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
	return &Results{Query: q, Audio: audio, Creators: creators}, nil
}
