-- Phase 5.5 — Live translation: per-session translation settings, per-language
-- caption persistence, and future extended events.

-- +goose Up
-- One translation config per live session (set by the creator at go-live or
-- while live). Only one source language and one target set is active.
CREATE TABLE live_translations (
    session_id    UUID PRIMARY KEY REFERENCES live_sessions (id) ON DELETE CASCADE,
    source_lang   TEXT NOT NULL DEFAULT '',
    target_langs  TEXT[] NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Persisted translated captions per session+language (source-language text
-- lives in live_captions_transcript_chunks). Cap the retained span with a
-- cleanup in the caption writer (keep last N per session+lang).
CREATE TABLE live_captions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    lang         TEXT NOT NULL,
    text         TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 2000),
    start_ms     INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms       INTEGER NOT NULL CHECK (end_ms >= start_ms),
    is_final     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_live_captions_session ON live_captions (session_id, lang, start_ms);

-- Source-language captions (what the creator actually said).
CREATE TABLE live_captions_source (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    text         TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 2000),
    start_ms     INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms       INTEGER NOT NULL CHECK (end_ms >= start_ms),
    is_final     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_live_captions_source ON live_captions_source (session_id, start_ms);

-- +goose Down
DROP TABLE IF EXISTS live_captions_source;
DROP TABLE IF EXISTS live_captions;
DROP TABLE IF EXISTS live_translations;
