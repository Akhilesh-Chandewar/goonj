package audio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/storage"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/shared/tasks"
)

// Allowed upload content types (validated server-side, never trust the client).
var allowedMime = map[string]string{
	"audio/mpeg":  ".mp3",
	"audio/wav":   ".wav",
	"audio/x-wav": ".wav",
	"audio/mp4":   ".m4a",
	"audio/aac":   ".aac",
	"audio/flac":  ".flac",
}

var keySafety = regexp.MustCompile(`[^a-z0-9/_-]+`)

// Service implements audio upload and playback flows.
type Service struct {
	store    *Store
	objstore *storage.ObjectStore
	enqueuer *asynq.Client
	log      *slog.Logger
}

func NewService(store *Store, objstore *storage.ObjectStore, enqueuer *asynq.Client, log *slog.Logger) *Service {
	return &Service{store: store, objstore: objstore, enqueuer: enqueuer, log: log}
}

var (	ErrNotCreator      = errors.New("creator profile required")
	ErrInvalidTitle   = errors.New("title must be 1-200 characters")
	ErrUnsupportedMime = errors.New("unsupported audio format")
	ErrInvalidState   = errors.New("audio is not in an editable state")
)

// InitUploadInput requests a presigned direct upload.
type InitUploadInput struct {
	UserID      string
	Title       string
	Description string
	Category    string
	Language    string
	MimeType    string
}

// InitUploadResponse gives the browser everything to upload directly.
type InitUploadResponse struct {
	Audio     *Audio `json:"audio"`
	UploadURL string `json:"upload_url"`
	StorageKey string `json:"storage_key"`
}

// InitUpload creates an UPLOADING audio row and presigns a PUT URL.
func (s *Service) InitUpload(ctx context.Context, in InitUploadInput) (*InitUploadResponse, error) {
	if len(in.Title) == 0 || len(in.Title) > 200 {
		return nil, ErrInvalidTitle
	}
	mime := normalizeMime(in.MimeType)
	ext, ok := allowedMime[mime]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedMime, in.MimeType)
	}

	creatorID, err := s.store.CreatorIDByUserID(ctx, in.UserID)
	if err != nil {
		return nil, ErrNotCreator
	}

	a, err := s.store.Create(ctx, creatorID, in.Title, in.Description, in.Category, in.Language, "upload", nil)
	if err != nil {
		return nil, err
	}

	key := objectKey(a.ID, ext)
	url, err := s.objstore.PresignedPutURL(ctx, key, mime, 30*time.Minute)
	if err != nil {
		return nil, err
	}

	// Persist the original's storage key now so CompleteUpload verifies the
	// exact object the browser was told to write.
	if err := s.store.AddFile(ctx, a.ID, AudioFile{
		Quality:    "original",
		StorageKey: key,
		MimeType:   mime,
	}); err != nil {
		return nil, err
	}

	return &InitUploadResponse{Audio: a, UploadURL: url, StorageKey: key}, nil
}

// CompleteUpload verifies the object landed, then queues FFmpeg processing.
func (s *Service) CompleteUpload(ctx context.Context, audioID, userID string) (*Audio, error) {
	a, err := s.store.Get(ctx, audioID)
	if err != nil {
		return nil, err
	}
	if a.Status != StatusUploading {
		return nil, ErrInvalidState
	}

	creatorID, err := s.store.CreatorIDByUserID(ctx, userID)
	if err != nil || creatorID != a.CreatorID {
		return nil, ErrNotCreator
	}

	// Find the original's storage key recorded at init time.
	files, err := s.store.Files(ctx, audioID)
	if err != nil {
		return nil, err
	}
	var originalKey string
	for _, f := range files {
		if f.Quality == "original" {
			originalKey = f.StorageKey
			break
		}
	}
	if originalKey == "" {
		return nil, errors.New("upload was not initialized")
	}

	// The browser must have finished the PUT; verify the object exists.
	if _, _, err := s.objstore.Stat(ctx, originalKey); err != nil {
		return nil, fmt.Errorf("uploaded object not found: %w", err)
	}

	if err := s.store.SetStatus(ctx, audioID, StatusProcessing); err != nil {
		return nil, err
	}

	task, err := tasks.NewProcessAudio(tasks.ProcessAudioPayload{
		AudioID:    audioID,
		StorageKey: originalKey,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.enqueuer.EnqueueContext(ctx, task); err != nil {
		_ = s.store.SetStatus(ctx, audioID, StatusFailed)
		return nil, fmt.Errorf("enqueue processing: %w", err)
	}

	s.log.Info("audio queued for processing", slog.String("audio", audioID))
	return s.store.Get(ctx, audioID)
}

// Playback describes playable URLs for one audio row.
type Playback struct {
	Audio    *Audio       `json:"audio"`
	Sources  []PlaybackSource `json:"sources"`
	Waveform []int        `json:"waveform,omitempty"`
}

// PlaybackSource is one quality variant with a presigned URL.
type PlaybackSource struct {
	Quality     string `json:"quality"`
	URL         string `json:"url"`
	BitrateKbps int    `json:"bitrate_kbps"`
	MimeType    string `json:"mime_type"`
}

// PlaybackFor assembles presigned playback URLs for READY audio.
func (s *Service) PlaybackFor(ctx context.Context, id string) (*Playback, error) {
	a, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Status != StatusReady {
		return nil, fmt.Errorf("audio is %s, not playable yet", a.Status)
	}

	files, err := s.store.Files(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no processed files available")
	}

	sources := make([]PlaybackSource, 0, len(files))
	for _, f := range files {
		if f.Quality == "original" {
			continue // originals are inputs to the pipeline, not playable variants
		}
		url, err := s.objstore.PresignedGetURL(ctx, f.StorageKey, 2*time.Hour)
		if err != nil {
			return nil, err
		}
		sources = append(sources, PlaybackSource{
			Quality:     f.Quality,
			URL:         url,
			BitrateKbps: f.BitrateKbps,
			MimeType:    f.MimeType,
		})
	}

	waveform, err := s.store.Waveform(ctx, id)
	if err != nil {
		s.log.Warn("waveform load failed", slog.String("audio", id), slog.Any("error", err))
	}
	return &Playback{Audio: a, Sources: sources, Waveform: waveform}, nil
}

// Feed lists public READY audio.
func (s *Service) Feed(ctx context.Context, category string, limit, offset int) ([]Audio, error) {
	return s.store.Feed(ctx, category, limit, offset)
}

// ListByCreator lists the studio's content.
func (s *Service) ListByCreator(ctx context.Context, userID string) ([]Audio, error) {
	creatorID, err := s.store.CreatorIDByUserID(ctx, userID)
	if err != nil {
		return nil, ErrNotCreator
	}
	return s.store.ListByCreator(ctx, creatorID)
}

// Delete soft-fails processing output for now (row stays for audit).
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	a, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	creatorID, err := s.store.CreatorIDByUserID(ctx, userID)
	if err != nil || creatorID != a.CreatorID {
		return ErrNotCreator
	}
	return s.store.SetStatus(ctx, id, StatusPrivate)
}

func normalizeMime(m string) string {
	switch m {
	case "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return "audio/wav"
	case "audio/x-m4a", "audio/m4a":
		return "audio/mp4"
	default:
		return m
	}
}

// objectKey builds the canonical storage key for a new upload.
func objectKey(audioID, ext string) string {
	return fmt.Sprintf("audio/originals/%s%s", audioID, ext)
}
