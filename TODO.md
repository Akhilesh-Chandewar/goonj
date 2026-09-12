# TODO — Remaining Work

Living checklist of what is left, audited against PLAN.md and the actual
codebase after the Phase 5 completion slice (notifications, reports,
discovery, analytics rollup, scheduling, web surface, OpenAPI codegen,
integration tests, JWT rotation, CDN support). Update this file as items
land; move resolved battles to PROBLEMS.md, not here.

---

## Phase 5 — AI & scale (current phase)

- [x] **Embedding-based recommendations** — `/recommendations` (seeded from
      the viewer's last listened episode, trending fallback) and
      `/recommendations/audio/{id}` ("more like this") via `chunk_embeddings`.
- [x] **Trending** — Redis ZSET (`goonj:trending:audio`), time-windowed
      engagement weights, worker recompute loop (15 min default,
      `TRENDING_INTERVAL` to override), public `/trending` with a 1-minute
      in-process cache; **Discover page** in the web client.
- [x] **Codegen** — `packages/api-client` generates TS types from
      `openapi.yaml` (openapi-typescript) with a typed fetch wrapper
      (`client.get("/trending")` returns spec-inferred JSON). Web's discover
      page consumes it. Remaining: adopt the client in older pages and
      oapi-codegen for the Go handlers.
- [ ] **CDN + adaptive streaming + distributed workers** —
      `S3_CDN_BASE_URL` support shipped (playback URLs become CDN/key when
      set; compose + .env.example wired). Still open: actual CDN in front of
      storage, HLS/DASH adaptive variants, Asynq queue priorities.

## Promised by earlier phases

- [x] **Notifications** — table + module, went-live fan-out, recording-ready
      pings, REST surface, and the web `NotificationBell`.
- [x] **Scheduling + reminders** — `scheduled_at`, schedule/upcoming
      endpoints + studio form and /live upcoming list, worker reminder loop.
- [x] **Live analytics rollups** — `live_analytics` + presence sampling,
      rolled up at finalize-recording time.
- [x] **Moderation / reports** — table + module, coalesced unique index
      dedupe, moderator queue page, report buttons on audio/live pages.
- [x] **Recording auto-start** — PROBLEMS #7 closed.

## Engineering hygiene

- [x] **OpenAPI refreshed** — v0.5.0 documents the Phase 5 surface.
- [x] **Test coverage** — module unit tests (reports, notifications, live,
      auth rotation) + store-layer integration tests in
      `internal/integration` (notifications store, reports lifecycle, live
      scheduling), running against real Postgres via `GOONJ_TEST_DATABASE`;
      CI boots a pgvector service container for them. Still open:
- [ ] **More store/service coverage** — remaining repositories (audio,
      playlists, social, history, search) and E2E flows into CI.
- [x] **Docker-only Postgres policy** — documented in README; the stack's
      pgvector container is the single database (host port via
      `POSTGRES_HOST_PORT`), migrations applied by the seed image.
- [x] **JWT secret rotation** — `JWT_SECRET` signs, `JWT_SECRETS_PREVIOUS`
      validates (API + ws-gateway); zero-downtime rotation documented in
      README/.env.example, with unit tests.
- [ ] **Shared nav** — pages still own their headers; extract one
      layout-level nav (bell included).
- [ ] **Secure cookies review** — refresh token still rides in localStorage
      (Phase 2 bridge); move to httpOnly SameSite cookie behind TLS.

## Prod-readiness (past compose)

- [x] **TURN/TLS config path** — `LIVEKIT_CLIENT_URL` separates the
      browser-facing wss:// endpoint from the server-side one; TURN/published
      UDP range live in the LiveKit server config (documented; PROBLEMS #5).
- [ ] Actually deploy TLS + TURN (IaC), real S3/R2 + CDN in front of
      processed audio (`S3_CDN_BASE_URL` is wired, the infra isn't).
- [ ] Retune `SEMANTIC_MIN_SIMILARITY` for the production embedding model.
