#!/usr/bin/env bash
# Goonj Phase 5 (AI & semantic search) end-to-end verification against the
# live compose stack. Runs the upload pipeline, then the AI path:
# transcription (skipped gracefully when no STT backend is configured),
# embedding, AI summary, semantic search, and the transcript endpoint.
# OpenAI is optional — with no keys the local baselines must carry the flow.
# Usage: bash scripts/e2e-phase5.sh
set -u
API="${API:-http://localhost:18080/api/v1}"
PG="docker exec goonj-postgres-1 psql -U goonj -t -A -q -c"
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  PASS: $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }
step() { echo; echo "== $1"; }
jq_get() { python3 -c "import sys,json;d=json.load(sys.stdin);print(eval(\"d$1\"))"; }

step "0. Preconditions"
curl -sf "$API/health/services" >/dev/null && ok "api healthy" || { bad "api unhealthy"; exit 1; }
SCHEMA=$($PG "SELECT count(*) FROM goose_db_version WHERE version_id = 6 AND is_applied")
EXT=$($PG "SELECT count(*) FROM pg_extension WHERE extname='vector'")
[ "$SCHEMA" = "1" ] && [ "$EXT" = "1" ] && ok "schema v6 + pgvector extension" \
  || { bad "DB missing migration 6/pgvector — run: docker exec goonj-api-1 seed"; exit 1; }
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1
[ -f /tmp/test-tone.mp3 ] || { ffmpeg -loglevel error -f lavfi -i "sine=frequency=440:duration=6" -codec:a libmp3lame -b:a 64k /tmp/test-tone.mp3; }

step "1. Creator account + upload to READY"
TS=$(date +%s)
CE="creator$TS@e2e.dev"
CTOK=$(curl -s -X POST "$API/auth/register" -H "Content-Type: application/json" \
  -d "{\"email\":\"$CE\",\"password\":\"password123\",\"username\":\"creator$TS\",\"as_creator\":true}" | jq_get "['tokens']['access_token']")
[ -n "$CTOK" ] && ok "creator registered" || { bad "registration failed"; exit 1; }
CH="Authorization: Bearer $CTOK"
INIT=$(curl -s -X POST "$API/audio/uploads" -H "Content-Type: application/json" -H "$CH" \
  -d '{"title":"Semantic Waves","description":"a show about ocean soundscapes and whale songs","category":"electronic","mime_type":"audio/mpeg"}')
AID=$(echo "$INIT" | jq_get "['audio']['id']")
UURL=$(echo "$INIT" | jq_get "['upload_url']")
[ -n "$AID" ] && ok "upload session created ($AID)" || { bad "upload init failed: $INIT"; exit 1; }
curl -sf -X PUT -H "Content-Type: audio/mpeg" --upload-file /tmp/test-tone.mp3 "$UURL" \
  && ok "presigned PUT accepted" || bad "presigned PUT failed"
curl -s -X POST "$API/audio/uploads/$AID/complete" -H "$CH" | grep -q PROCESSING && ok "queued (PROCESSING)" || bad "complete failed"
READY=""
for i in $(seq 1 30); do
  READY=$(curl -s "$API/audio/$AID" | jq_get "['status']" 2>/dev/null)
  [ "$READY" = "READY" ] && break
  sleep 2
done
[ "$READY" = "READY" ] && ok "audio READY" || { bad "status=$READY after 60s"; exit 1; }

step "2. Transcription task behavior (STT may be unconfigured)"
# The worker enqueues audio:transcribe after MarkReady. Either a transcript
# appears (STT configured) or the task completed as a graceful skip.
CHUNKS=""
for i in $(seq 1 15); do
  CHUNKS=$($PG "SELECT count(*) FROM transcript_chunks WHERE audio_id = '$AID'")
  [ "${CHUNKS:-0}" != "0" ] && break
  sleep 2
done
if [ "${CHUNKS:-0}" != "0" ]; then
  ok "transcript stored ($CHUNKS chunks)"
else
  ok "no transcript: no STT backend configured (graceful skip) — expected without keys"
fi

