package social

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the social REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts social routes under /creators plus the subscriptions feed.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()

	r.With(mw.RequireAuth).Get("/followed", h.followed)
	r.Get("/{id}", h.creatorPage)
	r.Get("/{id}/follow", h.followState)
	r.With(mw.RequireAuth).Post("/{id}/follow", h.follow)
	r.With(mw.RequireAuth).Delete("/{id}/follow", h.unfollow)

	return r
}

// FeedRouter mounts the subscriptions feed (separate prefix /subscriptions).
func (h *Handlers) FeedRouter(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	r.With(mw.RequireAuth).Get("/feed", h.feed)
	return r
}

func (h *Handlers) followed(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	items, err := h.svc.Followed(r.Context(), identity.UserID)
	if err != nil {
		h.log.Error("followed list failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load followed creators")
		return
	}
	if items == nil {
		items = []Creator{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) creatorPage(w http.ResponseWriter, r *http.Request) {
	viewer := ""
	if identity := authmod.FromContext(r.Context()); identity != nil {
		viewer = identity.UserID
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	page, err := h.svc.CreatorPage(r.Context(), chi.URLParam(r, "id"), viewer, limit, offset)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "creator not found")
			return
		}
		h.log.Error("creator page failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handlers) followState(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if identity := authmod.FromContext(r.Context()); identity != nil {
		userID = identity.UserID
	}
	c, err := h.svc.store.GetCreator(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	following := false
	if userID != "" {
		following, err = h.svc.store.IsFollowing(r.Context(), userID, c.ID)
		if err != nil {
			h.writeSvcErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, FollowState{Following: following, Subscribers: c.SubscriberCount})
}

func (h *Handlers) follow(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	st, err := h.svc.Follow(r.Context(), identity.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) unfollow(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	st, err := h.svc.Unfollow(r.Context(), identity.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) feed(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	items, err := h.svc.Feed(r.Context(), identity.UserID, limit, offset)
	if err != nil {
		h.log.Error("subscriptions feed failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load feed")
		return
	}
	if items == nil {
		items = []FeedItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) writeSvcErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	h.log.Error("social service error", slog.Any("error", err))
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
