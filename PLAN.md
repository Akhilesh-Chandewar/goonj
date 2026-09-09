# Goonj (गूंज) — "Every voice deserves an echo"

Audio-only platform: **YouTube's discoverability + Spotify's player + Twitter Spaces live audio.**
No video, ever. **Live-first:** the core differentiator is Go Live, and everything created in a live
session (recording, chat, transcript) is saved as permanent platform content.

---

## 1. Stack

| Layer | Choice |
|---|---|
| API | Go 1.27 + chi router, modular DDD layout |
| DB | PostgreSQL 16, UUIDv7 PKs, pgx/v5 + sqlc, goose migrations |
| Cache / realtime state | Redis 7 (sessions, rate limits, presence, trending ZSETs, pub/sub) |
| Jobs | Asynq (Redis-backed): retries, scheduled tasks, DLQ |
| Object storage | S3-compatible — MinIO dev, S3/R2 prod; presigned multipart uploads |
| Audio processing | FFmpeg workers: transcode L/M/H, EBU R128 loudnorm, waveform peaks, ffprobe metadata |
| **Live audio** | **LiveKit SFU** (audio-only rooms) — swappable provider interface |
| Live realtime | Go WS gateway: chat, presence, reactions, moderation events via Redis pub/sub (never raw audio over WS) |
| Frontend | Next.js 15 (App Router) + TypeScript + Tailwind + TanStack Query + Zustand + shadcn/Radix + wavesurfer.js |
| Contract | OpenAPI 3.1 first → oapi-codegen (Go) + generated TS client |

## 2. Monorepo

```
goonj/
├── apps/
│   ├── api/            # Go REST API (cmd/api + internal/modules/*)
│   ├── worker/         # audio pipeline, trending, analytics, notifications
│   ├── ws-gateway/     # live chat / presence / reactions
│   └── web/            # Next.js 15
├── packages/
│   ├── api-client/     # generated from openapi.yaml
│   └── shared/
├── openapi/openapi.yaml
├── docker-compose.yml  # web, api, worker, ws, postgres, redis, minio, livekit, mailpit
├── Makefile · .env.example · README.md
```

Go modules: `auth, users, creators, audio, playlists, comments, likes, subscriptions, search,
recommendations, notifications, analytics, moderation, live`. Each = handler → service → repository.
Live streaming and search/recommendations sit behind interfaces (providers swappable).

## 3. Database (PostgreSQL, normalized, indexed, soft deletes + timestamps)

- **Identity:** users, profiles, creators, subscriptions (unique user+creator)
- **Content:** categories, tags, audio_tags, audio (draft/processing/scheduled/published/private),
  audio_metadata (duration, language, waveform peaks JSONB), audio_files (quality variants), chapters, transcripts
- **Engagement:** likes, comments (self-referencing parent_id for replies), comment_likes, playlists,
  playlist_items (position), listening_history, listening_sessions, notifications, reports
- **Analytics:** audio_analytics, creator_analytics (worker-aggregated, never per-event writes)
- **Live:** live_sessions (SCHEDULED/LIVE/ENDED/CANCELLED), live_streams, live_viewers (snapshots only —
  presence in Redis), live_chat_messages (persisted async), live_reactions (aggregated windows),
  live_moderators, live_events, live_analytics

Indexes: audio(status, published_at DESC), creator_id, category_id, GIN full-text + pg_trgm for
search/autocomplete, unique (user_id, audio_id) likes, listening_history(user_id, updated_at DESC).

## 4. API (REST /api/v1, cursor pagination, RFC-7807 errors, request IDs)

- Auth: register/login/refresh/logout, JWT access (15m) + rotating refresh (httpOnly cookie), RBAC
  (USER/CREATOR/MODERATOR/ADMIN), OAuth-ready
- Content: GET/POST/PATCH/DELETE /audio, like/unlike, comments (+replies, likes), follow, playlists
- Discovery: /search, /trending, /recommendations, /categories
- Uploads: POST /uploads → presigned multipart URL; client uploads direct to storage
- Analytics: POST /analytics/events (batched)
- **Live:** POST/GET/PATCH/DELETE /live, /live/:id/start|end|join|leave, /live/:id/chat,
  /live/:id/reactions, /live/:id/report, /live/:id/reminder, WS /ws/live/:id
- Security: validation everywhere, MIME sniffing + ffprobe validation, upload caps, signed URLs,
  rate limits (Redis), CSRF, secure headers, audit log, expiring stream tokens (never expose permanent
  credentials to the frontend)

## 5. Core pipelines

**Upload/processing:** client → presigned multipart → storage → Asynq task → FFmpeg worker
(3 qualities, loudnorm, waveform, duration) → audio_files → READY. API never touches large files.

**Live:** creator mic (audio-only) → LiveKit SFU → listeners. WS gateway fans out chat/presence/reactions
through Redis pub/sub. Presence + listener counts live in Redis, snapshots to PG. Reactions aggregated
in windows. Chat persisted asynchronously.

**Live → Saved (core requirement):** on stream end → egress recording → FFmpeg pipeline →
creator chooses **Save as draft** or **Publish as episode**; final chat log and transcript (optional)
attach to the episode; live analytics roll up into creator analytics.

**Analytics:** client batches events → queue → worker → aggregate tables.
**Trending:** configurable time-decayed score → Redis ZSET, recomputed periodically.
**Recommendations:** Recommender interface — v1 content-based; later pgvector semantic; later collaborative.

## 6. Roadmap — Live-first

| Phase | Scope | Exit criteria |
|---|---|---|
| **0 — Scaffold** | Monorepo, docker-compose, OpenAPI skeleton, migrations, config, logging, CI, seed | `docker compose up` green, health checks pass |
| **1 — Foundation** | Auth + RBAC, profiles/creators, storage, minimal upload→pipeline→episode (needed because live recordings become episodes), persistent audio player shell | Register → create channel → upload → process → play |
| **2 — Live Core** 🔴 | Go Live flow (create session → start), LiveKit audio-only broadcast, live player + waveform, WS chat, presence/listener counts, reactions, basic moderation (delete/timeout/block, slow mode), Live Now discovery + /live pages, live avatars | Creator goes live, listener joins, chats, reacts |
| **3 — Live→Saved** | Egress recording → draft/publish as episode, chat log persistence, live analytics (peak/concurrent/retention charts), notifications (went live, starting soon), scheduling + reminders, optional live transcript | Ended stream becomes a published episode with saved chat + analytics |
| **4 — On-demand & social** | Home feed, search (PG FTS + trigram), audio pages, likes, comments, follows, playlists, history/continue-listening, subscriptions feed, Creator Studio analytics | Full browse/engage loop |
| **5 — AI & scale** | Transcription, semantic search (pgvector), AI summaries, embedding recs, CDN, adaptive streaming, distributed workers | — |

## 7. Engineering principles

SOLID, domain-driven modules, type safety end-to-end (OpenAPI → Go + TS), idempotent workers with
retries + DLQ, pagination everywhere, indexed queries, transaction boundaries, graceful failure,
structured JSON logs, health/readiness checks. Never: giant services, business logic in controllers or
React components, synchronous large-file processing, per-event DB writes, unbounded responses.
