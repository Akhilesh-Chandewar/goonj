# Goonj (गूंज) — *Modern Day Radio*

Audio-only content platform: **the discoverability of YouTube, the player of
Spotify, and live shows anyone can tune into.** The flagship demo: a creator
goes live speaking one language — listeners see **live translated captions
and hear a translated voice in near-realtime** in the language they pick.
No video, ever. Live-first: everything created in a live session (recording,
chat, transcript) becomes permanent platform content.

See [PLAN.md](./PLAN.md) for the product roadmap and [PROBLEMS.md](./PROBLEMS.md)
for the engineering battle log.

---

## 1. What it does (feature map)

| Area | What ships |
|---|---|
| **Live shows** | Go-live audio broadcasts (LiveKit SFU, audio-only), realtime chat, presence/listener counts, reactions, moderation events over a WebSocket gateway |
| **Realtime translation** 🔊 | Server-side always-on translator: creator speaks language A, listeners read captions in B/C/D as words are spoken, and can toggle **translated voice playback** in the browser |
| **Creator browser translation** | Alternative path: the creator's browser opens a WebRTC session straight to OpenAI's Realtime translation endpoint (audio never touches Goonj) — lowest possible latency |
| **Live → Saved** | Stream end → room-composite egress recording → FFmpeg pipeline → draft episode → one-click publish. Chat log and captions attach to the episode |
| **On-demand** | Uploads via presigned multipart → worker pipeline (loudnorm, 3 quality variants, waveform, ffprobe validation) → persistent player |
| **AI** | Transcription (whisper via OpenAI-compatible API), per-chunk embeddings, **semantic search** (pgvector HNSW), AI episode summaries, embedding-based recommendations |
| **Discovery** | Full-text + trigram search, creators search, **time-decayed trending** (Redis ZSET, worker recompute), **"more like this"** and personalized recommendations, category feed |
| **Social** | Likes, threaded comments with likes, follows, playlists, listening history / continue-listening, subscriptions feed, creator pages |
| **Notifications** | Went-live fan-out to followers, recording-ready pings, scheduled-show reminders (worker loop, ~10 min before), unread badge REST API |
| **Scheduling** | Schedule a show (planned start), public upcoming list, automatic follower reminders |
| **Analytics** | Per-minute live presence sampling → peak/unique/avg/duration rollups per session at stream end; creator studio stats |
| **Moderation** | Listener reports on episodes/sessions, moderator queue with resolve/dismiss, duplicate collapse via expression index |

---

## 2. Architecture

```
                         ┌────────────────────────── BROWSER ──────────────────────────┐
                         │  Next.js 16 (App Router) · TypeScript · Tailwind · Zustand  │
                         │  player (quality variants) · studio · live room · captions  │
                         └──────┬─────────────┬──────────────┬─────────────┬───────────┘
                                │ REST        │ WS (chat/    │ WS (transl. │ WebRTC
                                │ /api/v1     │ presence)    │ audio PCM)  │ (media)
                         ┌──────▼─────┐ ┌─────▼──────┐ ┌────▼─────────┐ ┌─▼──────────┐
                         │  api (Go)  │ │ ws-gateway │ │ ws-gateway   │ │  LiveKit   │
                         │  chi DDD   │ │ (control)  │ │ (translate)  │ │  SFU       │
                         └──┬───┬───┬─┘ └─────┬──────┘ └────▲─────────┘ └─┬────┬─────┘
                            │   │   │         │             │             │    │
              ┌─────────────┘   │   └─────────┼─────────────┘             │    │ track egress
              │             ┌───▼───┐         │                       (creator │ (raw PCM)
              │             │ Redis │◄────────┘  pub/sub: rooms,      mic in)     │
              │             │ pubsub│  goonj:live:<id>:ch / :tts:<lang>          │
              │             │ asynq │  goonj:translate:ctl (translator control)  │
              │             └───┬───┘                                            │
              │   ┌─────────────┼──────────────────────────────┐                 │
              │   │             │                              │                 │
         ┌────▼───▼───┐   ┌─────▼─────┐                 ┌──────▼──────┐   ┌──────▼───────┐
         │ PostgreSQL │   │  worker   │                 │  livekit    │   │ livekit-egress│
         │ 16+pgvector│   │  (Go)     │                 │  SFU        │   │ (recording +  │
         │ goose mgmt │   │ FFmpeg AI │                 └─────────────┘   │  PCM pump →)  │
         └────────────┘   │ translator│                                   └──────────────┘
                          └─────┬─────┘        ┌────────────┐
                                │              │ floci (S3) │── uploads, processed audio,
                          ┌─────▼──────┐       └────────────┘   recordings, transcripts
                          │ OpenAI-com │   (Groq for STT/chat; OpenAI for Realtime)
                          │ compatible │
                          └────────────┘
```

