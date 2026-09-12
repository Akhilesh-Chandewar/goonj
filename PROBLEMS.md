# Problems Faced — Engineering Log

Running record of real problems hit while building Goonj, what the symptoms
were, the root cause, and how each was fixed — so the same battles are not
fought twice. Newest phase first within each section.

Status: ✅ resolved · ⚠️ open / accepted limitation

---

## Phase 5 — AI & semantic search

### 21. pgx sends []float32 as float4[] — vector(1536) rejects it ✅
- **Symptom:** the `audio:embed` task entered the handler, retried with
  exponential backoff, and failed with **no application log line at all** —
  `docker logs worker` showed only `embedding audio` every ~40s.
- **Diagnosis:** `InsertEmbeddings` passed `[]float32` straight to pgx, which
  encodes it as a `float4[]` array; Postgres has no implicit cast from
  `float4[]` to `vector`, so the INSERT died on the type. The silent look was
  a red herring — asynq logs handler errors to its own logger, not slog; the
  asynq `INFO` lines about retries were the only trace. The **query** side of
  semantic search had already been given a text-literal `::vector` cast; the
  **insert** side had been missed.
- **Fix:** shared `ai.VectorLiteral(v)` renders pgvector's `[1,2,3]` text
  form; every vector parameter is now `$n::vector` (inserts in
  `cmd/worker/ai.go`, queries in `internal/modules/search/semantic.go`).
- **Lesson:** pgvector has no implicit cast from any pgx native encoding —
  route every vector through the text literal at *both* ends of the pipe,
  and treat "asynq task retries without a slog line" as `handler error
  logged by asynq`, not "nothing is wrong".

### 22. ANN search has no relevance floor — one episode matches everything ✅
- **Symptom:** `"quantum cryptographic key exchange"` "found" an episode
  about whale songs with cosine similarity 0.068 — nearest-neighbour search
  always returns *something* when the table is small.
- **Diagnosis:** HNSW orders by distance but has no concept of "too far";
  with hash embeddings the noise floor for unrelated pairs is ~0.05–0.1.
- **Fix:** `WHERE 1 - (embedding <=> $q) >= $min` with
  `DefaultMinSimilarity = 0.15`, overridable per deployment via
  `SEMANTIC_MIN_SIMILARITY` (model-dependent: retune when swapping models).
- **Lesson:** vector search needs an explicit similarity threshold tuned per
  embedding model; without one, precision degrades silently as the catalog
  grows and users just see "random" results.

### 23. Compose host-port vars defined in .env but never referenced ⚠️
- **Symptom:** rebuilding `api` after the Phase 5 changes tried to bind host
  port 8080 (already occupied by nothing visible) while the running stack
  listened on 18080.
- **Diagnosis:** `.env` shifted every port (`API_HOST_PORT=18080`, …) but
  `docker-compose.yml` only referenced some of them (`POSTGRES_HOST_PORT`,
  `REDIS_HOST_PORT`, `S3_HOST_PORT`, `WS_HOST_PORT`) — `API_HOST_PORT`,
  `WEB_HOST_PORT` and `LIVEKIT_HOST_PORT` were dead vars, so `compose up`
  after any host reset rebinds the unshifted defaults (PROBLEMS #18 again,
  this time in the compose file itself).
- **Fix:** compose now templates all three through `${VAR:-default}`; the
  in-container healthcheck still probes the unshifted internal port.
- **Lesson:** a var in `.env` does nothing until a service references it;
  audit `.env` keys against compose interpolation after any port shift.

---

### 24. chi Mount panic recurred when adding translation routes to /live ✅
- **Symptom:** API container crash-looped: `panic: attempting to Mount() a
  handler on an existing path, '/live'` — the new liveMux was mounted while
  the original `v1.Mount("/live", …)` line was left in place.
- **Fix:** removed the old mount; the shared mux (live + translation
  `RegisterRoutes`) is now the only /live registration. Same trap as #16,
  different prefix — the lesson generalizes: **any new module sharing a
  prefix must replace, never supplement, the old Mount.**

### 25. Server-side translator needed CGO — pivoted to browser WebRTC ✅
- **Symptom:** a `cmd/translator` worker joining the LiveKit room via
  `server-sdk-go/v2` + `pkg/media` (Opus→PCM decode) failed to build:
  `pkg-config: Package 'opus'/'soxr' not found` (libopus + libsoxr C deps),
  plus a SIP-related compile error in the SDK version.
