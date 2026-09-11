package engagement

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the engagement REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts engagement routes on a fresh subrouter.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

// RegisterRoutes registers engagement endpoints on an existing mux so they
// can share the /audio prefix with the audio module (chi forbids mounting
// two handlers on the same path).
func (h *Handlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Get("/liked", h.liked)

	r.Get("/{id}/like", h.state)
	r.With(mw.RequireAuth).Post("/{id}/like", h.like)
	r.With(mw.RequireAuth).Delete("/{id}/like", h.unlike)

	r.Get("/{id}/comments", h.listComments)
	r.With(mw.RequireAuth).Post("/{id}/comments", h.addComment)
	r.With(mw.RequireAuth).Delete("/comments/{commentId}", h.deleteComment)

	r.With(mw.RequireAuth).Post("/comments/{commentId}/like", h.likeComment)
	r.With(mw.RequireAuth).Delete("/comments/{commentId}/like", h.unlikeComment)
}

func (h *Handlers) liked(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := h.svc.LikedAudio(r.Context(), identity.UserID, limit, offset)
	if err != nil {
		h.log.Error("liked list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load liked audio")
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) state(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if identity := authmod.FromContext(r.Context()); identity != nil {
		userID = identity.UserID
	}
	st, err := h.svc.State(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) like(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	st, err := h.svc.Like(r.Context(), identity.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) unlike(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	st, err := h.svc.Unlike(r.Context(), identity.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) listComments(w http.ResponseWriter, r *http.Request) {
	viewer := ""
	if identity := authmod.FromContext(r.Context()); identity != nil {
		viewer = identity.UserID
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tops, replies, err := h.svc.Comments(r.Context(), chi.URLParam(r, "id"), viewer, limit)
	if err != nil {
		h.log.Error("comments list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load comments")
		return
	}
	if tops == nil {
		tops = []*Comment{}
	}
	if replies == nil {
		replies = []*Comment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"comments": tops,
		"replies":  replies,
	}})
}

func (h *Handlers) addComment(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Body     string  `json:"body"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if s := strings.TrimSpace(req.Body); s == "" {
		writeErr(w, http.StatusBadRequest, "comment body is required")
		return
	}

	c, err := h.svc.AddComment(r.Context(), chi.URLParam(r, "id"), identity.UserID, req.ParentID, req.Body)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handlers) deleteComment(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.DeleteComment(r.Context(), chi.URLParam(r, "commentId"), identity.UserID); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) likeComment(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.LikeComment(r.Context(), identity.UserID, chi.URLParam(r, "commentId")); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "liked"})
}

func (h *Handlers) unlikeComment(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.UnlikeComment(r.Context(), identity.UserID, chi.URLParam(r, "commentId")); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unliked"})
}

func (h *Handlers) writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "must be"):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("engagement service error", slog.Any("error", err))
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
