# Goonj (गूंज) — *Modern Day Radio*

Audio-only content platform: **modern day radio** — live shows anyone can host,
on-demand episodes anyone can tune into. **No video.** Live-first: everything
created in a live session (recording, chat, transcript) becomes permanent
content.

See [PLAN.md](./PLAN.md) for the full architecture, database design, API
contract strategy, and phased roadmap.

## Stack

| Layer | Tech |
|---|---|
| API / workers / ws-gateway | Go 1.27 (chi, pgx, go-redis, Asynq, coder/websocket) |
| Database | PostgreSQL 16 + goose migrations |
| Cache / queues / presence | Redis 7 |
| Web | Next.js 16 (App Router) + TypeScript + Tailwind, **bun** workspaces |
| Contracts | OpenAPI 3.1 → generated Go + TS clients (Phase 1) |
| Object storage (Phase 1) | S3-compatible: floci/MinIO locally, S3/R2 in prod |
| Live (Phase 2) | LiveKit SFU (audio-only) + WS chat/presence |

## Repository layout

```
goonj/
├── apps/
│   ├── api/            # Go module: cmd/{api,worker,ws-gateway,seed}
│   │   ├── internal/
│   │   │   ├── modules/     # domain modules (health, auth, …)
│   │   │   ├── platform/    # config, logger, pg, redis, http server
│   │   │   └── app/         # composition root
│   │   └── migrations/ # goose SQL
│   └── web/            # Next.js 16 (bun)
├── packages/shared/    # TS constants + API helpers
├── openapi/            # API contract
├── docker-compose.yml  # full local stack
├── Dockerfile.go       # builds api/worker/ws-gateway/seed images
└── Dockerfile.web      # Next.js production image
```

## Quick start

Prereqs: Docker, Go 1.27 (`~/.local/go/bin`), [bun](https://bun.sh) ≥ 1.4, make.

```bash
# Full stack in containers (postgres, redis, api, worker, ws, web, seed)
make up

# Or run natively for hot reload:
make web-install
make web-dev        # http://localhost:3000
cd apps/api && go run ./cmd/api   # :8080
```

Verify:

```bash
curl http://localhost:8080/api/v1/ping
curl http://localhost:8080/readyz
curl http://localhost:8080/api/v1/health/services
open http://localhost:3000
```

## Development

```bash
make go-build   # binaries → ./bin
make go-test    # Go tests
make typecheck  # TS across workspaces
make ci         # everything CI runs
```

Environment: copy `.env.example` → `.env` (compose reads it; never commit
`.env`).

### Database policy — Docker is the only Postgres

PostgreSQL runs exclusively as the compose container (`pgvector/pgvector:pg16`):

```bash
docker compose up -d postgres                                  # start
docker compose exec postgres pg_isready -U goonj -d goonj      # check
```

The host port comes from `POSTGRES_HOST_PORT` in `.env` (dev default shifted
to avoid colliding with anything local). A host-installed PostgreSQL server
is **not** used by Goonj and can be removed without affecting the stack
(pgAdmin, if installed, still connects fine to the Docker container over the
published port). Apply migrations by running the seed image, which embeds
`goose`:

```bash
docker compose run --rm seed   # migrations + seed data (idempotent)
```

### Store-layer integration tests

Unit tests never touch infrastructure. Store-layer tests live in
`apps/api/internal/integration` and run against real Postgres when
`GOONJ_TEST_DATABASE` is set (skipped otherwise, so `go test ./...` stays
green anywhere):

```bash
GOONJ_TEST_DATABASE="postgres://goonj:goonj@localhost:15433/goonj?sslmode=disable" \
  go test ./internal/integration/...   # run from apps/api
```

CI runs the same suite against a `pgvector/pgvector:pg16` service container
(`.github/workflows/ci.yml`).

### JWT secret rotation

`JWT_SECRET` signs new tokens; `JWT_SECRETS_PREVIOUS` (comma-separated,
newest previous first) is still accepted for validation so a rotation needs
no downtime: set both, deploy, then drop the previous secret once all
sessions could have refreshed. The API and ws-gateway honor both variables.

## Roadmap

Phase 0 ✅ scaffold · Phase 1 foundation (auth, upload→episode, player) ·
Phase 2 🔴 live core · Phase 3 live→saved · Phase 4 on-demand & social ·
Phase 5 AI & scale. Details in [PLAN.md](./PLAN.md).
