package history

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the history REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts history routes under /history.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	r.With(mw.RequireAuth).Post("/{id}/progress", h.report)
	r.With(mw.RequireAuth).Get("/{id}/resume", h.resume)
	r.With(mw.RequireAuth).Get("/", h.recent)
	return r
}

func (h *Handlers) report(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		PositionMs int `json:"position_ms"`
		DurationMs int `json:"duration_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.Report(r.Context(), identity.UserID, chi.URLParam(r, "id"), req.PositionMs, req.DurationMs); err != nil {
		h.log.Error("progress report failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to record progress")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handlers) recent(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := h.svc.Recent(r.Context(), identity.UserID, limit, offset)
	if err != nil {
		h.log.Error("history list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	if items == nil {
		items = []Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) resume(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	pos, completed := h.svc.Resume(r.Context(), identity.UserID, chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{
		"position_ms": pos,
		"completed":   completed,
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
