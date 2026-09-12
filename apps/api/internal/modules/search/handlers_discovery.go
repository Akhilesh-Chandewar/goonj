package search

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// DiscoveryHandlers serve /recommendations and /trending.
type DiscoveryHandlers struct {
	reco *RecoStore
	rdb  *redis.Client
	log  *slog.Logger

	// Trending is read from Redis; a tiny in-process cache softens bursts.
	mu          sync.Mutex
	cacheUntil  time.Time
	cacheHits   []AudioHit
}

func NewDiscoveryHandlers(reco *RecoStore, rdb *redis.Client, log *slog.Logger) *DiscoveryHandlers {
	return &DiscoveryHandlers{reco: reco, rdb: rdb, log: log}
}

// RegisterRoutes mounts discovery routes on an existing mux.
func (h *DiscoveryHandlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Get("/recommendations", h.recommendations)
	r.With(mw.RequireAuth).Get("/recommendations/audio/{id}", h.similar)
	r.Get("/trending", h.trending) // public: discovery surface
}

// Router mounts on a fresh subrouter (for tests).
func (h *DiscoveryHandlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

// similar returns "more like this" for one episode.
func (h *DiscoveryHandlers) similar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "missing id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := h.reco.SimilarTo(r.Context(), id, limit)
	if err != nil {
		h.log.Error("similar failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": hits})
}

// recommendations returns a personalized-ish feed: similar to the episode the
// viewer last listened to (global fallback: trending).
func (h *DiscoveryHandlers) recommendations(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 12
	}

	var hits []AudioHit
	// Seed from the viewer's most recent listening entry (if any).
	var seedID string
	err := h.reco.pool.QueryRow(r.Context(), `
		SELECT audio_id::text FROM listening_history WHERE user_id = $1
		ORDER BY updated_at DESC LIMIT 1`, identity.UserID).Scan(&seedID)
	if err == nil && seedID != "" {
		similar, err := h.reco.SimilarTo(r.Context(), seedID, limit)
		if err == nil {
			hits = toAudioHits(similar)
		}
	}
	if len(hits) == 0 {
		// Fallback: trending ids hydrated.
		ids, err := TrendingIDs(r.Context(), h.rdb, limit)
		if err == nil {
			hits, _ = HydrateAudio(r.Context(), h.reco.pool, ids)
		}
	}
	if hits == nil {
		hits = []AudioHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": hits, "seed": seedID})
}

// trending serves the current trending list (1-minute in-process cache).
func (h *DiscoveryHandlers) trending(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	h.mu.Lock()
	cacheOK := time.Now().Before(h.cacheUntil) && len(h.cacheHits) > 0
	cached := h.cacheHits
	h.mu.Unlock()
	if cacheOK && len(cached) >= limit {
		writeJSON(w, http.StatusOK, map[string]any{"data": cached[:limit]})
		return
	}

	ids, err := TrendingIDs(r.Context(), h.rdb, 50)
	if err != nil {
		h.log.Error("trending read failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	hits, err := HydrateAudio(r.Context(), h.reco.pool, ids)
	if err != nil {
		h.log.Error("trending hydrate failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.mu.Lock()
	h.cacheHits = hits
	h.cacheUntil = time.Now().Add(time.Minute)
	h.mu.Unlock()
	if hits == nil {
		hits = []AudioHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": hits})
}

func toAudioHits(in []HitAudio) []AudioHit {
	out := make([]AudioHit, 0, len(in))
	for _, h := range in {
		out = append(out, AudioHit{
			ID: h.ID, Title: h.Title, Category: h.Category, CreatorID: h.CreatorID,
			CreatorName: h.CreatorName, Handle: h.Handle, DurationMs: h.DurationMs,
			PublishedAt: h.PublishedAt,
		})
	}
	return out
}
