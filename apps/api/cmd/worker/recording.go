package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/notifications"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/shared/tasks"
)

// recordingStore is the worker-side view of live_recordings (outcome updates
// only; no live-module import).
type recordingStore struct {
	pool *pgxpool.Pool
}

func (r *recordingStore) ByEgress(ctx context.Context, egressID string) (sessionID, status, existingKey string, err error) {
	row := r.pool.QueryRow(ctx, `
		SELECT session_id::text, status, COALESCE(storage_key, '')
		FROM live_recordings WHERE egress_id = $1`, egressID)
	err = row.Scan(&sessionID, &status, &existingKey)
	return sessionID, status, existingKey, err
}

func (r *recordingStore) Complete(ctx context.Context, egressID string, size int64, key string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = 'COMPLETED', file_size = $2, storage_key = $3,
			completed_at = now(), updated_at = now()
		WHERE egress_id = $1`, egressID, size, key)
	return err
}

func (r *recordingStore) Fail(ctx context.Context, egressID, cause string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = 'FAILED', error = $2, completed_at = now(), updated_at = now()
		WHERE egress_id = $1`, egressID, cause)
	return err
}

func (r *recordingStore) MarkConverted(ctx context.Context, egressID, audioID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE live_recordings
		SET status = 'CONVERTED', audio_id = $2, updated_at = now()
		WHERE egress_id = $1`, egressID, audioID)
	return err
}

// draftStore is the worker-side writer for audio rows produced from live
// recordings (no audio-module import: worker stays a thin consumer).
type draftStore struct {
	pool *pgxpool.Pool
}

// CreateLiveRecording inserts a PRIVATE PROCESSING audio row for a session
// and returns its id.
func (d *draftStore) CreateLiveRecording(ctx context.Context, creatorID, sessionID, title, description, category string) (string, error) {
	var id string
	err := d.pool.QueryRow(ctx, `
		INSERT INTO audio (creator_id, title, description, category, source, source_session_id, status, visibility)
		VALUES ($1, $2, $3, $4, 'live_recording', $5, 'PROCESSING', 'private')
		RETURNING id`,
		creatorID, title, description, category, sessionID).Scan(&id)
	return id, err
}

// AddFile records the recording as the audio's original source file.
func (d *draftStore) AddFile(ctx context.Context, audioID string, f QualityFile) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO audio_files (audio_id, quality, storage_key, bitrate_kbps, size_bytes, mime_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (audio_id, quality) DO UPDATE
		SET storage_key = EXCLUDED.storage_key,
			bitrate_kbps = EXCLUDED.bitrate_kbps,
			size_bytes = EXCLUDED.size_bytes,
			mime_type = EXCLUDED.mime_type`,
		audioID, f.Quality, f.StorageKey, f.BitrateKbps, f.SizeBytes, f.MimeType)
	return err
}

// SetStatus moves an audio row through the lifecycle.
func (d *draftStore) SetStatus(ctx context.Context, id, status string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE audio SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

// recordingObjectKey is where the egress service is configured to write.
func recordingObjectKey(sessionID string) string {
	return fmt.Sprintf("live/recordings/%s.mp3", sessionID)
}

// sessionRoomName mirrors the live module's room naming ("live-<id>").
func sessionRoomName(sessionID string) string {
	return "live-" + sessionID
}

// normalizeRecordingKey prefers the egress-reported location when usable and
// maps every shape LiveKit reports onto a plain object key:
//   - s3://bucket/key            → key
//   - https://host/bucket/key    → key (path-style; bucket segment stripped)
//   - https://bucket.host/key    → key (virtual-hosted style)
//   - /key, key                  → key
//
// Anything ambiguous falls back to the caller's canonical key.
func normalizeRecordingKey(location, bucket, fallback string) string {
	if location == "" {
		return fallback
	}
	if rest, ok := strings.CutPrefix(location, "s3://"); ok {
		if _, key, ok := strings.Cut(rest, "/"); ok && key != "" {
			return key
		}
		return fallback
	}
	if i := strings.Index(location, "://"); i > 0 {
		rest := location[i+3:]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return fallback
		}
		path := rest[slash+1:]
		if path == "" {
			return fallback
		}
		// Path-style URL: the first path segment is the bucket.
		if first, remainder, ok := strings.Cut(path, "/"); ok {
			if first == bucket && remainder != "" {
				return remainder
			}
		}
		// Virtual-hosted style: the bucket lives in the host.
		return path
	}
	return strings.TrimPrefix(location, "/")
}

