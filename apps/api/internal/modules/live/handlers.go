package live

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Handlers exposes the live REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Router mounts live routes. Creator-only routes require the CREATOR role;
// session-owner checks happen in the service.
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()

	r.With(mw.RequireRole("CREATOR", "ADMIN")).Post("/", h.create)
	r.Get("/", h.listLive)
	r.Get("/{id}", h.get)

	r.With(mw.RequireAuth).Post("/{id}/start", h.start)
	r.With(mw.RequireAuth).Post("/{id}/end", h.end)
	r.With(mw.RequireAuth).Get("/{id}/recording", h.recording)
	r.With(mw.RequireAuth).Post("/{id}/join", h.join)
	r.With(mw.RequireAuth).Post("/{id}/leave", h.leave)
	r.With(mw.RequireAuth).Post("/{id}/heartbeat", h.heartbeat)
	r.With(mw.RequireAuth).Post("/{id}/recording/start", h.startRecording)
	r.With(mw.RequireAuth).Post("/{id}/reactions", h.react)
	r.With(mw.RequireAuth).Post("/{id}/report", h.report)

	// Scheduling: creator sets a planned start; the public list is open.
	r.With(mw.RequireAuth).Post("/{id}/schedule", h.schedule)
	r.Get("/upcoming", h.upcoming)

	return r
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	session, err := h.svc.Create(r.Context(), CreateInput{
		UserID:      identity.UserID,
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
		Visibility:  req.Visibility,
	})
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (h *Handlers) listLive(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sessions, err := h.svc.ListLive(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list live sessions")
		return
	}
	if sessions == nil {
		sessions = []Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sessions})
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	session, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	conc, uniq, peak, _ := h.svc.Stats(r.Context(), session.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"session":    session,
		"concurrent": conc,
		"unique":     uniq,
		"peak":       peak,
	})
}

func (h *Handlers) start(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	session, token, err := h.svc.Start(r.Context(), chi.URLParam(r, "id"), identity.UserID, identity.Username)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":      session,
		"stream_token": token,
	})
}

func (h *Handlers) end(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	session, err := h.svc.End(r.Context(), chi.URLParam(r, "id"), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

// startRecording kicks off participant egress once the creator's client is
// connected and publishing (POST /live/{id}/recording/start).
func (h *Handlers) startRecording(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	rec, err := h.svc.StartRecordingNow(r.Context(), chi.URLParam(r, "id"), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// recording exposes the session's recording status (studio: "saving your
// stream…" → "draft ready → publish").
func (h *Handlers) recording(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	rec, err := h.svc.Recording(r.Context(), chi.URLParam(r, "id"), identity.UserID)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handlers) join(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	session, token, conc, uniq, err := h.svc.Join(r.Context(), chi.URLParam(r, "id"), identity.UserID, identity.Username)
	if err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":          session,
		"stream_token":     token,
		"concurrent":       conc,
		"unique_listeners": uniq,
	})
}

func (h *Handlers) leave(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.Leave(r.Context(), chi.URLParam(r, "id"), identity.UserID, identity.Username); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

func (h *Handlers) heartbeat(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	if err := h.svc.Heartbeat(r.Context(), chi.URLParam(r, "id"), identity.UserID); err != nil {
		h.writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) react(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Reaction string `json:"reaction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !validReaction(req.Reaction) {
		writeErr(w, http.StatusBadRequest, "unknown reaction")
		return
	}

	// Reactions are fire-and-forget: fan out, aggregate later (worker).
	h.svc.Publish(r.Context(), chi.URLParam(r, "id"), toJSON(map[string]any{
		"type":     "reaction",
		"reaction": req.Reaction,
		"user_id":  identity.UserID,
	}))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handlers) report(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	h.svc.store.AppendEvent(r.Context(), chi.URLParam(r, "id"), "session.reported",
		toJSON(map[string]any{"reason": req.Reason, "reporter": identity.UserID}))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "reported"})
}

func (h *Handlers) writeSvcErr(w http.ResponseWriter, err error) {
	if !isKnownSvcErr(err) {
		h.log.Error("live service error", slog.Any("error", err))
	}
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrSessionNotFound):
		writeErr(w, http.StatusNotFound, "live session not found")
	case errors.Is(err, ErrNotOwner):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNotCreator):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrInvalidStatus):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidTitle):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrPrivateSession):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrRecordingUnavailable):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func isKnownSvcErr(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrSessionNotFound) ||
		errors.Is(err, ErrNotOwner) || errors.Is(err, ErrNotCreator) ||
		errors.Is(err, ErrInvalidStatus) || errors.Is(err, ErrInvalidTitle) ||
		errors.Is(err, ErrPrivateSession)
}

func validReaction(r string) bool {
	switch r {
	case "heart", "clap", "fire", "laugh", "party", "thumbsup":
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
