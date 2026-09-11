package audio

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// studioStats handles GET /studio/stats for the signed-in creator.
func (h *Handlers) studioStats(w http.ResponseWriter, r *http.Request) {
	identity := authmod.FromContext(r.Context())
	creatorID, err := h.svc.store.CreatorIDByUserID(r.Context(), identity.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusForbidden, "creator profile required")
			return
		}
		h.log.Error("creator lookup failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	stats, err := h.svc.store.StudioStats(r.Context(), creatorID)
	if err != nil {
		h.log.Error("studio stats failed", slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "failed to load stats")
		return
	}
	if latest, err := h.svc.store.LatestPublished(r.Context(), creatorID); err == nil {
		stats.Latest = latest
	} else if !errors.Is(err, pgx.ErrNoRows) {
		h.log.Warn("latest lookup failed", slog.Any("error", err))
	}
	writeJSON(w, http.StatusOK, stats)
}