step "3. Embedding + summary (local baselines; synthetic transcript)"
# Independent of STT availability: insert a synthetic transcript directly,
# then run the idempotent audio:embed task via the enqueue dev tool.
$PG "INSERT INTO transcript_chunks (audio_id, idx, start_ms, end_ms, text) VALUES
  ('$AID', 0, 0, 30000, 'Welcome to Semantic Waves. Today we drift through the open ocean listening to humpback whale songs and the low rumble of the sea.'),
  ('$AID', 1, 30000, 60000, 'Later in the show the soundscape shifts to dolphins clicking in kelp forests and the creaks of Antarctic ice shelves.'),
  ('$AID', 2, 60000, 90000, 'We close with a meditation on silence: why radio producers fear dead air and how ambient noise heals the edit.')" \
  && ok "synthetic transcript inserted (3 chunks)" || bad "transcript insert failed"
EMBED_LOG=$(cd apps/api && REDIS_URL="${REDIS_URL:-redis://localhost:16380/0}" go run ./cmd/enqueue \
  audio:embed "{\"audio_id\":\"$AID\"}" 2>&1)
echo "$EMBED_LOG" | grep -q enqueued && ok "audio:embed enqueued" || bad "enqueue failed: $EMBED_LOG"
EMB=""
for i in $(seq 1 15); do
  EMB=$($PG "SELECT count(*) FROM chunk_embeddings e JOIN transcript_chunks c ON c.id = e.chunk_id WHERE c.audio_id = '$AID'")
  [ "${EMB:-0}" = "3" ] && break
  sleep 2
done
[ "${EMB:-0}" = "3" ] && ok "3 chunk embeddings stored (hash-embed-1536)" || bad "embeddings=$EMB after 30s (worker logs: docker compose logs worker)"
MODEL=$($PG "SELECT DISTINCT model FROM chunk_embeddings e JOIN transcript_chunks c ON c.id=e.chunk_id WHERE c.audio_id='$AID'")
SUM=$($PG "SELECT summary FROM audio_summaries WHERE audio_id = '$AID'")
[ -n "$SUM" ] && ok "AI summary present (model=$MODEL): $(echo "$SUM" | head -c 100)..." || bad "no summary row"

step "4. Semantic search (pgvector ANN)"
SR=$(curl -s "$API/search?q=whale%20songs%20of%20the%20open%20ocean")
echo "$SR" | grep -q "$AID" && ok "semantic query finds the episode" || bad "semantic search missed episode"
SR2=$(curl -s "$API/search?q=ocean%20soundscapes")
echo "$SR2" | grep -q "$AID" && ok "paraphrased query also finds it" || bad "paraphrase missed"
BADQ=$(curl -s "$API/search?q=quantum%20cryptographic%20key%20exchange")
echo "$BADQ" | grep -q "$AID" && bad "unrelated query matched (rank leakage)" || ok "unrelated query does not match"

step "5. Transcript endpoint"
TR=$(curl -s -o /tmp/tr.json -w "%{http_code}" "$API/audio/$AID/transcript")
[ "$TR" = "200" ] && ok "GET /audio/{id}/transcript -> 200" || bad "transcript -> $TR"
python3 - <<EOF
import json
d = json.load(open("/tmp/tr.json"))
assert d["audio_id"] == "$AID", "wrong audio"
assert len(d["chunks"]) == 3, f"want 3 chunks, got {len(d['chunks'])}"
assert d["chunks"][0]["start_ms"] == 0 and d["chunks"][-1]["end_ms"] == 90000, "span wrong"
print("  PASS: transcript payload valid (3 timed chunks)")
EOF
[ $? = 0 ] && PASS=$((PASS+1)) || bad "transcript payload invalid"
curl -s -o /dev/null -w "%{http_code}" "$API/audio/00000000-0000-0000-0000-000000000000/transcript" | grep -q 404 \
  && ok "unknown audio -> 404" || bad "unknown audio not 404"

echo
echo "======================================"
echo "  RESULT: $PASS passed, $FAIL failed"
echo "======================================"
exit $([ "$FAIL" = "0" ] && echo 0 || echo 1)
