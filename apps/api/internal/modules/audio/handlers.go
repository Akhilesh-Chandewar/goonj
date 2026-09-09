package audio

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the audio REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts audio routes.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()

	r.Get("/", h.feed)
	r.With(mw.RequireAuth).Get("/mine", h.mine)

	r.With(mw.RequireRole("CREATOR", "ADMIN")).Post("/uploads", h.initUpload)
	r.With(mw.RequireRole("CREATOR", "ADMIN")).Post("/uploads/{id}/complete", h.completeUpload)

	r.Get("/{id}", h.detail)
	r.Get("/{id}/playback", h.playback)
	r.With(mw.RequireRole("CREATOR", "ADMIN")).Delete("/{id}", h.delete)

	return r
}

func (h *Handlers) feed(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	items, err := h.svc.Feed(r.Context(), category, limit, offset)
	if err != nil {
		h.log.Error("feed failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load feed")
		return
	}
	if items == nil {
		items = []Audio{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) mine(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	items, err := h.svc.ListByCreator(r.Context(), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	if items == nil {
		items = []Audio{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) initUpload(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Language    string `json:"language"`
		MimeType    string `json:"mime_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	resp, err := h.svc.InitUpload(r.Context(), InitUploadInput{
		UserID:      identity.UserID,
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
		Language:    req.Language,
		MimeType:    req.MimeType,
	})
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) completeUpload(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	a, err := h.svc.CompleteUpload(r.Context(), chi.URLParam(r, "id"), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handlers) detail(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "audio not found")
			return
		}
		h.log.Error("detail failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handlers) playback(w http.ResponseWriter, r *http.Request) {
	pb, err := h.svc.PlaybackFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "audio not found")
			return
		}
		h.log.Error("playback failed", slog.Any("error", err))
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pb)
}

func (h *Handlers) delete(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id"), identity.UserID); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNotCreator):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrInvalidTitle), errors.Is(err, ErrUnsupportedMime), errors.Is(err, ErrInvalidState):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("audio service error", slog.Any("error", err))
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
