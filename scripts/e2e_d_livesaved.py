#!/usr/bin/env python3
"""E2E section D: live -> saved. Start show, publish synthetic audio into the
room (livekit-cli load-test — room-composite egress only starts rendering once
tracks exist), end it, watch the worker finalize the recording into a draft
episode, then publish it."""
import json, subprocess, time, urllib.request, urllib.error, uuid

BASE = "http://localhost:18080/api/v1"
TAG = uuid.uuid4().hex[:6]

def call(method, path, body=None, token=None, timeout=15):
    data = json.dumps(body).encode() if isinstance(body, (dict, list)) else body
    req = urllib.request.Request(BASE + path, data=data, method=method)
    if isinstance(body, (dict, list)):
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read().decode()
            try: payload = json.loads(raw or "{}")
            except Exception: payload = raw
            return r.status, payload, (time.perf_counter() - t0) * 1000
    except urllib.error.HTTPError as e:
        try: payload = json.loads(e.read().decode() or "{}")
        except Exception: payload = {}
        return e.code, payload, (time.perf_counter() - t0) * 1000

results = []
def step(name, ok, ms, extra=""):
    results.append((name, ok, ms))
    print(f"{'PASS' if ok else 'FAIL'} {name:58s} {ms:8.1f}ms  {extra}")

def poll(fn, deadline_s=150, interval=1.0):
    t0 = time.perf_counter()
    last = None
    while time.perf_counter() - t0 < deadline_s:
        ok, last = fn()
        if ok:
            return True, (time.perf_counter() - t0) * 1000, last
        time.sleep(interval)
    return False, (time.perf_counter() - t0) * 1000, last

# creator
s, body, ms = call("POST", "/auth/register", {"email": f"lv_{TAG}@t.local", "password": "Testpass123!",
    "username": f"lv_{TAG}", "display_name": "Live Creator", "as_creator": True})
ctok = body["tokens"]["access_token"]

# live session
s, body, ms = call("POST", "/live", {"title": f"LiveSaved {TAG}", "category": "talk",
    "visibility": "public"}, ctok)
sid = body["id"]
s, body, ms = call("POST", f"/live/{sid}/start", None, ctok)
step("POST /live/{id}/start (auto egress)", s == 200, ms, f"http {s}")

# Publish a synthetic audio track so the egress Chrome has something to render.
# (Room-composite egress waits for the room's first track before sending its
# start signal; an empty room aborts with "Start signal not received".)
room = f"live-{sid}"
loadtest = subprocess.Popen([
    "docker", "run", "--rm", "--network", "goonj_default",
    "livekit/livekit-cli", "load-test",
    "--url", "ws://livekit:7880", "--api-key", "devkey", "--api-secret", "devsecret",
    "--room", room, "--audio-publishers", "1", "--publishers", "0",
    "--duration", "45s", "--identity-prefix", f"e2e-{TAG}",
], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
print(f"       load-test publisher started (room={room})")
time.sleep(8)  # let the track reach the room and the egress start signal fire

# recording status while live
s, body, ms = call("GET", f"/live/{sid}/recording", None, ctok)
rec0 = json.dumps(body)[:120]
step("GET /live/{id}/recording (while live)", s == 200, ms, rec0)

# let it record a bit, then end
time.sleep(8)
s, body, ms = call("POST", f"/live/{sid}/end", None, ctok)
step("POST /live/{id}/end", s == 200, ms, f"http {s}")
loadtest.wait(timeout=60)

# poll /recording until COMPLETED/CONVERTED with a draft episode
def recording_done():
    st, b, _ = call("GET", f"/live/{sid}/recording", None, ctok)
    blob = json.dumps(b)
    return st == 200 and ("COMPLETED" in blob or "CONVERTED" in blob), b
ok, rec_ms, b = poll(recording_done, 150)
rec = b if isinstance(b, dict) else {}
audio_id = rec.get("audio_id") or (rec.get("recording", {}) or {}).get("audio_id", "")
step("recording finalized -> draft (poll)", ok, rec_ms, f"status={rec.get('status')} audio={audio_id[:8] or '?'} in {rec_ms/1000:.1f}s")

# if no audio_id surfaced, look up via /audio/mine
if not audio_id:
    st, b, _ = call("GET", "/audio/mine", None, ctok)
    for x in (b.get("data", []) if isinstance(b, dict) else []):
        if x.get("source") == "live" and x.get("status") in ("DRAFT", "PROCESSING", "UPLOADING", "READY"):
            audio_id = x["id"]
            break
    step("found draft via /audio/mine", bool(audio_id), 0, f"id={audio_id[:8] if audio_id else 'none'}")

# poll draft until READY (processed pipeline), then publish via PATCH
def draft_ready():
    st, b, _ = call("GET", f"/audio/{audio_id}", None, ctok)
    return st == 200 and b.get("status") == "READY", b
ok, dr_ms, b = poll(draft_ready, 180)
step("draft processed -> READY (poll)", ok, dr_ms, f"in {dr_ms/1000:.1f}s")

s, body, ms = call("PATCH", f"/audio/{audio_id}", {"visibility": "public"}, ctok)
pub_ok = s == 200 and (body.get("visibility") == "public" or body.get("published_at"))
step("PATCH /audio/{id} publish (public)", pub_ok, ms, f"http {s}")

# it should now appear in the feed
st, b, _ = call("GET", "/audio?limit=50")
feed_hit = any(x.get("id") == audio_id for x in (b.get("data", []) if isinstance(b, dict) else []))
step("published episode visible in /audio feed", st == 200 and feed_hit, 0, f"http {st}")

okc = sum(1 for _, o, _ in results if o)
print(f"\nSummary: {okc}/{len(results)} steps passed")
json.dump([{"step": n, "ok": o, "ms": round(m, 1)} for n, o, m in results] + [{"session": sid, "audio": audio_id}],
          open("/tmp/e2e_d.json", "w"))
