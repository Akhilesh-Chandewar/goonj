// Package live implements the Go Live core: session lifecycle, LiveKit-backed
// audio rooms, Redis presence, room event fanout, and egress recording that
// feeds the Live→Saved pipeline. The streaming provider sits behind the
// Streamer/Recorder interfaces (PLAN §5) so it can be swapped without
// touching business logic.
package live

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Config carries the streaming provider settings.
type Config struct {
	StreamingHost string // LiveKit endpoint used server-side (tokens, egress)
	// StreamingClientURL is the endpoint browsers receive in ws_url; empty
	// falls back to StreamingHost. Set to the public wss:// URL in prod so
	// TLS terminates correctly (TURN is server-side rtc.turn config).
	StreamingClientURL string
	StreamingAPIKey    string
	StreamingSecret    string
	Recorder           Recorder           // nil disables recording (streams still run)
	Finalizer          RecordingFinalizer // nil skips finalize enqueue
	// Notifier fans out "went live" notifications to followers (optional).
	Notifier LiveNotifier
}

// LiveNotifier is the notifications hook (kept as an interface so live never
// imports the notifications module directly).
type LiveNotifier interface {
	NotifyLiveStarted(ctx context.Context, creatorID, creatorName, sessionID, title string)
}

// Module bundles the live module's collaborators.
type Module struct {
	Service    *Service
	Handlers   *Handlers
	Store      *Store
	Recordings *RecordingStore
	Streamer   Streamer
}

// NewModule wires the live module together.
func NewModule(pool *pgxpool.Pool, rdb *redis.Client, cfg Config, log *slog.Logger) *Module {
	store := NewStore(pool)
	recordings := NewRecordingStore(pool)
	presence := NewPresence(rdb, log)
	streamer := NewLiveKitStreamer(cfg.StreamingHost, cfg.StreamingClientURL, cfg.StreamingAPIKey, cfg.StreamingSecret)
	service := NewService(store, recordings, presence, streamer, cfg.Recorder, cfg.Finalizer, log)
	service.notifier = cfg.Notifier

	return &Module{
		Service:    service,
		Handlers:   NewHandlers(service, log),
		Store:      store,
		Recordings: recordings,
		Streamer:   streamer,
	}
}

// Router returns the live routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
