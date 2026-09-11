-- Phase 4 — On-demand & social: engagement (likes, comments), follows,
-- playlists, listening history, and search support (FTS + trigram).
-- Per PLAN §3: engagement writes are O(user,content) upserts, never per-event
-- aggregate churn; analytics rollups stay in worker-owned tables.

-- +goose Up
CREATE TABLE audio_likes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    audio_id   UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id)
);
CREATE INDEX idx_audio_likes_audio ON audio_likes (audio_id);
CREATE INDEX idx_audio_likes_user ON audio_likes (user_id, created_at DESC);

CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audio_id   UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES comments (id) ON DELETE CASCADE,
    body       TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 1000),
    is_pinned  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_comments_audio ON comments (audio_id, created_at) WHERE deleted_at IS NULL;
CREATE INDEX idx_comments_parent ON comments (parent_id) WHERE deleted_at IS NULL;

CREATE TABLE comment_likes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, comment_id)
);
CREATE INDEX idx_comment_likes_comment ON comment_likes (comment_id);

CREATE TABLE follows (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    creator_id UUID NOT NULL REFERENCES creators (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, creator_id)
);
CREATE INDEX idx_follows_creator ON follows (creator_id);

CREATE TABLE playlists (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title       TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '',
    visibility  TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('public', 'private')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_playlists_user ON playlists (user_id, updated_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE playlist_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playlist_id UUID NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    audio_id    UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (playlist_id, audio_id)
);
CREATE INDEX idx_playlist_items_playlist ON playlist_items (playlist_id, position);

CREATE TABLE listening_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    audio_id    UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    position_ms INTEGER NOT NULL DEFAULT 0 CHECK (position_ms >= 0),
    completed   BOOLEAN NOT NULL DEFAULT FALSE,
    play_count  INTEGER NOT NULL DEFAULT 1 CHECK (play_count > 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id)
);
CREATE INDEX idx_history_recent ON listening_history (user_id, updated_at DESC);

-- Search support: full-text on audio text, trigram on titles and creator
-- names for fuzzy/autocomplete matching. Partial indexes keep them tight.
CREATE INDEX idx_audio_search_fts ON audio
    USING gin (to_tsvector('simple', title || ' ' || description))
    WHERE deleted_at IS NULL AND status = 'READY' AND visibility = 'public';
CREATE INDEX idx_audio_title_trgm ON audio USING gin (title gin_trgm_ops);
CREATE INDEX idx_creators_search_trgm ON creators
    USING gin ((channel_name || ' ' || handle) gin_trgm_ops)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_creators_search_trgm;
DROP INDEX IF EXISTS idx_audio_title_trgm;
DROP INDEX IF EXISTS idx_audio_search_fts;
DROP TABLE IF EXISTS listening_history;
DROP TABLE IF EXISTS playlist_items;
DROP TABLE IF EXISTS playlists;
DROP TABLE IF EXISTS follows;
DROP TABLE IF EXISTS comment_likes;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS audio_likes;
