package social

import (
	"context"
	"log/slog"
)

// Service implements the follow graph business rules.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// Follow adds a follow edge and returns the creator's subscriber count.
func (s *Service) Follow(ctx context.Context, userID, creatorID string) (*FollowState, error) {
	if _, err := s.store.GetCreator(ctx, creatorID); err != nil {
		return nil, err
	}
	if err := s.store.Follow(ctx, userID, creatorID); err != nil {
		return nil, err
	}
	c, err := s.store.GetCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	s.log.Info("creator followed", "creator", creatorID, "user", userID)
	return &FollowState{Following: true, Subscribers: c.SubscriberCount}, nil
}

// FollowState is the post-write follow summary.
type FollowState struct {
	Following  bool  `json:"following"`
	Subscribers int64 `json:"subscribers"`
}

// Unfollow removes the follow edge.
func (s *Service) Unfollow(ctx context.Context, userID, creatorID string) (*FollowState, error) {
	if err := s.store.Unfollow(ctx, userID, creatorID); err != nil {
		return nil, err
	}
	c, err := s.store.GetCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	return &FollowState{Following: false, Subscribers: c.SubscriberCount}, nil
}

// CreatorPage returns a channel with its public episodes and the viewer's
// follow state.
func (s *Service) CreatorPage(ctx context.Context, creatorID, viewerID string, limit, offset int) (map[string]any, error) {
	c, err := s.store.GetCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	episodes, err := s.store.CreatorAudio(ctx, creatorID, limit, offset)
	if err != nil {
		return nil, err
	}
	if episodes == nil {
		episodes = []FeedItem{}
	}
	following := false
	if viewerID != "" {
		following, err = s.store.IsFollowing(ctx, viewerID, creatorID)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"creator":   c,
		"episodes":  episodes,
		"following": following,
	}, nil
}

// Followed lists creators the viewer follows.
func (s *Service) Followed(ctx context.Context, userID string) ([]Creator, error) {
	return s.store.ListFollowed(ctx, userID)
}

// Feed returns the subscriptions feed for the viewer.
func (s *Service) Feed(ctx context.Context, userID string, limit, offset int) ([]FeedItem, error) {
	return s.store.SubscriptionsFeed(ctx, userID, limit, offset)
}
