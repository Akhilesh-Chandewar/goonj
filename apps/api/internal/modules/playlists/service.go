package playlists

import (
	"context"
	"log/slog"
)

// Service implements playlist business rules.
type Service struct {
	store *Store
	log   *slog.Logger
}

func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// Create makes a new playlist (title validated in handlers via request size).
func (s *Service) Create(ctx context.Context, userID, title, description, visibility string) (*Playlist, error) {
	if visibility != "public" && visibility != "private" {
		visibility = "private"
	}
	return s.store.Create(ctx, userID, title, description, visibility)
}

// Get returns one playlist readable by the caller, with its items.
func (s *Service) Get(ctx context.Context, id, viewerID string) (*Playlist, error) {
	pl, err := s.store.Get(ctx, id, viewerID)
	if err != nil {
		return nil, err
	}
	items, err := s.store.Items(ctx, id)
	if err != nil {
		return nil, err
	}
	pl.Items = items
	return pl, nil
}

// Mine lists the caller's playlists (metadata only, no items).
func (s *Service) Mine(ctx context.Context, userID string) ([]Playlist, error) {
	return s.store.ListByOwner(ctx, userID)
}

// Update rewrites an owned playlist's metadata.
func (s *Service) Update(ctx context.Context, id, userID, title, description, visibility string) error {
	if title == "" || len(title) > 120 {
		return errInvalidTitle
	}
	if visibility != "public" && visibility != "private" {
		visibility = "private"
	}
	return s.store.UpdateMeta(ctx, id, userID, title, description, visibility)
}

// Delete removes an owned playlist.
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	return s.store.Delete(ctx, id, userID)
}

// AddAudio appends an audio to an owned playlist.
func (s *Service) AddAudio(ctx context.Context, id, userID, audioID string) (bool, error) {
	if _, err := s.store.Get(ctx, id, userID); err != nil {
		return false, err
	}
	added, err := s.store.AddItem(ctx, id, audioID)
	if err != nil {
		return false, err
	}
	if added {
		s.log.Info("playlist item added", "playlist", id, "audio", audioID)
	}
	return added, nil
}

// RemoveAudio removes an audio from an owned playlist.
func (s *Service) RemoveAudio(ctx context.Context, id, userID, audioID string) (bool, error) {
	if _, err := s.store.Get(ctx, id, userID); err != nil {
		return false, err
	}
	return s.store.RemoveItem(ctx, id, audioID)
}
