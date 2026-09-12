#!/usr/bin/env python3
"""E2E section A: auth -> live room -> presence -> translation control plane.
Prints per-call latency (ms) and pass/fail per step."""
import json, time, urllib.request, urllib.error, uuid

BASE = "http://localhost:18080/api/v1"
TAG = uuid.uuid4().hex[:6]

def call(method, path, body=None, token=None, timeout=10):
    url = BASE + path if path.startswith("/") else path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            payload = json.loads(r.read().decode() or "{}")
            ms = (time.perf_counter() - t0) * 1000
            return r.status, payload, ms
    except urllib.error.HTTPError as e:
        payload = {}
        try: payload = json.loads(e.read().decode() or "{}")
        except Exception: pass
        return e.code, payload, (time.perf_counter() - t0) * 1000

results = []
def step(name, ok, ms, extra=""):
    results.append((name, ok, ms))
    print(f"{'PASS' if ok else 'FAIL'} {name:55s} {ms:8.1f}ms  {extra}")

# 1. Register creator
s, body, ms = call("POST", "/auth/register", {
    "email": f"creator_{TAG}@test.local", "password": "Testpass123!", "username": f"creator_{TAG}",
    "display_name": "E2E Creator", "as_creator": True})
ctok = body.get("tokens", {}).get("access_token", "")
step("register creator (as_creator)", s == 201, ms, f"http {s}")
assert ctok, f"no creator token: {body}"

# 2. Register listener
s, body, ms = call("POST", "/auth/register", {
    "email": f"listener_{TAG}@test.local", "password": "Testpass123!", "username": f"listener_{TAG}",
    "display_name": "E2E Listener"})
ltok = body.get("tokens", {}).get("access_token", "")
lid = body.get("user_id", "")
step("register listener", s == 201, ms, f"http {s}")
assert ltok, f"no listener token: {body}"

# 3. Create live session
s, body, ms = call("POST", "/live", {"title": f"E2E Show {TAG}", "description": "latency probe",
    "category": "tech", "visibility": "public"}, ctok)
sess = body.get("id", "")
step("POST /live (create session)", s == 201, ms, f"http {s}")
assert sess, body

# 4. Start stream
s, body, ms = call("POST", f"/live/{sess}/start", None, ctok)
step("POST /live/{id}/start", s in (200, 201), ms, f"http {s}")

# 5. Listener joins
s, body, ms = call("POST", f"/live/{sess}/join", None, ltok)
step("POST /live/{id}/join (listener)", s in (200, 201), ms, f"http {s}")

# 6. Heartbeat x3
lat = []
for _ in range(3):
    s, body, ms = call("POST", f"/live/{sess}/heartbeat", None, ltok)
    lat.append(ms)
step("POST /live/{id}/heartbeat x3", s in (200, 201), sum(lat)/3, f"avg of 3, last http {s}")

# 7. Session stats (presence visible?)
s, body, ms = call("GET", f"/live/{sess}")
concurrent = body.get("concurrent_listeners", body.get("concurrent", "?"))
step("GET /live/{id} (stats)", s == 200, ms, f"listeners={concurrent}")

# 8. Translation: configure (creator). No OpenAI key pointing at OpenAI — observe behavior.
s, body, ms = call("PUT", f"/live/{sess}/translate/config",
    {"enabled": True, "source_lang": "en", "target_langs": ["hi", "es"]}, ctok)
step("PUT /live/{id}/translate/config", s in (200, 201), ms, f"http {s} body={json.dumps(body)[:80]}")

# 9. Translation config readback
s, body, ms = call("GET", f"/live/{sess}/translate/config", None, ctok)
step("GET /live/{id}/translate/config", s == 200, ms, f"http {s} body={json.dumps(body)[:80]}")

# 10. Caption ingest (creator relays a delta then a final) — measures fan-out path
t0 = time.perf_counter()
s1, _, ms1 = call("POST", f"/live/{sess}/translate/captions",
    {"lang": "hi", "text": "नमस्ते दुनिया", "final": False, "start_ms": 0}, ctok)
s2, _, ms2 = call("POST", f"/live/{sess}/translate/captions",
    {"lang": "hi", "text": "नमस्ते दुनिया, यह एक परीक्षण है", "final": True, "start_ms": 0}, ctok)
step("POST /live/{id}/translate/captions (delta+final)", s1 in (200, 201) and s2 in (200, 201),
     ms1 + ms2, f"http {s1},{s2}")

# 11. Caption readback by listener (late-joiner path; persistence is async)
# -> poll briefly until the final line is visible
got, ms, ok = "", 0.0, False
for _ in range(20):  # up to ~2s
    s, body, ms = call("GET", f"/live/{sess}/translate/captions?lang=hi", None, ltok)
    got = json.dumps(body, ensure_ascii=False)
    ok = s == 200 and "परीक्षण" in got
    if ok:
        break
    time.sleep(0.1)
step("GET /live/{id}/translate/captions (listener)", ok, ms,
     f"http {s}, final visible: {'परीक्षण' in got}, n={body.get('data') and len(body['data'])}")

# 12. Browser translation session mint (upstream is Groq => expect graceful failure)
s, body, ms = call("POST", f"/live/{sess}/translate/session", {"lang": "hi"}, ctok)
step("POST /live/{id}/translate/session (no OpenAI -> graceful)", s in (502, 503, 400), ms,
     f"http {s} (graceful degradation OK)")

# 13. Languages endpoint
s, body, ms = call("GET", f"/live/{sess}/translate/languages", None, ltok)
step("GET /live/{id}/translate/languages", s == 200, ms, f"http {s}")

print("\nSummary:")
ok = sum(1 for _, o, _ in results if o)
print(f"{ok}/{len(results)} steps passed")
with open("/tmp/e2e_a.json", "w") as f:
    json.dump([{"step": n, "ok": o, "ms": round(m, 1)} for n, o, m in results] + [{"session": sess}], f)
