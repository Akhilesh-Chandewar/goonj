package engagement

import (
	"context"
	"errors"
	"log/slog"
)

// Service implements engagement business rules.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// Like toggles... no — Like is explicit, Unlike is explicit. Toggle lives in
// the client so a failed request can't leave ambiguous state server-side.
func (s *Service) Like(ctx context.Context, userID, audioID string) (*LikeState, error) {
	if _, err := s.store.AudioLikeCount(ctx, audioID); err != nil {
		return nil, err
	}
	created, err := s.store.LikeAudio(ctx, userID, audioID)
	if err != nil {
		return nil, err
	}
	count, err := s.store.AudioLikeCount(ctx, audioID)
	if err != nil {
		return nil, err
	}
	s.log.Info("audio liked", "audio", audioID, "user", userID, "created", created)
	return &LikeState{Liked: true, Likes: count, Created: created}, nil
}

// LikeState is the post-write like summary.
type LikeState struct {
	Liked   bool  `json:"liked"`
	Likes   int64 `json:"likes"`
	Created bool  `json:"created"`
}

// Unlike removes the caller's like.
func (s *Service) Unlike(ctx context.Context, userID, audioID string) (*LikeState, error) {
	removed, err := s.store.UnlikeAudio(ctx, userID, audioID)
	if err != nil {
		return nil, err
	}
	count, err := s.store.AudioLikeCount(ctx, audioID)
	if err != nil {
		return nil, err
	}
	return &LikeState{Liked: false, Likes: count, Created: removed}, nil
}

// State reports the caller's like state and the total (audio page hydration).
func (s *Service) State(ctx context.Context, userID, audioID string) (*LikeState, error) {
	count, err := s.store.AudioLikeCount(ctx, audioID)
	if err != nil {
		return nil, err
	}
	liked := false
	if userID != "" {
		liked, err = s.store.AudioLikedByUser(ctx, userID, audioID)
		if err != nil {
			return nil, err
		}
	}
	return &LikeState{Liked: liked, Likes: count}, nil
}

// LikedAudio lists the caller's liked, public, READY audio.
func (s *Service) LikedAudio(ctx context.Context, userID string, limit, offset int) ([]map[string]any, error) {
	return s.store.LikedAudio(ctx, userID, limit, offset)
}

// AddComment creates a comment or reply.
func (s *Service) AddComment(ctx context.Context, audioID, userID string, parentID *string, body string) (*Comment, error) {
	if len(body) == 0 || len(body) > 1000 {
		return nil, errors.New("comment must be 1-1000 characters")
	}
	return s.store.InsertComment(ctx, audioID, userID, parentID, body)
}

// Comments returns the thread for one audio.
func (s *Service) Comments(ctx context.Context, audioID, viewerID string, limit int) ([]*Comment, []*Comment, error) {
	return s.store.ListComments(ctx, audioID, viewerID, limit)
}

// DeleteComment soft-deletes the caller's own comment.
func (s *Service) DeleteComment(ctx context.Context, id, userID string) error {
	ok, err := s.store.DeleteComment(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// LikeComment toggles-free comment like.
func (s *Service) LikeComment(ctx context.Context, userID, commentID string) error {
	created, err := s.store.LikeComment(ctx, userID, commentID)
	if err == nil && created {
		s.log.Info("comment liked", "comment", commentID, "user", userID)
	}
	return err
}

// UnlikeComment removes a comment like.
func (s *Service) UnlikeComment(ctx context.Context, userID, commentID string) error {
	_, err := s.store.UnlikeComment(ctx, userID, commentID)
	return err
}
