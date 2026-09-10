-- Phase 3 — Live→Saved: egress recordings. Every go-live session records via
-- LiveKit room-composite egress; when the stream ends the recording lands in
-- object storage and is converted into a draft `audio` episode by the worker.

-- +goose Up
CREATE TABLE live_recordings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    egress_id     TEXT NOT NULL,
    room_name     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'RECORDING'
        CHECK (status IN ('RECORDING', 'ENDING', 'COMPLETED', 'FAILED', 'CONVERTED')),
    storage_key   TEXT,
    file_size     BIGINT NOT NULL DEFAULT 0 CHECK (file_size >= 0),
    audio_id      UUID REFERENCES audio (id) ON DELETE SET NULL,
    error         TEXT,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_live_recordings_session ON live_recordings (session_id);
CREATE INDEX idx_live_recordings_egress ON live_recordings (egress_id);
-- Worker polling: find recordings that still need finalization.
CREATE INDEX idx_live_recordings_pending ON live_recordings (started_at)
    WHERE status IN ('RECORDING', 'ENDING');

-- +goose Down
DROP TABLE IF EXISTS live_recordings;
