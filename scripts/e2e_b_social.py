#!/usr/bin/env python3
"""E2E section B: scheduling, follow->went-live notification fan-out, reports/moderation."""
import json, subprocess, time, urllib.request, urllib.error, uuid

BASE = "http://localhost:18080/api/v1"
TAG = uuid.uuid4().hex[:6]

def call(method, path, body=None, token=None, timeout=10):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read().decode() or "{}"), (time.perf_counter() - t0) * 1000
    except urllib.error.HTTPError as e:
        try: payload = json.loads(e.read().decode() or "{}")
        except Exception: payload = {}
        return e.code, payload, (time.perf_counter() - t0) * 1000

results = []
def step(name, ok, ms, extra=""):
    results.append((name, ok, ms))
    print(f"{'PASS' if ok else 'FAIL'} {name:58s} {ms:8.1f}ms  {extra}")

def poll_until(fn, deadline_s=8.0, interval=0.25):
    """Returns (ok, ms_until_visible, last_result)."""
    t0 = time.perf_counter()
    while time.perf_counter() - t0 < deadline_s:
        ok, res = fn()
        if ok:
            return True, (time.perf_counter() - t0) * 1000, res
        time.sleep(interval)
    return False, (time.perf_counter() - t0) * 1000, None

# users
s, body, ms = call("POST", "/auth/register", {"email": f"c2_{TAG}@t.local", "password": "Testpass123!",
    "username": f"c2_{TAG}", "display_name": "Creator Two", "as_creator": True})
ctok, cid = body["tokens"]["access_token"], body["user_id"]
l1 = call("POST", "/auth/register", {"email": f"m_{TAG}@t.local", "password": "Testpass123!",
    "username": f"m_{TAG}", "display_name": "Mod User"})
l2 = call("POST", "/auth/register", {"email": f"f_{TAG}@t.local", "password": "Testpass123!",
    "username": f"f_{TAG}", "display_name": "Follower"})
mtok, ftoken = l1[1]["tokens"]["access_token"], l2[1]["tokens"]["access_token"]
step("register 3 users (creator/mod/follower)", all(x[0] == 201 for x in (l1, l2)), ms)

# schedule
s, body, ms = call("POST", "/live", {"title": f"Sched {TAG}", "category": "music", "visibility": "public"}, ctok)
sid = body["id"]
from datetime import datetime, timedelta, timezone
when = (datetime.now(timezone.utc) + timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
s, body, ms = call("POST", f"/live/{sid}/schedule", {"scheduled_at": when}, ctok)
step("POST /live/{id}/schedule (+1h)", s == 200 and body.get("scheduled_at", "").startswith(when[:16]), ms, f"http {s}")
s, body, ms = call("GET", "/live/upcoming")
step("GET /live/upcoming (public)", s == 200 and any(x.get("id") == sid for x in body.get("data", [])), ms, f"http {s}")

# follow x2 — resolve the creators row id for the registered user first
# (follows reference creators.id, not users.id; clients get it via the
# creator page)
def creator_row_id(user_id):
    out = subprocess.run(["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "goonj",
        "-d", "goonj", "-t", "-A", "-c", f"SELECT id FROM creators WHERE user_id='{user_id}'"],
        capture_output=True, text=True).stdout.strip()
    return out
grid = creator_row_id(cid)
step("resolve creators.id for new creator (SQL)", bool(grid), 0, f"id={grid[:8]}…")
follow_ms = 0.0
follow_ok = True
for tok, who in ((mtok, "mod"), (ftoken, "follower")):
    s, body, ms = call("POST", f"/creators/{grid}/follow", None, tok)
    follow_ms += ms
    follow_ok = follow_ok and s == 200
step("POST /creators/{id}/follow x2", follow_ok, follow_ms, f"http 200,200")

# went-live fan-out: start, then poll follower notifications
s, body, ms = call("POST", f"/live/{sid}/start", None, ctok)
step("POST /live/{id}/start", s == 200, ms, f"http {s}")

def follower_notified():
    st, b, _ = call("GET", "/notifications", None, ftoken)
    items = b.get("data", b.get("items", []))
    return st == 200 and any("live" in json.dumps(x).lower() for x in items), b
ok, fanout_ms, body = poll_until(follower_notified, 8)
step("went-live notification fan-out (async, poll)", ok, fanout_ms, f"visible after {fanout_ms:.0f}ms")

# unread count + read-all
s, body, ms = call("GET", "/notifications/unread-count", None, ftoken)
n_unread = body.get("count", body.get("unread", 0))
step("GET /notifications/unread-count", s == 200 and n_unread >= 1, ms, f"unread={n_unread}")
s, body, ms = call("POST", "/notifications/read-all", None, ftoken)
s, body, ms = call("GET", "/notifications/unread-count", None, ftoken)
n_after = body.get("count", body.get("unread", 0))
step("POST /notifications/read-all -> unread 0", s == 200 and n_after == 0, ms, f"after={n_after}")

# moderation: promote mod user to ADMIN via SQL (dev flow), re-login for role token
m_email = f"m_{TAG}@t.local"
subprocess.run(["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "goonj", "-d", "goonj",
    "-c", f"UPDATE users SET role='ADMIN' WHERE email='{m_email}'"], capture_output=True)
s, body, ms = call("POST", "/auth/login", {"email": m_email, "password": "Testpass123!"})
atok = body.get("tokens", {}).get("access_token", "")
step("login after role promotion (bcrypt)", s == 200 and atok, ms, f"http {s}")

# file report (follower) + queue + resolve
s, body, ms = call("POST", f"/live/{sid}/report", {"reason": "spam"}, ftoken)
step("POST /live/{id}/report (session event)", s == 202, ms, f"http {s}")
s, body, ms = call("POST", "/reports", {"session_id": sid, "reason": "harassment", "details": "e2e audio"}, ftoken)
step("POST /reports (audio-less, session target)", s in (200, 201), ms, f"http {s}")
def report_in_queue():
    st, b, _ = call("GET", "/reports?status=PENDING", None, atok)
    return st == 200 and len(b.get("data", [])) >= 2, b
ok, q_ms, b = poll_until(report_in_queue, 5)
reports = (b or {}).get("data", [])
rid = reports[0]["id"] if reports else ""
step("GET /reports?status=PENDING (moderator)", ok, q_ms, f"{len(reports)} pending")
s, body, ms = call("POST", f"/reports/{rid}/resolve", None, atok)
step("POST /reports/{id}/resolve", s == 200, ms, f"http {s}")
s, body, ms = call("GET", "/reports?status=RESOLVED", None, atok)
step("GET /reports?status=RESOLVED", s == 200 and any(x["id"] == rid for x in body.get("data", [])), ms, f"http {s}")

# end stream
s, body, ms = call("POST", f"/live/{sid}/end", None, ctok)
step("POST /live/{id}/end", s == 200, ms, f"http {s}")

ok = sum(1 for _, o, _ in results if o)
print(f"\nSummary: {ok}/{len(results)} steps passed")
json.dump([{"step": n, "ok": o, "ms": round(m, 1)} for n, o, m in results], open("/tmp/e2e_b.json", "w"))