- **Diagnosis:** server-side media decode drags in CGO, OS packages and a
  much larger image — and would *add* a network hop (SFU→worker→OpenAI),
  the exact thing a latency-first design avoids.
- **Decision:** the creator's browser already owns the mic; OpenAI's
  translation endpoint speaks WebRTC. The browser now connects **directly**
  to OpenAI with an ephemeral client secret (browser→OpenAI, one hop), and
  Goonj only relays caption *text* over the existing Redis→ws-gateway path.
  The server-side WS client (`infrastructure/openai`) is kept + unit-tested
  for a future media worker if per-language translated **audio** tracks are
  ever needed.
- **Lesson:** prefer the path with the fewest network hops and no C
  toolchain; keep the heavier alternative behind an interface for when
  scale actually demands it.

---

## Phase 4 — On-demand & social (engagement, search, history)

### 16. chi panics when two modules Mount the same prefix ✅
- **Symptom:** API container crash-looped at boot:
  `panic: chi: attempting to Mount() a handler on an existing path, '/audio'`.
  Engagement routes (likes/comments) were mounted at `/audio` alongside the
  audio module; `go build`/`go vet` cannot catch a runtime route collision.
- **Diagnosis:** `chi.Mount` is exclusive per path — the second `Mount`
  panics inside `app.New`, killing the process before the HTTP server starts.
- **Fix:** modules expose `RegisterRoutes(mux, mw)` next to `Router`;
  the composition root builds one `/audio` mux and registers audio +
  engagement onto it (`internal/app/app.go`). `Router` is now a thin wrapper
  over `RegisterRoutes` on a fresh mux.
- **Lesson:** route tables are runtime state. When two modules share a URL
  prefix, make the shared prefix an explicit registration point in the
  composition root, not a second `Mount`.

### 17. pgx types each placeholder per occurrence — `uuid = text` ✅
- **Symptom:** `GET /audio/{id}/comments` → 500 with
  `operator does not exist: uuid = text (SQLSTATE 42883)`, only when the
  thread endpoint was called (writes worked fine).
- **Diagnosis:** one `$2` was used both as `$2 <> ''` (forces `text`) and
  `c.user_id = $2` (uuid column). pgx infers a single type per parameter
  number across the whole statement, so the comparison became uuid = text.
- **Fix:** pass the viewer as `*string` (nil for anonymous) and cast
  explicitly: `$2::uuid IS NOT NULL AND c.user_id = $2::uuid`. The same
  landmine was removed from an unused `audio_id = ANY($2)` helper.
- **Lesson:** with pgx, every occurrence of a placeholder must agree on a
  Postgres type. Prefer explicit `::type` casts or typed nils over sentinel
  empty strings that double as text literals.

### 18. Stale docker-proxy processes squat on compose ports after a host reset ✅
- **Symptom:** every `docker compose up` failed with `ports are not
  available` while `docker ps -a` showed zero running containers; killing
  the `docker-proxy` PIDs failed with `Operation not permitted` (root-owned,
  parent pid 1).
- **Diagnosis:** a host reset orphaned the proxies; the daemon has no
  reaper for them and `compose down` cannot help because compose no longer
  knows about them.
- **Fix:** shifted host ports via `.env` (gitignored) plus a temporary
  compose override; new wrinkle — exported shell vars
  (`POSTGRES_HOST_PORT=5433`, …) **beat** `.env`, so they had to be
  overridden per invocation. All consumers (DATABASE_URL, S3_PUBLIC_ENDPOINT,
  ALLOWED_ORIGINS, NEXT_PUBLIC_API_URL) had to move in lockstep or presigned
  URLs silently point browsers at a port nothing listens on.
- **Lesson:** a port change is a *system-wide* config change, not a compose
  flag. Every consumer that bakes a URL around a port must move together.

### 19. A stray no-op edit can clobber a whole .env file ✅
- **Symptom:** after re-sorting `.env` for the port shift, the upload
  pipeline started failing with `NoSuchKey`-style 404s on `HeadObject` and
  browsers got presigned URLs against the old S3 port.
- **Diagnosis:** the rewrite dropped `S3_PUBLIC_ENDPOINT` and
  `ALLOWED_ORIGINS` lines; compose fell back to defaults pointing at the old
  ports. `docker compose up` only re-reads env for *changed* services, so
  the API container kept the stale value until `--force-recreate`.
