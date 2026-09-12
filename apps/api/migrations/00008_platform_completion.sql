-- Phase 5 — platform completion: notifications, moderation reports, and
-- live analytics rollups (PLAN §3 identity/content/live gaps).

-- +goose Up
-- Session scheduling (TODO: "Scheduling + reminders"): a SCHEDULED session
-- carries a planned start; followers get a reminder ~10 min before.
ALTER TABLE live_sessions ADD COLUMN scheduled_at TIMESTAMPTZ;
CREATE INDEX idx_live_sessions_upcoming ON live_sessions (scheduled_at ASC)
    WHERE status = 'SCHEDULED' AND scheduled_at IS NOT NULL;

-- User notifications (went live, recording ready, new follower, reminders).
-- Delivered lazily: clients poll GET /notifications; no push infra needed.
CREATE TABLE notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type        TEXT NOT NULL, -- live_started | recording_ready | new_follower | session_reminder
    title       TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    body        TEXT NOT NULL DEFAULT '',
    -- Optional links to the subject of the notification.
    session_id  UUID REFERENCES live_sessions (id) ON DELETE CASCADE,
    audio_id    UUID REFERENCES audio (id) ON DELETE CASCADE,
    actor_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
-- Partial index for the unread badge.
CREATE INDEX idx_notifications_unread ON notifications (user_id, created_at DESC)
    WHERE read_at IS NULL;

-- Content reports (moderation queue): listeners flag audio or sessions.
CREATE TABLE reports (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id  UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Exactly one target is set.
    audio_id     UUID REFERENCES audio (id) ON DELETE CASCADE,
    session_id   UUID REFERENCES live_sessions (id) ON DELETE CASCADE,
    reason       TEXT NOT NULL CHECK (reason IN ('spam','harassment','copyright','explicit','misinformation','other')),
    details      TEXT NOT NULL DEFAULT '' CHECK (length(details) <= 1000),
    status       TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RESOLVED','DISMISSED')),
    resolved_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One open report per (reporter, target) pair; resolved ones allow re-reporting.
    CONSTRAINT reports_target CHECK (audio_id IS NOT NULL OR session_id IS NOT NULL)
);
-- NULLs never collide in a UNIQUE constraint, so dedupe on coalesced ids.
-- The store's ON CONFLICT clause repeats these exact expressions.
CREATE UNIQUE INDEX idx_reports_no_dupes ON reports
    (reporter_id, COALESCE(audio_id, '00000000-0000-0000-0000-000000000000'::uuid),
     COALESCE(session_id, '00000000-0000-0000-0000-000000000000'::uuid), status);
CREATE INDEX idx_reports_open ON reports (status, created_at) WHERE status = 'PENDING';

-- Per-session live analytics rollups (written once at stream end by the
-- worker/API; presence snapshots stay in Redis per PLAN §5).
CREATE TABLE live_analytics (
    session_id      UUID PRIMARY KEY REFERENCES live_sessions (id) ON DELETE CASCADE,
    peak_concurrent INTEGER NOT NULL DEFAULT 0,
    total_unique    INTEGER NOT NULL DEFAULT 0,
    avg_concurrent  INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    reactions_total INTEGER NOT NULL DEFAULT 0,
    chat_total      INTEGER NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Listener presence snapshots (sampled by the worker while a session is
-- LIVE; avg_concurrent derives from these at rollup time).
CREATE TABLE live_presence_samples (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id  UUID NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    concurrent  INTEGER NOT NULL CHECK (concurrent >= 0),
    sampled_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_presence_samples ON live_presence_samples (session_id, sampled_at);

-- +goose Down
DROP INDEX IF EXISTS idx_live_sessions_upcoming;
ALTER TABLE live_sessions DROP COLUMN IF EXISTS scheduled_at;
DROP TABLE IF EXISTS live_presence_samples;
DROP TABLE IF EXISTS live_analytics;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS notifications;
