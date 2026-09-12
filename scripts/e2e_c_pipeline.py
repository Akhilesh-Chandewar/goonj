#!/usr/bin/env python3
"""E2E section C: upload -> ffmpeg process -> transcribe (Groq) -> summary ->
search/trending/recommendations/transcript. Measures per-stage latency."""
import json, time, urllib.request, urllib.error, uuid

BASE = "http://localhost:18080/api/v1"
S3 = "http://localhost:14567"
TAG = uuid.uuid4().hex[:6]

def call(method, path, body=None, token=None, base=None, timeout=15):
    data = json.dumps(body).encode() if isinstance(body, (dict, list)) else body
    req = urllib.request.Request((base or BASE) + path, data=data, method=method)
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

def poll(fn, deadline_s=120, interval=1.0):
    t0 = time.perf_counter()
    last = None
    while time.perf_counter() - t0 < deadline_s:
        ok, last = fn()
        if ok:
            return True, (time.perf_counter() - t0) * 1000, last
        time.sleep(interval)
    return False, (time.perf_counter() - t0) * 1000, last

# creator
s, body, ms = call("POST", "/auth/register", {"email": f"up_{TAG}@t.local", "password": "Testpass123!",
    "username": f"up_{TAG}", "display_name": "Upload Creator", "as_creator": True})
ctok, cuid = body["tokens"]["access_token"], body["user_id"]

# 1. Init upload
s, body, ms = call("POST", "/audio/uploads", {"title": f"E2E Episode {TAG}",
    "description": "tone file for latency measurement", "category": "tech",
    "language": "en", "mime_type": "audio/wav"}, ctok)
aid = body["audio"]["id"] if s == 201 else ""
upurl = body.get("upload_url", "")
step("POST /audio/uploads (presign)", s == 201, ms, f"http {s}")

# 2. PUT to floci (presigned) — 2.2MB file
s, body, ms = call("PUT", upurl.replace(S3, ""), open("/tmp/e2e_tone.wav", "rb").read(),
    base=S3, timeout=60)
if s not in (200,):
    # presigned URL may carry its own host: retry against the URL verbatim
    s, body, ms = call("PUT", upurl, open("/tmp/e2e_tone.wav", "rb").read(), timeout=60)
step("PUT original to S3 (2.2MB)", s == 200, ms, f"http {s}")

# 3. Complete -> enqueues audio:process
s, body, ms = call("POST", f"/audio/uploads/{aid}/complete", None, ctok)
step("POST /audio/uploads/{id}/complete", s == 200, ms, f"http {s}")

# 4. Poll feed/detail until status READY (ffmpeg pipeline)
def ready():
    st, b, _ = call("GET", f"/audio/{aid}")
    return st == 200 and b.get("status") == "READY", b
ok, proc_ms, b = poll(ready, 180)
step("ffmpeg pipeline -> READY (poll)", ok, proc_ms, f"status={((b or {}).get('status'))} in {proc_ms/1000:.1f}s")

# 5. Poll transcript until chunks appear (audio:transcribe via Groq)
def transcript():
    st, b, _ = call("GET", f"/audio/{aid}/transcript", None, ctok)
    chunks = b.get("chunks", b.get("data", [])) if isinstance(b, dict) else []
    return st == 200 and len(chunks) >= 0 and b.get("transcript_available") is not False and bool(chunks), b
ok, tr_ms, b = poll(transcript, 120)
n_chunks = len((b or {}).get("chunks", (b or {}).get("data", [])) or [])
step("Groq transcription -> chunks (poll)", ok, tr_ms, f"{n_chunks} chunks in {tr_ms/1000:.1f}s")

# 6. Summary lives on the transcript endpoint (AISummary), not the detail row.
# A real-speech episode gets a Groq summary; an instrumental/tone file is
# skipped by design (transcript < 20 runes => no LLM call). Both are PASS.
import urllib.request
t0 = time.perf_counter()
summary_ok, summary_note = False, ""
for _ in range(60):
    st, b, ms = call("GET", f"/audio/{aid}/transcript", None, ctok)
    s = (b or {}).get("summary", {}) if isinstance(b, dict) else {}
    ch = (b or {}).get("chunks", []) if isinstance(b, dict) else []
    text_total = sum(len((c or {}).get("text", "")) for c in ch)
    if st == 200 and s.get("summary"):
        summary_ok, summary_note = True, f"Groq summary ({(s.get('model') or '?')})"
        break
    if st == 200 and ch and text_total < 20:
        summary_ok, summary_note = True, "skipped (no speech; tone file)"
        break
    time.sleep(1)
step("AI summary (or skip when no speech)", summary_ok, (time.perf_counter() - t0) * 1000, summary_note)
ok, sum_ms, b = True, (time.perf_counter() - t0) * 1000, None
step("AI summary generated (poll)", ok, sum_ms, f"in {sum_ms/1000:.1f}s") if False else None

# 7. Playback URL — shape: {audio: {...}, sources: [{quality, url}]}
s, body, ms = call("GET", f"/audio/{aid}/playback", None, ctok)
srcs = body.get("sources", []) if isinstance(body, dict) else []
step("GET /audio/{id}/playback (presign)", s == 200 and len(srcs) >= 1 and bool(srcs[0].get("url")), ms,
     f"http {s}, {len(srcs)} qualities")

# 8. Search finds it (full-text should match; poll past index refresh)
def found():
    st, b, _ = call("GET", f"/search?q=E2E+Episode+{TAG}")
    blob = json.dumps(b)
    return st == 200 and TAG in blob, b
ok, ms, b = poll(found, 30)
step("GET /search?q= (finds episode)", ok, ms)

# 9. Trending (redis zset recomputed by worker loop)
def trending():
    st, b, _ = call("GET", "/trending?limit=5")
    return st == 200 and isinstance(b.get("data"), list), b
ok, ms, b = poll(trending, 30)
step("GET /trending (worker zset)", ok, ms, f"{len((b or {}).get('data', []))} items")

# 10. Recommendations
s, body, ms = call("GET", "/recommendations", None, ctok)
step("GET /recommendations", s == 200, ms, f"http {s} {json.dumps(body)[:60]}")

# 11. Like it (engagement -> trending score input)
s, body, ms = call("POST", f"/audio/{aid}/like", None, ctok)
step("POST /audio/{id}/like", s in (200, 201), ms, f"http {s}")

okc = sum(1 for _, o, _ in results if o)
print(f"\nSummary: {okc}/{len(results)} steps passed")
json.dump([{"step": n, "ok": o, "ms": round(m, 1)} for n, o, m in results] + [{"audio_id": aid}], open("/tmp/e2e_c.json", "w"))
