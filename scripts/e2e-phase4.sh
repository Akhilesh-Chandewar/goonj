#!/usr/bin/env bash
# Goonj Phase 4 end-to-end verification against the live compose stack.
# Covers: upload pipeline (presigned PUT -> FFmpeg worker -> playback),
# engagement (likes/comments), follows, playlists, history, search, stats.
# Usage: bash scripts/e2e-phase4.sh
set -u
API="${API:-http://localhost:18080/api/v1}"
WEB="${WEB:-http://localhost:13000}"
S3="${S3:-http://localhost:14567}"
PG="docker exec goonj-postgres-1 psql -U goonj -t -A -q -c"
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  PASS: $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }
step() { echo; echo "== $1"; }
jq_get() { python3 -c "import sys,json;d=json.load(sys.stdin);print(eval(\"d$1\"))"; }

step "0. Infrastructure health"
curl -sf "$API/health/services" >/dev/null && ok "api healthy (postgres+redis ok)" || bad "api unhealthy"
curl -sf "$API/ping" | grep -q goonj-api && ok "ping ok" || bad "ping failed"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$WEB/")
[ "$CODE" = "200" ] && ok "web serves /" || bad "web / -> $CODE"
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1
aws --endpoint-url "$S3" s3 ls >/dev/null 2>&1 && ok "s3 reachable" || bad "s3 unreachable"
# Precondition: the *running* DB actually has the Phase 4 schema (PROBLEMS #20:
# a stale volume or half-applied state fails every engagement call below).
SCHEMA=$($PG "SELECT count(*) FROM goose_db_version WHERE version_id = 5 AND is_applied")
[ "$SCHEMA" = "1" ] && ok "schema at version 5 (phase 4 tables)" || bad "DB missing migration 5 — run: cd apps/api && MIGRATIONS_DIR=migrations DATABASE_URL=postgres://goonj:goonj@localhost:15433/goonj?sslmode=disable go run ./cmd/seed"

step "1. Creator + listener accounts"
TS=$(date +%s)
CE="creator$TS@e2e.dev"; LE="listener$TS@e2e.dev"
CTOK=$(curl -s -X POST "$API/auth/register" -H "Content-Type: application/json" \
  -d "{\"email\":\"$CE\",\"password\":\"password123\",\"username\":\"creator$TS\",\"as_creator\":true}" | jq_get "['tokens']['access_token']")
LTOK=$(curl -s -X POST "$API/auth/register" -H "Content-Type: application/json" \
  -d "{\"email\":\"$LE\",\"password\":\"password123\",\"username\":\"listener$TS\"}" | jq_get "['tokens']['access_token']")
[ -n "$CTOK" ] && [ -n "$LTOK" ] && ok "creator + listener registered" || { bad "registration failed"; exit 1; }
CH="Authorization: Bearer $CTOK"
LH="Authorization: Bearer $LTOK"

step "2. Upload pipeline: presigned PUT -> worker -> READY"
INIT=$(curl -s -X POST "$API/audio/uploads" -H "Content-Type: application/json" -H "$CH" \
  -d '{"title":"E2E Tone Episode","description":"generated sine wave","category":"electronic","mime_type":"audio/mpeg"}')
AID=$(echo "$INIT" | jq_get "['audio']['id']")
UURL=$(echo "$INIT" | jq_get "['upload_url']")
[ -n "$AID" ] && ok "upload session created ($AID)" || { bad "upload init failed: $INIT"; exit 1; }
curl -sf -X PUT -H "Content-Type: audio/mpeg" --upload-file /tmp/test-tone.mp3 "$UURL" \
  && ok "presigned PUT accepted" || bad "presigned PUT failed"
RES=$(curl -s -X POST "$API/audio/uploads/$AID/complete" -H "$CH")
echo "$RES" | grep -q PROCESSING && ok "queued for processing (PROCESSING)" || bad "complete -> $(echo $RES | head -c 120)"
READY=""
for i in $(seq 1 30); do
  READY=$(curl -s "$API/audio/$AID" | jq_get "['status']" 2>/dev/null)
  [ "$READY" = "READY" ] && break
  sleep 2
