// Package live implements the Go Live core: session lifecycle, LiveKit-backed
// audio rooms, Redis presence, and room event fanout. The streaming provider
// sits behind the Streamer interface (PLAN §53) so it can be swapped without
// touching business logic.
package live

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Config carries the streaming provider settings.
type Config struct {
	StreamingHost    string // LiveKit ws endpoint shown to clients
	StreamingAPIKey  string
	StreamingSecret  string
}

// Module bundles the live module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
	Store    *Store
}

// NewModule wires the live module together.
func NewModule(pool *pgxpool.Pool, rdb *redis.Client, cfg Config, log *slog.Logger) *Module {
	store := NewStore(pool)
	presence := NewPresence(rdb, log)
	streamer := NewLiveKitStreamer(cfg.StreamingHost, cfg.StreamingAPIKey, cfg.StreamingSecret)
	service := NewService(store, presence, streamer, log)

	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
		Store:    store,
	}
}

// Router returns the live routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
