-- Goonj initial schema: identity core (users, profiles, creators, subscriptions).
-- UUIDv4 PKs for now; content tables (audio, live, engagement) land with Phase 1/2.
-- goose wraps each migration in its own transaction — no explicit BEGIN/COMMIT.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'USER'
        CHECK (role IN ('USER', 'CREATOR', 'MODERATOR', 'ADMIN')),
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);
CREATE INDEX idx_users_role ON users (role) WHERE deleted_at IS NULL;

CREATE TABLE profiles (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    username   CITEXT NOT NULL UNIQUE
        CHECK (username ~ '^[a-z0-9_]{3,30}$'),
    display_name TEXT NOT NULL,
    bio        TEXT NOT NULL DEFAULT '',
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE creators (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    handle         CITEXT NOT NULL UNIQUE
        CHECK (handle ~ '^[a-z0-9_]{3,30}$'),
    channel_name   TEXT NOT NULL,
    tagline        TEXT NOT NULL DEFAULT '',
    banner_url     TEXT,
    is_live        BOOLEAN NOT NULL DEFAULT FALSE,
    subscriber_count BIGINT NOT NULL DEFAULT 0 CHECK (subscriber_count >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);
CREATE INDEX idx_creators_live ON creators (is_live) WHERE deleted_at IS NULL AND is_live;

CREATE TABLE subscriptions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    creator_id   UUID NOT NULL REFERENCES creators (id) ON DELETE CASCADE,
    notify_live  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, creator_id)
);
CREATE INDEX idx_subscriptions_creator ON subscriptions (creator_id);

CREATE TABLE schema_notes (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS schema_notes;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS creators;
DROP TABLE IF EXISTS profiles;
DROP TABLE IF EXISTS users;