### The realtime translation data path (the demo centerpiece)

Two cooperating paths, both streaming (no batching beyond one frame):

**A. Server-side translator (always-on, operates per live session):**

```
creator mic ──WebRTC──► LiveKit SFU
                            │ StartTrackEgress (websocket_url output)
                            ▼
            worker PCMPump :9600  ◄── raw PCM16 48kHz mono frames
                            │  downsample 48k→24k (decimation, ~0 added latency)
                            ▼
            one OpenAI Realtime translation session per target language
            (gpt-realtime-translate, WS, 24kHz PCM16 in, deltas out)
                            │
            ┌───────────────┼───────────────────┐
            ▼               ▼                   ▼
    transcript deltas  final lines      translated audio (PCM16 24k)
            │               │                   │
            └──────► Redis goonj:live:<id>:ch    └──► Redis goonj:live:<id>:tts:<lang>
                            │                                    │
                            ▼                                    ▼
                  ws-gateway (text JSON)              ws-gateway (binary frames)
                            │                                    │
                            ▼                                    ▼
                  listener caption bar           listener WebAudio playback
                  (partial while creator speaks) (🔊 toggle, ≤600ms latency cap)
```

* **Control plane:** creator picks languages in the studio → `PUT /live/{id}/translate/config` → settings persist + a `ConfigEvent` publishes on `goonj:translate:ctl` → the worker's translator reconciles: starts the track egress + one OpenAI session per language, applies edits live (add/remove languages), stops everything at stream end.
* **Backpressure policy:** every hop is drop-oldest. The PCMPump caps its window (~2s), WebAudio caps playback queue (600ms), Redis pub/sub is fire-and-forget. Translation stays realtime; degradation is quality, never growing latency.
* **Late joiners** hydrate the last 100 caption lines per language from `live_captions` (`GET /live/{id}/translate/captions?lang=…`).
* **Source-language captions** are also relayed/persisted (`live_captions_source`) — listeners can follow the original text too.

**B. Creator-browser path (self-serve, zero server hops):**
`TranslationStudio` mints an ephemeral OpenAI client secret per language
(`POST /live/{id}/translate/session` → `gpt-realtime-translate`), opens a
WebRTC peer connection **directly to OpenAI** from the creator's browser,
and relays caption deltas to the same ingest endpoint. Same fan-out, same
persistence — the browser substitutes for the egress+pump machinery, and
mic audio never transits Goonj.

