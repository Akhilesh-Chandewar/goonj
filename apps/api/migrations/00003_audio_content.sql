-- Phase 1 — Foundation: audio content (uploads, processing outputs, metadata).
-- The `audio` table also stores live recordings in Phase 3 (source_session_id).

-- +goose Up
CREATE TABLE audio (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id        UUID NOT NULL REFERENCES creators (id) ON DELETE CASCADE,
    title             TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description       TEXT NOT NULL DEFAULT '',
    category          TEXT NOT NULL DEFAULT 'general',
    language          TEXT NOT NULL DEFAULT 'en',
    status            TEXT NOT NULL DEFAULT 'UPLOADING'
        CHECK (status IN ('UPLOADING', 'PROCESSING', 'READY', 'FAILED', 'SCHEDULED', 'PRIVATE')),
    visibility        TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private')),
    source            TEXT NOT NULL DEFAULT 'upload' CHECK (source IN ('upload', 'live_recording')),
    source_session_id UUID REFERENCES live_sessions (id) ON DELETE SET NULL,
    published_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX idx_audio_status ON audio (status, published_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_audio_creator ON audio (creator_id, created_at DESC);
CREATE INDEX idx_audio_category ON audio (category) WHERE deleted_at IS NULL;

CREATE TABLE audio_metadata (
    audio_id     UUID PRIMARY KEY REFERENCES audio (id) ON DELETE CASCADE,
    duration_ms  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    language     TEXT NOT NULL DEFAULT 'en',
    waveform     JSONB,
    size_bytes   BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    mime_type    TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audio_files (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audio_id    UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    quality     TEXT NOT NULL CHECK (quality IN ('low', 'medium', 'high', 'original')),
    storage_key TEXT NOT NULL,
    bitrate_kbps INTEGER NOT NULL DEFAULT 0,
    size_bytes  BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    mime_type   TEXT NOT NULL DEFAULT 'audio/mpeg',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (audio_id, quality)
);
CREATE INDEX idx_audio_files_audio ON audio_files (audio_id);

-- +goose Down
DROP TABLE IF EXISTS audio_files;
DROP TABLE IF EXISTS audio_metadata;
DROP TABLE IF EXISTS audio;
