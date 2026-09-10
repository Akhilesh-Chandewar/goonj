# Problems Faced — Engineering Log

Running record of real problems hit while building Goonj, what the symptoms
were, the root cause, and how each was fixed — so the same battles are not
fought twice. Newest phase first within each section.

Status: ✅ resolved · ⚠️ open / accepted limitation

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

### 7. Silent lead-in before recording starts ⚠️ (accepted for now)
- **Symptom:** the first seconds of a stream may be missing from the recording
  because recording starts only after the creator's client connects.
- **Mitigation:** the web client requests `/recording/start` as soon as its
  room connection is live; room-composite egress no longer waits for a
  published track, which shrinks the gap. Server-side auto-start on session
  `Start` is the eventual fix.

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
- **Fix:** `Dockerfile.go` copies `apps/api/migrations` into the image
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
  token) when a verification session outlives it. Future: refresh-on-401 in
  the web client.
