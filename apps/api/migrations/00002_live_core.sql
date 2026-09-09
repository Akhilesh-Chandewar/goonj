-- Phase 2 — Live Core: live sessions, stream credentials, chat, moderators,
-- reactions, and analytics. Presence/listener counts live in Redis; only
-- snapshots and aggregates are persisted here.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE live_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id      UUID NOT NULL REFERENCES creators (id) ON DELETE CASCADE,
    title           TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description     TEXT NOT NULL DEFAULT '',
    cover_image_url TEXT,
    category        TEXT NOT NULL DEFAULT 'general',
    status          TEXT NOT NULL DEFAULT 'SCHEDULED'
        CHECK (status IN ('SCHEDULED', 'LIVE', 'ENDED', 'CANCELLED')),
    visibility      TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private')),
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    peak_listeners  INTEGER NOT NULL DEFAULT 0 CHECK (peak_listeners >= 0),
    total_listeners BIGINT NOT NULL DEFAULT 0 CHECK (total_listeners >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_live_sessions_status ON live_sessions (status, started_at DESC);
CREATE INDEX idx_live_sessions_creator ON live_sessions (creator_id, created_at DESC);

CREATE TABLE live_streams (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     UUID NOT NULL UNIQUE REFERENCES live_sessions (id) ON DELETE CASCADE,
    room_name      TEXT NOT NULL UNIQUE,
    ingest_token   TEXT NOT NULL,
    token_expires  TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE live_moderators (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    granted_by UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, user_id)
);

CREATE TABLE live_chat_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body       TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 500),
    is_pinned  BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_live_chat_session ON live_chat_messages (session_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Aggregated reaction windows, not individual reactions.
CREATE TABLE live_reactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    reaction    TEXT NOT NULL CHECK (reaction IN ('heart', 'clap', 'fire', 'laugh', 'party', 'thumbsup')),
    count       INTEGER NOT NULL DEFAULT 0 CHECK (count > 0),
    window_start TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, reaction, window_start)
);
CREATE INDEX idx_live_reactions_session ON live_reactions (session_id, created_at DESC);

-- Periodic presence snapshots (Redis is the real-time source of truth).
CREATE TABLE live_viewers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    concurrent     INTEGER NOT NULL,
    captured_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_live_viewers_session ON live_viewers (session_id, captured_at DESC);

CREATE TABLE live_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_live_events_session ON live_events (session_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS live_events;
DROP TABLE IF EXISTS live_viewers;
DROP TABLE IF EXISTS live_reactions;
DROP TABLE IF EXISTS live_chat_messages;
DROP TABLE IF EXISTS live_moderators;
DROP TABLE IF EXISTS live_streams;
DROP TABLE IF EXISTS live_sessions;