done
[ "$READY" = "READY" ] && ok "worker processed audio to READY" || bad "status=$READY after 60s (worker logs: docker compose logs worker)"

step "3. Auto-publish + playback"
# Upload-sourced episodes are public as soon as the worker marks them READY;
# the draft->publish flow (PATCH visibility=public) applies to live recordings.
VIS=$(curl -s "$API/audio/$AID" | jq_get "['visibility']")
[ "$VIS" = "public" ] && ok "episode public after READY (auto-publish)" || bad "visibility=$VIS"
curl -s -X PATCH "$API/audio/$AID" -H "Content-Type: application/json" -H "$CH" \
  -d '{"title":"E2E Tone Episode (remastered)"}' | grep -q 'remastered' && ok "metadata edit works" || bad "metadata edit failed"
PB=$(curl -s "$API/audio/$AID/playback")
DUR=$(echo "$PB" | jq_get "['audio']['duration_ms']")
SRC=$(echo "$PB" | jq_get "['sources'][0]['url']")
[ "${DUR:-0}" -ge 5000 ] && [ "${DUR:-0}" -le 8000 ] && ok "duration probed: ${DUR}ms (expected ~6000)" || bad "duration=${DUR:-missing}"
HREF=$(echo "$SRC" | sed 's/^"//;s/"$//')
curl -sf -o /dev/null "$HREF" && ok "presigned playback URL serves bytes" || bad "playback URL failed"

step "4. Engagement: likes + comment thread"
ST1=$(curl -s -X POST "$API/audio/$AID/like" -H "$LH")
echo "$ST1" | grep -q '"liked":true' && ok "listener liked (count=$(echo $ST1 | jq_get "['likes']"))" || bad "like -> $ST1"
curl -s -X POST "$API/audio/$AID/like" -H "$CH" >/dev/null
ST2=$(curl -s "$API/audio/$AID/like")
echo "$ST2" | grep -q '"likes":2' && ok "like count reached 2" || bad "state -> $ST2"
CU=$(curl -s -X POST "$API/audio/$AID/comments" -H "Content-Type: application/json" -H "$LH" -d '{"body":"loving this tone"}')
CID=$(echo "$CU" | jq_get "['id']")
[ -n "$CID" ] && ok "comment posted" || bad "comment -> $CU"
curl -s -X POST "$API/audio/$AID/comments" -H "Content-Type: application/json" -H "$CH" \
  -d "{\"body\":\"thanks for listening\",\"parent_id\":\"$CID\"}" | grep -q '"body"' && ok "reply posted" || bad "reply failed"
curl -s -X POST "$API/audio/comments/$CID/like" -H "$CH" | grep -q liked && ok "comment liked" || bad "comment like failed"
TH=$(curl -s "$API/audio/$AID/comments" -H "$LH")
echo "$TH" | grep -q '"creator'"$TS"'"' && ok "thread shows usernames" || bad "thread usernames missing"
echo "$TH" | grep -q '"mine":true' && ok "thread flags viewer's own comments" || bad "mine flags missing"

step "5. Follow graph + subscriptions feed"
SUBS=$(curl -s -X POST "$API/creators/$AID/follow" -H "$LH" 2>/dev/null) # wrong id on purpose -> expect 404
curl -s -X POST "$API/creators/00000000-0000-0000-0000-000000000000/follow" -H "$LH" | grep -q "not found" && ok "follow rejects unknown creator (404)" || bad "unknown creator follow not rejected"
CRID=$($PG "SELECT id FROM creators ORDER BY created_at DESC LIMIT 1")
FW=$(curl -s -X POST "$API/creators/$CRID/follow" -H "$LH")
echo "$FW" | grep -q '"subscribers":1' && ok "follow -> subscriber_count=1" || bad "follow -> $FW"
UF=$(curl -s -X DELETE "$API/creators/$CRID/follow" -H "$LH")
echo "$UF" | grep -q '"subscribers":0' && ok "unfollow -> subscriber_count=0" || bad "unfollow -> $UF"
curl -s -X POST "$API/creators/$CRID/follow" -H "$LH" >/dev/null
curl -s "$API/subscriptions/feed" -H "$LH" | grep -q "$AID" && ok "subscriptions feed contains the episode" || bad "feed missing episode"
curl -s "$API/creators/followed" -H "$LH" | grep -q "creator$TS" && ok "followed list shows channel" || bad "followed list empty"

