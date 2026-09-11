// Package audio implements on-demand audio content: presigned direct
// uploads, FFmpeg processing via the worker queue, and presigned playback.
package audio

import (
	"log/slog"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/storage"
	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the audio module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
	// Transcripts serves /audio/{id}/transcript; nil only when the module
	// itself is disabled (no object storage in that case anyway).
	Transcripts *TranscriptStore
}

// NewModule wires the audio module together.
func NewModule(pool *pgxpool.Pool, objstore *storage.ObjectStore, queue *asynq.Client, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, objstore, queue, log)
	transcripts := NewTranscriptStore(pool)
	return &Module{
		Service:     service,
		Handlers:    NewHandlers(service, transcripts, log),
		Transcripts: transcripts,
	}
}

// Router returns the audio routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