// finalizeRecording turns a stopped egress recording into a draft episode:
//  1. wait for the egress to land the MP3 in object storage,
//  2. create a PRIVATE audio row owned by the session's creator,
//  3. register the recording as the original file and enqueue the regular
//     audio pipeline (loudnorm/transcode/waveform) against it,
//  4. mark the recording CONVERTED with a link to the draft episode.
//
// Retries are idempotent: re-runs short-circuit on live_recordings.status.
func (h *audioHandler) finalizeRecording(ctx context.Context, t *asynq.Task) error {
	payload, err := tasks.DecodeFinalizeRecording(t)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err) // no retry on bad payload
	}
	h.log.Info("finalizing live recording",
		slog.String("session", payload.SessionID),
		slog.String("egress", payload.EgressID))

	if h.egress == nil {
		return fmt.Errorf("egress client unavailable") // infra: retry later
	}

	// Idempotency: skip work already done on a previous attempt.
	sessionID, status, _, err := h.recordings.ByEgress(ctx, payload.EgressID)
	if err != nil {
		return fmt.Errorf("load recording: %w", err)
	}
	switch status {
	case "CONVERTED":
		h.log.Info("recording already converted; skipping",
			slog.String("egress", payload.EgressID))
		return nil
	case "FAILED":
		return fmt.Errorf("recording previously FAILED; not retrying")
	}

	// 1. Wait for LiveKit to finish uploading the recording.
	out, err := h.egress.WaitComplete(ctx, payload.EgressID, 10*time.Minute)
	if err != nil {
		if out != nil && out.FileLocation == "" {
			_ = h.recordings.Fail(ctx, payload.EgressID, err.Error())
			return fmt.Errorf("egress failed permanently: %w", err)
		}
		return err // transient; Asynq retries with backoff
	}
	storageKey := recordingObjectKey(payload.SessionID)
	if out.FileLocation != "" {
		storageKey = normalizeRecordingKey(out.FileLocation, h.bucket, storageKey)
	}
	if err := h.recordings.Complete(ctx, payload.EgressID, out.FileSize, storageKey); err != nil {
		return fmt.Errorf("complete recording: %w", err)
	}

	// Recording landed; now it is safe to tear the room down (End() leaves it
	// alive so egress can finalize — see live.Service.End).
	if err := h.egress.CloseRoom(ctx, sessionRoomName(sessionID)); err != nil {
		h.log.Warn("room cleanup failed (non-fatal)",
			slog.String("session", payload.SessionID), slog.Any("error", err))
	}

	// 2. Resolve the session and create the draft episode.
	var creatorID, title, description, category string
	if err := h.pool.QueryRow(ctx, `
			SELECT creator_id::text, title, description, category
			FROM live_sessions WHERE id = $1`, sessionID).
		Scan(&creatorID, &title, &description, &category); err != nil {
		return fmt.Errorf("load session: %w", err)
	}

	audioID, err := h.drafts.CreateLiveRecording(ctx, creatorID, sessionID,
		title, description, category)
	if err != nil {
		return fmt.Errorf("create draft episode: %w", err)
	}

	// 3. Point the audio pipeline at the recording and enqueue processing.
	if err := h.drafts.AddFile(ctx, audioID, QualityFile{
		Quality: "original", StorageKey: storageKey, MimeType: "audio/mpeg",
	}); err != nil {
		return fmt.Errorf("register original: %w", err)
	}
	task, err := tasks.NewProcessAudio(tasks.ProcessAudioPayload{
		AudioID:      audioID,
		StorageKey:   storageKey,
		OriginalMime: "audio/mpeg",
	})
	if err != nil {
		return fmt.Errorf("build process task: %w", err)
	}
	if _, err := h.enqueuer.EnqueueContext(ctx, task); err != nil {
		_ = h.drafts.SetStatus(ctx, audioID, "FAILED")
		return fmt.Errorf("enqueue processing: %w", err)
	}

	// 4. Link the draft back to the recording.
	if err := h.recordings.MarkConverted(ctx, payload.EgressID, audioID); err != nil {
		return fmt.Errorf("mark converted: %w", err)
	}

	// Phase 5 completion: roll analytics up into live_analytics and notify
	// the creator that their draft episode is ready. Both best-effort.
	if h.rdb != nil {
		if err := RollupLiveAnalytics(ctx, h.pool, h.rdb, h.log, sessionID, audioID); err != nil {
			h.log.Warn("analytics rollup failed (non-fatal)",
				slog.String("session", sessionID), slog.Any("error", err))
		}
	}
	if h.notify != nil {
		var creatorUser, title string
		if err := h.pool.QueryRow(ctx, `
			SELECT u.id::text, s.title FROM live_sessions s
			JOIN creators c ON c.id = s.creator_id
			JOIN users u ON u.id = c.user_id
			WHERE s.id = $1`, sessionID).Scan(&creatorUser, &title); err == nil {
			h.notify.Notify(ctx, []string{creatorUser}, notifications.Notification{
				Type: notifications.TypeRecordingReady, Title: "Your recording is ready",
				Body: title + " was converted to a draft episode — publish it from the studio.",
				AudioID: &audioID,
			})
		}
	}

	h.log.Info("live recording converted to draft episode",
		slog.String("session", payload.SessionID),
		slog.String("audio", audioID),
		slog.String("key", storageKey))
	return nil
}
