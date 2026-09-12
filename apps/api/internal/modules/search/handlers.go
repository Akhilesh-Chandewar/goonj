package search

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the search REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts search routes under /search.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.search)
	r.Get("/suggestions", h.suggestions)
	return r
}

func (h *Handlers) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	res, err := h.svc.All(r.Context(), q, limit, offset)
	if err != nil {
		h.log.Error("search failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// suggestions returns lightweight audio title hits for autocomplete boxes.
func (h *Handlers) suggestions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	res, err := h.svc.All(r.Context(), q, 8, 0)
	if err != nil {
		h.log.Error("suggestions failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	titles := make([]map[string]string, 0, len(res.Audio))
	for _, a := range res.Audio {
		titles = append(titles, map[string]string{
			"id":    a.ID,
			"title": a.Title,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": titles})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	// Marshal first: if encoding fails (NaN/Inf from a bad rank, context bug,
	// …) we must not have already sent a 200 with no body — clients would see
	// a silent empty response instead of a real error.
	raw, err := json.Marshal(body)
	if err != nil {
		slog.Error("json marshal failed", slog.Any("error", err))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"response serialization failed"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
