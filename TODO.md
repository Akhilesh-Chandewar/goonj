# TODO — Remaining Work

Living checklist of what is left, audited against PLAN.md and the actual
codebase after Phase 5's AI slice (transcription, embeddings, semantic
search, AI summaries — shipped in `23fb056`). Update this file as items
land; move resolved battles to PROBLEMS.md, not here.

---

## Phase 5 — AI & scale (current phase)

- [ ] **Embedding-based recommendations** — `/recommendations` route does not
      exist yet. `chunk_embeddings` + the model filter + similarity floor from
      search make "more like this episode" a small step now.
- [ ] **Trending** — no trending module, no Redis ZSET, no time-decayed
      recompute job (PLAN §5 promises this).
- [ ] **CDN + adaptive streaming + distributed workers** — prod-infra items;
      Asynq supports horizontal workers but queue priorities are untuned.

## Promised by earlier phases, still missing

- [ ] **Notifications** — no `notifications` table or module anywhere; the
      "went live / recording ready" events from Phase 3 never landed.
- [ ] **Scheduling + reminders** — `SCHEDULED` audio status exists in schema
      and code but no flow uses it; session scheduling + reminders unstarted.
- [ ] **Live analytics rollups** — no `live_analytics` tables; only on-the-fly
      studio stats. PLAN wants worker-aggregated peak/concurrent/retention.
- [ ] **Moderation / reports** — no moderation module, no `reports` table;
      Phase 2's "basic moderation" only covers WS-level message deletion.
- [ ] **Recording auto-start** — PROBLEMS #7 (⚠️ open): first seconds of a
      stream are missing from recordings; fix is server-side auto-start on
      session `Start`.

## Engineering hygiene

- [ ] **OpenAPI drift** — `openapi.yaml` has zero Phase 4/5 endpoints and
      nothing is codegen'd (handlers + `api.ts` are hand-written). Either
      regenerate or drop the contract-first claim.
- [ ] **Test coverage** — 3 test files total (config, embedder, chunker).
      Store/service layers untested; E2E scripts are manual, not in CI.
- [ ] **Web client** — refresh-on-401 missing (PROBLEMS #15); transcripts and
      AI summary not surfaced in the player UI yet.

## Prod-readiness (past compose)

- [ ] TLS + TURN for LiveKit; published UDP range; config via IaC (PROBLEMS #5).
- [ ] Real S3/R2 + CDN in front of processed audio.
- [ ] Retune `SEMANTIC_MIN_SIMILARITY` for the production embedding model.
- [ ] Real `JWT_SECRET` rotation; secure cookies review.
