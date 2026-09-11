package translation

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// openaiNoKeyMsg mirrors the no-key error text from the openai package so
// handlers can map it to 503 without importing internals.
const openaiNoKeyMsg = "openai: no api key configured"

// Module bundles the translation module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the translation module together.
func NewModule(pool *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) *Module {
	store := NewStore(pool)
	svc := NewService(store, rdb, log)
	return &Module{
		Service:  svc,
		Handlers: NewHandlers(svc, log),
	}
}

// Handlers exposes the translation REST API.
type Handlers struct {
	svc *Service
	log *slog.Logger
}

func NewHandlers(svc *Service, log *slog.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// RegisterRoutes mounts translation routes on an existing /live mux (the
// live module owns the prefix; chi forbids double mounts).
func (h *Handlers) RegisterRoutes(r chi.Router, mw *authmod.Middleware) {
	r.With(mw.RequireAuth).Get("/{id}/translate/languages", h.languages)
	r.With(mw.RequireAuth).Get("/{id}/translate/captions", h.captions)
	r.With(mw.RequireAuth).Get("/{id}/translate/config", h.settings)
	// Creator control plane: configure, mint browser sessions, ingest captions.
	r.With(mw.RequireRole("CREATOR", "ADMIN")).Put("/{id}/translate/config", h.configure)
	r.With(mw.RequireRole("CREATOR", "ADMIN")).Post("/{id}/translate/session", h.mintSession)
	r.With(mw.RequireRole("CREATOR", "ADMIN")).Post("/{id}/translate/captions", h.ingest)
}

// Router mounts on a fresh subrouter (convenience for tests).
func (h *Handlers) Router(mw *authmod.Middleware) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r, mw)
	return r
}

// languages lists supported target languages for the picker.
func (h *Handlers) languages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": SupportedLanguages})
}

// settings returns the current translation config for a session.
func (h *Handlers) settings(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Settings(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.log.Error("translation settings failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// configure is the creator control plane (PUT /live/{id}/translate/config).
func (h *Handlers) configure(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	var req struct {
		Enabled     bool     `json:"enabled"`
		SourceLang  string   `json:"source_lang"`
		TargetLangs []string `json:"target_langs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Room name mirrors the live module's convention ("live-<id>"); the
	// translator derives tracks from the room, not from this field.
	room := "live-" + sessionID
	st, err := h.svc.Configure(r.Context(), sessionID, room, &Settings{
		SourceLang:  req.SourceLang,
		TargetLangs: req.TargetLangs,
		Enabled:     req.Enabled,
	})
	if err != nil {
		switch err {
		case ErrInvalidLanguage, ErrNoLanguages, ErrTooManyLanguages:
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			h.log.Error("translation configure failed", slog.Any("error", err))
			writeErr(w, http.StatusServiceUnavailable, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// mintSession mints one ephemeral browser WebRTC translation session
// (POST /live/{id}/translate/session).
func (h *Handlers) mintSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	var req BrowserSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	resp, err := h.svc.MintBrowserSession(r.Context(), sessionID, req)
	if err != nil {
		switch err {
		case ErrTranslationDisabled, ErrInvalidLanguage:
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			if err.Error() == openaiNoKeyMsg {
				writeErr(w, http.StatusServiceUnavailable, "translation unavailable: no OPENAI_API_KEY")
				return
			}
			h.log.Error("mint session failed", slog.Any("error", err))
			writeErr(w, http.StatusBadGateway, "translation upstream error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// ingest relays one caption event from the creator's OpenAI data channel to
// every listener (POST /live/{id}/translate/captions).
func (h *Handlers) ingest(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	var req struct {
		Lang    string `json:"lang"`
		Text    string `json:"text"`
		Final   bool   `json:"final"`
		StartMs int    `json:"start_ms"`
		EndMs   int    `json:"end_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.IngestCaption(r.Context(), sessionID, req.Lang, req.Text, req.Final, req.StartMs, req.EndMs); err != nil {
		h.log.Error("caption ingest failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// captions serves caption history for late joiners / language switchers.
func (h *Handlers) captions(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	caps, err := h.svc.History(r.Context(), chi.URLParam(r, "id"), lang, limit)
	if err != nil {
		h.log.Error("caption history failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": caps})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