- **Fix:** rewrote `.env` completely, then `--force-recreate`d the affected
  services; verification now asserts the *container's* env, not the file's.
- **Lesson:** treat `.env` as code: never hand-edit a subset. Assert runtime
  config inside the container (`docker exec … env`), not the file on disk.

### 20. Stale DB volume after a host reset: "table does not exist" despite green migrations ✅
- **Symptom:** every Phase 4 endpoint 500'd with `relation "follows" does
  not exist` while an earlier seed run had logged `migrated to version 5`.
- **Diagnosis:** the host was reset between the two runs; that seed went
  through a **stale proxy to a postgres instance that no longer exists**.
  The surviving named volume (`goonj_pgdata`) was pre-Phase-4.
- **Fix:** re-ran migrations against the live DB; added a precondition to
  the E2E harness that asserts the container's actual schema before testing.
- **Lesson:** after any host/docker reset, re-verify *stateful* layers
  (volumes) against the *running* services — old logs prove nothing. Use
  `docker exec <db> psql …` (in-network identity), not host port inference.

---

## Phase 3 — Live → Saved (egress recording pipeline)

### 1. Participant egress rejected by the egress build ✅
- **Symptom:** `POST /live/{id}/recording/start` → 500. API log:
  `twirp error invalid_argument: no supported codec is compatible with all
  outputs` — for both MP3 and OGG outputs.
- **Diagnosis:** reproduced directly with `lk egress start` raw JSON probes
  against the running stack: participant-egress + encoded file output is
  rejected by this egress image version, while room-composite audio-only +
  MP3 starts fine (it renders the room itself, no external template).
- **Fix:** switched the `live.Recorder` interface from
  `StartParticipantRecording(room, identity)` to
  `StartRecording(room)` backed by audio-only room-composite egress
  (`internal/infrastructure/livekit/egress.go`). Bonus: recordings no longer
  die when the creator disconnects, and no Chrome/template dependency.
- **Lesson:** when a provider API rejects a call, probe the provider directly
  (`lk egress start`, raw JSON) to isolate the failing combination instead of
  guessing from application logs.

### 2. Egress-reported storage location is a URL, not a key ✅
- **Symptom:** recording reached `CONVERTED`, but the worker's processing task
  failed with `NoSuchKey: The specified key does not exist` when downloading
  the original.
- **Diagnosis:** LiveKit's `FileResults[].location` was a full URL
  (`https://floci:4566/bucket/live/recordings/…mp3`), while
  `normalizeRecordingKey` only handled `s3://` and bare keys — the whole URL
  was being stored as the object key.
- **Fix:** rewrote the normalizer to strip scheme/host and the bucket segment
  (path-style) or just the host (virtual-hosted style), falling back to the
  canonical `live/recordings/<session>.mp3` key when ambiguous. The worker now
  receives the bucket name explicitly.
- **Lesson:** never trust that two systems mean the same thing by "location";
  normalize at the boundary and unit-test every shape the provider emits.

### 3. Ending the stream killed its own recording ✅
- **Symptom:** egress aborted with `Start signal not received` on every
  recorded session.
- **Diagnosis:** room-composite egress joins the room as a participant; the
  API's `End` handler deleted the room immediately (to disconnect everyone),
  tearing down the egress before it could finalize and upload.
- **Fix:** `End` no longer deletes the room; the worker's finalize task closes
  it (DeleteRoom) only after the file has landed in storage.
- **Lesson:** side effects with external lifecycles (SFU rooms, egress jobs)
  need an explicit owner and ordering — cleanup belongs to the step that
  finishes the work, not the step that starts it.

### 4. Chrome template page blocked the recorder (mixed content) ✅
- **Symptom:** early room-composite attempts never started; egress timed out
  waiting for a signal.
- **Diagnosis:** the egress container loads LiveKit's rendering template over
  HTTPS, which refuses `ws://livekit:7880` websockets (mixed content) — only
  `ws://localhost` is allowed.
- **Fix:** the egress service shares the livekit service's network namespace
  (`network_mode: "service:livekit"` in compose) so `ws_url: ws://localhost:7880`
  resolves inside the same pod. Documented in `apps/api/infra/livekit-egress.yaml`.
- **Lesson:** container network topology is part of the feature: co-locating
  the egress with the SFU sidesteps browser security rules that don't apply
  to normal servers.

