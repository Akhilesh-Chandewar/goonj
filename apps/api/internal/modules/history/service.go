package history

import (
	"context"
	"log/slog"
)

// Service implements listening-history rules: clamping, completion, and the
// resume handoff to the player.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// maxPosition caps nonsense client values (8 hours).
const maxPosition = 8 * 60 * 60 * 1000

// Report records listening progress. Position is clamped; "completed" is
// decided server-side (>= 95% of duration) and never trusted from the client.
func (s *Service) Report(ctx context.Context, userID, audioID string, positionMs, durationMs int) error {
	if positionMs < 0 {
		positionMs = 0
	}
	if positionMs > maxPosition {
		positionMs = maxPosition
	}
	completed := durationMs > 0 && positionMs >= durationMs*95/100
	return s.store.Upsert(ctx, userID, audioID, positionMs, completed)
}

// Recent lists the user's listening history.
func (s *Service) Recent(ctx context.Context, userID string, limit, offset int) ([]Entry, error) {
	return s.store.Recent(ctx, userID, limit, offset)
}

// Resume returns the saved position for an audio row (0 when fresh).
func (s *Service) Resume(ctx context.Context, userID, audioID string) (int, bool) {
	pos, completed, err := s.store.Resume(ctx, userID, audioID)
	if err != nil {
		s.log.Warn("resume lookup failed", "user", userID, "audio", audioID, "error", err)
		return 0, false
	}
	return pos, completed
}