step "6. Playlists"
PL=$(curl -s -X POST "$API/playlists" -H "Content-Type: application/json" -H "$LH" -d '{"title":"E2E mix","visibility":"private"}')
PLID=$(echo "$PL" | jq_get "['id']")
[ -n "$PLID" ] && ok "playlist created" || bad "playlist -> $PL"
curl -s -X POST "$API/playlists/$PLID/items" -H "Content-Type: application/json" -H "$LH" -d "{\"audio_id\":\"$AID\"}" | grep -q '"added":true' && ok "item added" || bad "item add failed"
N=$(curl -s "$API/playlists/$PLID" -H "$LH" | jq_get "['item_count']")
[ "$N" = "1" ] && ok "item_count=1" || bad "item_count=$N"
curl -s -X DELETE "$API/playlists/$PLID/items/$AID" -H "$LH" | grep -q '"removed":true' && ok "item removed" || bad "item remove failed"
curl -s -X DELETE "$API/playlists/$PLID" -H "$LH" | grep -q deleted && ok "playlist deleted" || bad "playlist delete failed"

step "7. History: progress -> resume -> list"
curl -s -X POST "$API/history/$AID/progress" -H "Content-Type: application/json" -H "$LH" \
  -d '{"position_ms":3000,"duration_ms":6000}' | grep -q recorded && ok "progress recorded" || bad "progress failed"
R=$(curl -s "$API/history/$AID/resume" -H "$LH")
echo "$R" | grep -q '"position_ms":3000' && ok "resume returns saved position" || bad "resume -> $R"
HL=$(curl -s "$API/history/" -H "$LH")
echo "$HL" | grep -q '"completed":false' && ok "history shows in-progress entry" || bad "history missing entry"
curl -s -X POST "$API/history/$AID/progress" -H "Content-Type: application/json" -H "$LH" \
  -d '{"position_ms":5900,"duration_ms":6000}' >/dev/null
curl -s "$API/history/" -H "$LH" | grep -q '"completed":true' && ok ">=95% marks completed" || bad "completion not detected"

step "8. Search + discovery"
SR=$(curl -s "$API/search?q=Tone+Episode")
echo "$SR" | grep -q "$AID" && ok "audio search finds episode" || bad "audio search missed"
curl -s "$API/search?q=creator$TS" | grep -q '"handle":"creator'"$TS"'"' && ok "creator search finds channel" || bad "creator search missed"
curl -s "$API/search/suggestions?q=Tone" | grep -q "$AID" && ok "suggestions endpoint works" || bad "suggestions failed"
curl -s "$API/audio" | grep -q "$AID" && ok "public feed lists episode" || bad "public feed missing episode"

step "9. Studio stats (creator analytics)"
SS=$(curl -s "$API/audio/studio/stats" -H "$CH")
echo "$SS" | grep -q '"episodes":1' && ok "episodes=1" || bad "episodes: $(echo $SS | head -c 80)"
echo "$SS" | grep -q '"total_likes":2' && ok "total_likes=2" || bad "total_likes: $(echo $SS | grep -o '"total_likes":[0-9]*')"
echo "$SS" | grep -q '"top_audio"' && ok "top_audio present" || bad "top_audio missing"

step "10. Access control negatives"
curl -s -X POST "$API/playlists" -H "Content-Type: application/json" -d '{"title":"anon"}' | grep -qi auth && ok "anon playlist create rejected" || bad "anon create not rejected"
OWN=$(curl -s -X POST "$API/playlists" -H "Content-Type: application/json" -H "$CH" -d '{"title":"creator pl"}' | jq_get "['id']")
curl -s -X DELETE "$API/playlists/$OWN" -H "$LH" | grep -q "not found" && ok "cross-user playlist delete rejected" || bad "cross-user delete allowed"

echo
echo "======================================"
echo "  RESULT: $PASS passed, $FAIL failed"
echo "======================================"
exit $([ "$FAIL" = "0" ] && echo 0 || echo 1)