> Provider reality check (verified against Groq's catalog): Groq serves
> **whisper STT + chat** but **no `/embeddings` and no Realtime API**. So the
> deployment matrix is: Groq powers transcription + summaries
> (`EMBEDDINGS_DISABLED=1` falls back to the local hash embedder), OpenAI
> powers embeddings + Realtime translation. Everything degrades gracefully —
> no key, no feature, no crashes.

### AI pipeline

```
upload → Asynq audio:process → FFmpeg (loudnorm EBU R128, 3 variants,
        waveform peaks, ffprobe validation) → READY
        → audio:transcribe (whisper-large-v3-turbo via Groq) → transcript_chunks
        → audio:embed → chunk_embeddings (pgvector 1536d) + audio_summaries (gpt-oss-120b)
```

* Embeddings: OpenAI-compatible `/embeddings` when available; otherwise a deterministic FNV hashed bag-of-bigrams (1536d) — honest retrieval for dev, zero cost.
* Semantic search: cosine distance over HNSW with a similarity floor (`SEMANTIC_MIN_SIMILARITY`, default 0.15) and model filtering (query vectors must match the model that embedded the chunks).
* Recommendations: "more like this" = per-episode centroid → ANN; personalized = seeded by the listener's most recent history entry, trending fallback.

### Live → Saved pipeline

```
POST /live/{id}/start ──► auto room-composite egress (server-side, no silent lead-in)
stream ends ──► egress finalizes to MP3 in floci ──► Asynq live:finalize-recording
             ──► draft episode row ──► audio pipeline ──► creator publishes
chat log + captions persist alongside; presence samples roll up into live_analytics
```

---

## 3. Technology stack (every layer)

### Backend — Go 1.27 (`apps/api`, single module, four binaries)

| Concern | Choice | Where |
|---|---|---|
| HTTP router | `chi` v5 + `chi/cors` | `internal/app` (composition root), per-module routers |
| Layout | DDD-ish modules: handler → service → store, each module owns its routes | `internal/modules/{auth,live,audio,search,translation,notifications,reports,engagement,social,playlists,history,ai,health}` |
| Database | PostgreSQL 16 + **pgvector** (HNSW), UUIDv7-style PKs | migrations in `apps/api/migrations` (goose) |
| SQL | `pgx/v5` + `pgxpool` (no ORM) | every module's `store.go` |
| Migrations | `goose` embedded; the `seed` binary applies them + dev data | `platform/migrate.go`, `cmd/seed` |
| Cache/realtime | Redis 7: presence TTL keys, pub/sub (rooms, translation control, caption + audio fan-out), rate limiting, refresh-token store | `platform/redis.go`, ws-gateway, translator |
| Jobs | Asynq (Redis-backed): `audio:process`, `live:finalize-recording`, `audio:transcribe`, `audio:embed` — idempotent, retrying, DLQ | `internal/shared/tasks`, `cmd/worker` |
| Live media | **LiveKit SFU** (audio-only rooms) via Twirp JSON client — room-composite egress (recording) + track egress (PCM pump), swappable `Streamer`/`Recorder` interfaces | `infrastructure/livekit` |
| WebSockets | `coder/websocket`; ws-gateway fans chat/presence/reactions/captions via Redis; separate binary streams translated audio frames | `cmd/ws-gateway` |
| AI providers | OpenAI-compatible REST: whisper STT, chat summaries, embeddings; Realtime WS for translation sessions. Works with OpenAI, **Groq**, Azure gateways, llama.cpp | `infrastructure/openai`, `modules/ai` |
| Audio processing | FFmpeg/ffprobe CLI (loudnorm, mp3 variants, waveform peaks) | `cmd/worker/main.go` |
| Auth | JWT access (15m) + opaque single-use refresh tokens (SHA-256 hashed in Redis, rotation on use); **zero-downtime key rotation** via `JWT_SECRETS_PREVIOUS`; RBAC USER/CREATOR/MODERATOR/ADMIN | `modules/auth` |
| Storage | S3-compatible (floci/MinIO dev, S3/R2 prod): presigned multipart uploads, dual endpoints (internal vs browser-facing), optional **CDN base URL** for playback | `infrastructure/storage` |
| Config/logging | Env-based `platform.Config` with dev defaults; slog JSON logs; health/readiness endpoints | `platform/` |
| Object model | Presence in Redis only (snapshots to PG), aggregates in `live_analytics`, per-event writes forbidden by design | `modules/live`, migrations |

### Frontend — Next.js 16 (`apps/web`, bun workspace)

* **Next.js 16 App Router + React 19**, TypeScript strict, Tailwind v4
* **Zustand** player store (persistent bar, queue, progress beacons to history API)
* LiveKit components-react for room joining; raw WebRTC + WebAudio for the translation paths
* Custom `api.ts` client: bearer auth, **single-flight refresh-on-401**, typed helpers
* `@goonj/shared` workspace package: API routes/constants shared client↔server
* `@goonj/api-client` workspace package: **generated** OpenAPI types (openapi-typescript) + typed fetch wrapper

### Contracts

* `openapi/openapi.yaml` — OpenAPI 3.1, v0.5.0, documents the full surface (auth, live, audio, notifications, moderation, discovery, scheduling, transcript)
* `packages/api-client` generates TS from it (`bun --filter '@goonj/api-client' generate`); Go handlers remain hand-written (oapi-codegen adoption tracked in TODO.md)

### Infrastructure (docker-compose, `make up`)

| Service | Image | Notes |
|---|---|---|
| postgres | `pgvector/pgvector:pg16` | the only Postgres (host port via `POSTGRES_HOST_PORT`) |
| redis | `redis:7-alpine` | pub/sub + asynq + presence + tokens |
| livekit | `livekit/livekit-server` | audio-only config, `use_external_ip:false` for compose networking |
| livekit-egress | `livekit/egress` | shares livekit's netns (mixed-content workaround); dials `worker:9600` for translation PCM |
| floci | `floci/floci` | S3-compatible, path-style |
| api / worker / ws-gateway / seed | `Dockerfile.go` | one image, four entrypoints |
| web | `Dockerfile.web` | Next.js standalone |

---

## 4. Repository layout

```
goonj/
├── apps/
│   ├── api/                    # Go module (all backend binaries)
│   │   ├── cmd/
│   │   │   ├── api/            # REST API (chi, modular DDD)
│   │   │   ├── worker/         # FFmpeg pipeline, AI, discovery loops, realtime translator
│   │   │   ├── ws-gateway/     # chat/presence/captions WS + translated-audio WS
│   │   │   ├── seed/           # migrations + dev data
│   │   │   └── enqueue/        # utility: enqueue ad-hoc Asynq tasks
│   │   ├── internal/
│   │   │   ├── modules/        # auth, live, audio, search, translation,
│   │   │   │                   # notifications, reports, engagement, social,
│   │   │   │                   # playlists, history, ai, health
│   │   │   │                   # (handler → service → store per module)
│   │   │   ├── infrastructure/ # livekit (SFU/egress/PCM pump), openai (REST/Realtime/WS), storage (S3)
│   │   │   ├── integration/    # store-layer tests vs real Postgres (opt-in env)
│   │   │   ├── platform/       # config, logger, pg, redis, httpserver, migrate
│   │   │   └── app/            # composition root (route mounting, DI)
│   │   └── migrations/         # goose SQL (00001…00008)
│   └── web/                    # Next.js 16 (App Router)
│       └── src/
│           ├── app/            # pages: /, /live, /live/[id], /discover, /audio/[id],
│           │                   # /search, /library, /creators/[id], /studio/*, /login
│           ├── components/     # player-bar, track-card, notification-bell,
│           │                   # report-button, CaptionBar, TranslationStudio, …
│           ├── lib/            # api client, engagement hooks, livekit hooks,
│           │                   # use-captions, use-translated-audio
│           └── stores/         # zustand player
├── packages/
│   ├── shared/                 # @goonj/shared: API_ROUTES, constants
│   └── api-client/             # @goonj/api-client: generated types + typed fetch
├── openapi/openapi.yaml        # API contract (v0.5.0)
├── docker-compose.yml          # full local stack
├── Dockerfile.go / Dockerfile.web
├── Makefile · .env.example
└── PLAN.md · PROBLEMS.md · TODO.md
```

## 5. API surface (summary — full detail in openapi.yaml)

* **Auth** `/auth/register|login|refresh|logout` — JWT + rotating refresh
* **Live** `/live` CRUD, `/{id}/start|end|join|leave|heartbeat`, `/recording`,
  `/{id}/schedule`, `/upcoming`, reactions/report; **translation control
  plane** `/{id}/translate/config|session|captions|languages`
* **Audio** `/audio` feed, uploads (presigned multipart), `/{id}/playback|
  transcript`, like, comments (+replies/likes), studio stats
* **Discovery** `/search` (+suggestions), `/trending`, `/recommendations[...]`,
  `/categories`
* **Social** `/creators/{id}/follow`, `/playlists[...]`, `/history[...]`,
  `/subscriptions/feed`
* **Notifications** `/notifications`, `/unread-count`, `/read-all`, `/{id}/read`
* **Moderation** `/reports` (file / queue), `/{id}/resolve|dismiss`
* **Sockets** `/ws/live/{id}` (chat/presence/captions), `/ws/translate/{id}/{lang}` (binary PCM)

## 6. Quick start

Prereqs: Docker, Go 1.27, [bun](https://bun.sh) ≥ 1.4, make.

```bash
# 1. Configure
cp .env.example .env        # edit: ports (host may be shifted), AI keys

# 2. Full stack in containers
make up                     # postgres, redis, floci, livekit, egress, api, worker, ws, web, seed

# 3. Or natively for hot reload (compose infra + local binaries)
docker compose up -d postgres redis floci livekit livekit-egress
docker compose run --rm seed          # migrations + seed data
cd apps/api && go run ./cmd/api       # :8080
cd apps/api && go run ./cmd/worker    # + translator pump :9600
cd apps/api && go run ./cmd/ws-gateway
make web-install && make web-dev      # http://localhost:3000
```

Verify:

```bash
curl http://localhost:8080/api/v1/ping
curl http://localhost:8080/readyz
open http://localhost:3000
```

### Screenshots

A current tour of the whole UI (public pages, creator studio, live room,
audio page with transcript + AI summary, moderator queue) — rendered by
[Playwright](scripts/screenshots.mjs) against the compose stack.

#### Discovery & playback

| Home | Live directory | Discover |
|---|---|---|
| ![Home](screenshots/01-home.png) | ![Live directory](screenshots/02-live-directory.png) | ![Discover](screenshots/03-discover.png) |

| Search | Login | Library |
|---|---|---|
| ![Search](screenshots/04-search.png) | ![Login](screenshots/05-login.png) | ![Library](screenshots/11-library.png) |

| Audio page — transcript + AI summary | Live room |
|---|---|
| ![Audio page](screenshots/10-audio-page.png) | ![Live room](screenshots/09-live-room.png) |

#### Creator & moderation

| Studio — go live | Studio — upload | Studio — content |
|---|---|---|
| ![Studio live](screenshots/06-studio-live.png) | ![Studio upload](screenshots/07-studio-upload.png) | ![Studio content](screenshots/08-studio-content.png) |

| Moderator queue |
|---|
| ![Moderator queue](screenshots/12-moderator-queue.png) |

Regenerate against a running stack:

```bash
bun scripts/screenshots.mjs     # Playwright; writes 12 PNGs to screenshots/
```

### Running the realtime translation demo

1. Put an **OpenAI** key in `.env` (`OPENAI_API_KEY`) — Realtime translation is
   OpenAI-only. (Groq covers STT/summaries; see the provider matrix in
   `.env.example`.)
2. Register a creator, go live from `/studio/live` (mic permission).
3. In the On-Air panel: pick target languages (e.g. हिन्दी, Español) → enable →
   **Start translating** (server-side) — the worker spins up per-language
   sessions automatically.
4. In a second browser (or incognito), open the same room as a listener:
   captions stream in the chosen language **while the creator is speaking**,
   and the 🔊 toggle plays the translated voice.
5. Late joiners get caption history immediately; switching languages re-buffers
   under a second.

### AI provider matrix

| Capability | Groq | OpenAI | Without any key |
|---|---|---|---|
| Transcription (episodes) | ✅ `whisper-large-v3-turbo` | ✅ `whisper-1` | skipped |
| AI summaries | ✅ `openai/gpt-oss-120b` | ✅ `gpt-4o-mini` | extractive baseline |
| Embeddings / semantic search | ❌ (set `EMBEDDINGS_DISABLED=1`) | ✅ `text-embedding-3-small` | hash embedder |
| Realtime translation + voice agent | ❌ | ✅ `gpt-realtime-translate` | feature hidden (503) |

## 7. Development

```bash
make go-build     # binaries → ./bin
make go-test      # Go tests (unit; integration skipped without a DB)
make typecheck    # TS across all workspaces
make ci           # everything CI runs
```

**Store-layer integration tests** (real Postgres, opt-in):

```bash
GOONJ_TEST_DATABASE="postgres://goonj:goonj@localhost:15433/goonj?sslmode=disable" \
  go test ./internal/integration/...        # from apps/api
```

CI runs unit + integration tests against a pgvector service container, plus
TS typecheck/build on every push (`.github/workflows/ci.yml`).

### Database policy — Docker is the only Postgres

PostgreSQL runs exclusively as the compose container; a host-installed server
is not used and can be removed without affecting the stack (pgAdmin still
connects to the container over the published port).

```bash
docker compose up -d postgres
docker compose exec postgres pg_isready -U goonj -d goonj
docker compose run --rm seed     # migrations + seed (idempotent)
```

### JWT secret rotation (zero downtime)

1. Generate a new secret; set `JWT_SECRET=<new>` **and**
   `JWT_SECRETS_PREVIOUS=<old>`, deploy.
2. Old tokens keep validating (API + ws-gateway) while every client refreshes
   within 15 minutes.
3. Remove `JWT_SECRETS_PREVIOUS` at the next deploy.

## 8. Latency: budget, measurements, and how to go lower

The realtime contract is **"drop frames, not deadlines"** — every hop in the
live paths is drop-oldest, so degradation is quality, never growing latency.

### Measured end-to-end (compose stack, this machine, 2026-09-12)

API latencies below are HTTP request→response (`curl`-equivalent timing);
pipeline numbers are poll-detected completion times (include scheduler jitter
and 1s polling granularity).

| Path | Stage | Measured |
|---|---|---|
| REST | register/login (bcrypt cost) | 60–100 ms |
| REST | live CRUD, heartbeat, stats, captions read | 1–5 ms |
| REST | live start (room create + token) | 80–650 ms first call, then ~80 ms |
| REST | presign upload / playback | 1–9 ms |
| Batch | 2.2 MB upload → floci | 40–140 ms |
| Batch | ffmpeg pipeline (loudnorm + 3 variants + waveform, 25 s audio) | ~2.0 s |
| Batch | Groq `whisper-large-v3-turbo` transcription (25 s audio) | ~1.0 s |
| Batch | Groq `gpt-oss-120b` summary (2-chunk transcript) | ~2.0 s |
| Async | went-live notification fan-out (follow→unread badge) | ~260 ms |
| Batch | egress finalize → draft → publish (live→saved) | seconds (Chrome pipeline) |

### Realtime translation — latency budget (server path)

| Hop | Budget | Mechanism |
|---|---|---|
| Creator mic → LiveKit SFU | ~20–60 ms | WebRTC/Opus, jitter buffer |
| SFU → TrackEgress WS → PCMPump | <10 ms | raw PCM push, drop-oldest window (~2 s cap) |
| 48k→24k decimation | ~0 ms | integer decimation, no filter pass |
| OpenAI translation session | 300–800 ms | model-dependent; partials stream immediately |
| Caption delta → Redis → ws-gateway → listener | 5–15 ms | pub/sub fan-out, no persistence on hot path |
| Translated PCM → Redis → listener WebAudio | 5–15 ms | binary WS frames, 600 ms playback-queue cap |
| **Speech → listener ears (total)** | **~0.4–0.9 s** | dominated by the model, not the plumbing |
| Speech → listener eyes (partials) | **~0.4–0.7 s** | caption deltas arrive before the utterance ends |
| Late joiner catch-up | <1 s | last 100 captions hydrated from PG in one query |

The browser-WebRTC path (creator mic direct to OpenAI) removes the SFU + egress
hops: ~100–150 ms of transport saved, at the cost of running on the creator's
machine and requiring their own OpenAI key.

### How to reduce latency further (roadmap, in impact order)

1. **Faster translation model.** The 300–800 ms model hop dominates the budget.
   Groq's Realtime API, when it ships translation, would cut it to ~100–200 ms
   (their inference is 5–10× faster); self-hosting a streaming seq2seq
   translation model (e.g. Whisper→M2M streaming stack) trades GPU ops for
   full control. This is the single biggest lever.
2. **In-process translated-audio mixing.** Today translated PCM rides Redis →
   ws-gateway → WebAudio. Publishing translated tracks back into the LiveKit
   room (server SDK) would let listeners use the native LiveKit jitter buffer
   and save one WS hop (~5–15 ms) while gaining SFU-grade transport (TURN,
   simulcast-style adaptation).
3. **Chunked transcription overlap** for on-demand episodes: start whisper on
   the first N seconds while the rest is still uploading/processing (pipeline
   the transcribe step with the ffmpeg step per-segment) — cuts time-to-
   transcript from ~3 s to ~1.5 s for short episodes, much more for long ones.
4. **Persistent worker warmth**: keep one pre-forked ffmpeg + one warm OpenAI
   connection pool per worker to shave cold-start (~100–200 ms per job).
5. **Edge/CDN for processed audio** (already supported via `S3_CDN_BASE_URL`):
   playback start latency drops from tens/hundreds of ms (S3 presign + fetch)
   to edge-cache hit (~10–30 ms). Use segmented HLS for long episodes so
   time-to-first-audio is independent of file size.
6. **Co-locate regions**: the compose stack measures ~0 network RTT; in prod,
   keeping the worker (translation + STT) in the same region as LiveKit and the
   model provider's edge is worth more than any code change — every 100 km is
   ~1 ms each way, and the realtime path crosses the network 6+ times.
7. **Not worth it**: replacing Redis pub/sub with in-process channels (the hop
   is <1 ms), binary JSON on the caption path (payloads are ~100 bytes), or
   WebSocket compression on PCM (already raw, CPU cost > wire savings).

## 9. Engineering principles

SOLID, domain-driven modules, type-safe contracts (OpenAPI → generated TS),
idempotent workers with retries + DLQ, pagination everywhere, indexed queries
and explicit transaction boundaries, graceful degradation over hard failures,
structured JSON logs, health/readiness for every binary. Never: giant
services, business logic in controllers or React components, synchronous
large-file processing, per-event DB writes, unbounded responses — and in the
realtime paths, **never let latency grow: drop frames, not deadlines.**
