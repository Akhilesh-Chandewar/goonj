package playlists

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the playlists REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts playlist routes under /playlists.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()

	r.With(mw.RequireAuth).Post("/", h.create)
	r.With(mw.RequireAuth).Get("/", h.mine)

	r.Get("/{id}", h.detail)
	r.With(mw.RequireAuth).Patch("/{id}", h.update)
	r.With(mw.RequireAuth).Delete("/{id}", h.delete)

	r.With(mw.RequireAuth).Post("/{id}/items", h.addItem)
	r.With(mw.RequireAuth).Delete("/{id}/items/{audioId}", h.removeItem)

	return r
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 120 {
		writeErr(w, http.StatusBadRequest, "title must be 1-120 characters")
		return
	}

	pl, err := h.svc.Create(r.Context(), identity.UserID, req.Title, req.Description, req.Visibility)
	if err != nil {
		h.log.Error("playlist create failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to create playlist")
		return
	}
	writeJSON(w, http.StatusCreated, pl)
}

func (h *Handlers) mine(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	items, err := h.svc.Mine(r.Context(), identity.UserID)
	if err != nil {
		h.log.Error("playlist list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load playlists")
		return
	}
	if items == nil {
		items = []Playlist{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) detail(w http.ResponseWriter, r *http.Request) {
	viewer := ""
	if identity := authmod.FromContext(r.Context()); identity != nil {
		viewer = identity.UserID
	}
	pl, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"), viewer)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pl)
}

func (h *Handlers) update(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)

	if err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), identity.UserID,
		req.Title, req.Description, req.Visibility); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	pl, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pl)
}

func (h *Handlers) delete(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id"), identity.UserID); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) addItem(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		AudioID string `json:"audio_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AudioID == "" {
		writeErr(w, http.StatusBadRequest, "audio_id is required")
		return
	}
	added, err := h.svc.AddAudio(r.Context(), chi.URLParam(r, "id"), identity.UserID, req.AudioID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "added", "added": added})
}

func (h *Handlers) removeItem(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	removed, err := h.svc.RemoveAudio(r.Context(),
		chi.URLParam(r, "id"), identity.UserID, chi.URLParam(r, "audioId"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "removed", "removed": removed})
}

func (h *Handlers) writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errInvalidTitle):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("playlists service error", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
