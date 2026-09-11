package ai

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the AI REST API (voice agent session minting).
type Handlers struct {
	log    *slog.Logger
	apiKey string
}

// NewHandlers builds the AI handlers. The OpenAI key is read from env at
// construction so handlers stay request-independent.
func NewHandlers(_ *pgxpool.Pool, log *slog.Logger) *Handlers {
	return &Handlers{log: log, apiKey: apiKeyFromEnv()}
}

// RegisterRoutes mounts AI routes on an existing mux.
func (h *Handlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Post("/agent/session", h.agentSession)
}

// Router mounts on a fresh subrouter (mounted at /ai by the app).
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

// agentSession mints an ephemeral OpenAI Realtime session for the browser's
// voice agent. 429-style unavailability (no key) surfaces as 503 so the
// client can hide the feature instead of erroring.
func (h *Handlers) agentSession(w http.ResponseWriter, r *http.Request) {
	var req AgentSessionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	resp, err := MintAgentSession(r.Context(), h.apiKey, req)
	if err != nil {
		if err == ErrUnavailable {
			writeErr(w, http.StatusServiceUnavailable, "voice agent unavailable: no OPENAI_API_KEY configured")
			return
		}
		h.log.Error("agent session mint failed", slog.Any("error", err))
		writeErr(w, http.StatusBadGateway, "voice agent upstream error")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