### 5. LiveKit config files do not interpolate environment variables ⚠️
- **Symptom:** `redis: address: ${REDIS_HOST}`-style entries stayed literal;
  egress could not reach Redis.
- **Fix:** hardcoded dev values in `apps/api/infra/livekit*.yaml` with a NOTE
  that production ships its own config via IaC.
- **Lesson:** verify the templating features you assume; LiveKit YAML config
  has none.

### 6. Container-to-container ICE fails behind NAT (hairpinning) ✅
- **Symptom:** media never flowed between the egress renderer and LiveKit when
  `rtc.use_external_ip: true` (the default dev posture).
- **Diagnosis:** with external IP mode LiveKit advertises the host's public IP;
  containers on the docker network cannot hairpin back to it.
- **Fix:** `use_external_ip: false` for compose (advertise the container IP);
  production flips it back to `true` with published UDP range + TURN.

### 7. Silent lead-in before recording starts ✅
- **Symptom:** the first seconds of a stream may be missing from the recording
  because recording starts only after the creator's client connects.
- **Fix:** recording now auto-starts server-side in the live service's `Start`
  (room-composite egress needs no published track, so nothing is missed). The
  creator client's `/recording/start` call remains as an idempotent fallback
  for the window where egress is briefly unavailable.

---

## Phase 2 — Live core

### 8. Presence must not be per-event database writes ⚠️ (by design)
- **Decision:** listener counts/heartbeats live in Redis (TTL keys + counters)
  and are snapshotted to Postgres; `live_viewers` is snapshots only. The
  schema enforces it; worker aggregation rolls live analytics up later.
- **Lesson:** realtime state and durable state have different owners; mixing
  them wrecks both latency and history.

### 9. Host port collisions in compose ✅
- **Symptom:** `docker compose up` failed when local Postgres/Redis already
  occupied 5432/6379.
- **Fix:** host-side ports are configurable (`POSTGRES_HOST_PORT=5433`,
  `REDIS_HOST_PORT=6380`, `WS_HOST_PORT=8082`) with internal ports untouched.

---

## Phase 1 — Uploads & audio pipeline

### 10. floci image has no shell → no container healthcheck ⚠️
- **Symptom:** cannot write a `healthcheck:` for the S3-compatible container
  (no `wget`/`sh` in the distroless image).
- **Fix:** rely on api/worker readiness (which exercise storage paths) and a
  comment explaining why; uploads degrade gracefully when storage is down.

### 11. Presigned URLs must match the audience ✅
- **Symptom:** browser upload/playback failed with signature/endpoint errors:
  URLs signed against the internal endpoint (`http://floci:4566`) don't work
  from the host browser, and vice versa for the worker.
- **Fix:** two endpoints configured — `S3_ENDPOINT` (internal, api/worker) and
  `S3_PUBLIC_ENDPOINT` (browser-facing, presigned URLs) — plus path-style
  addressing for floci/MinIO.

### 12. Never trust client-supplied media metadata ✅
- **Decision:** the worker probes every upload with `ffprobe` (duration,
  format) before processing; impossible durations are rejected. Client MIME
  is advisory only.

---

## Phase 0 — Scaffold

### 13. goose migrations must be embedded in the shipped image ✅
- **Symptom:** release containers had no migrations; seed job couldn't run.
- **Fix:** `Go.Dockerfile` copies `apps/api/migrations` into the image
  (`MIGRATIONS_DIR=/migrations`); one image builds all four binaries and
  compose picks the entrypoint per service.

---

## Verification harness notes

### 14. Testing live audio without a browser ✅
- **Problem:** participant recording E2E needs a real publisher with a mic
  track; CI/agent environments have none.
- **Fix:** the LiveKit CLI (`lk room join --publish tone.ogg
  --exit-after-publish`) publishes a synthesized Opus file on the microphone
  source, so the full go-live → record → finalize → publish flow runs
  headless. The tone itself is generated with ffmpeg inside the worker
  container (which already ships it).
- **Lesson:** the fastest way to test media infrastructure is to script the
  exact bytes you expect at the other end.

### 15. Access tokens expire mid-verification ✅
- **Symptom:** API calls suddenly 401 during long E2E sessions.
- **Fix:** 15-minute access TTL is intentional; re-login (or use the refresh
  token) when a verification session outlives it. The web client now does
  refresh-on-401 with single-flight rotation (`apps/web/src/lib/api.ts`).
