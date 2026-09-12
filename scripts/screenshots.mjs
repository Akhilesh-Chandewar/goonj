/**
 * Screenshot tour: captures the whole app surface into screenshots/ using
 * Playwright against the running compose stack (web :13000, api :18080).
 *
 *   bun scripts/screenshots.mjs
 *
 * Reliability: every page is asserted against expected on-page content before
 * the shot (with console/pageerror/network-failure capture), so a failed fetch
 * can never produce a silently broken screenshot — the script exits non-zero.
 *
 * Auth: registers fresh users over the API and injects `goonj.tokens` into
 * localStorage (the key lib/api.ts reads) before navigating to authed pages.
 * An ADMIN promotion (psql) is used to capture the moderator queue populated.
 */
import { chromium } from "playwright";
import { execSync } from "node:child_process";
import fs from "node:fs";

const WEB = process.env.WEB_URL ?? "http://localhost:13000";
const API = process.env.API_URL ?? "http://localhost:18080/api/v1";
const OUT = new URL("../screenshots/", import.meta.url).pathname;
fs.mkdirSync(OUT, { recursive: true });

const TAG = Math.random().toString(36).slice(2, 8);
const PASSWORD = "Testpass123!";
const fails = [];

async function api(method, path, body, token) {
  const res = await fetch(API + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  try { return { status: res.status, body: JSON.parse(text) }; }
  catch { return { status: res.status, body: text }; }
}

/** Init script injecting the token shape lib/api.ts reads. */
const injectAuth = (tokens) => async (context) => {
  await context.addInitScript((t) => {
    localStorage.setItem("goonj.tokens", JSON.stringify(t));
  }, { access_token: tokens, refresh_token: tokens, token_type: "bearer", expires_in: 900 });
};

const shots = [];
async function capture(page, name, url, mustContain, timeout = 20000) {
  const errors = [];
  const onConsole = (m) => { if (m.type() === "error") errors.push(m.text()); };
  const onPageError = (e) => errors.push(String(e));
  const onReqFailed = (r) => errors.push(`REQ FAILED: ${r.url()} ${r.failure()?.errorText ?? ""}`);
  page.on("console", onConsole); page.on("pageerror", onPageError); page.on("requestfailed", onReqFailed);

  try {
    await page.goto(url, { waitUntil: "networkidle", timeout });
    await page.waitForFunction(
      (needle) => document.body?.innerText.includes(needle), mustContain,
      { timeout: 15000 },
    );
    await page.waitForTimeout(400); // settle images/fonts
    await page.screenshot({ path: `${OUT}${name}.png`, fullPage: true });
    shots.push(name);
    console.log(`  ✓ ${name}`);
  } catch (e) {
    fails.push(name);
    console.error(`  ✗ ${name}: ${(e).message.split("\n")[0]}`);
    try { await page.screenshot({ path: `${OUT}${name}.png`, fullPage: true }); } catch {}
  } finally {
    // Report, but don't fail the shot for chatter unrelated to rendering
    // (e.g. telemetry beacons). Only surface real errors visibly.
    const real = errors.filter((x) => !x.includes("ERR_ABORTED"));
    if (real.length) console.error(`      console: ${real.slice(0, 3).join(" | ").slice(0, 300)}`);
    page.off("console", onConsole); page.off("pageerror", onPageError); page.off("requestfailed", onReqFailed);
  }
}

console.log("Setting up users…");
const creator = await api("POST", "/auth/register", {
  email: `shots_c_${TAG}@t.local`, password: PASSWORD, username: `shots_c_${TAG}`,
  display_name: "Screenshot Creator", as_creator: true,
});
const listener = await api("POST", "/auth/register", {
  email: `shots_l_${TAG}@t.local`, password: PASSWORD, username: `shots_l_${TAG}`,
  display_name: "Screenshot Listener",
});
if (creator.status !== 201 || listener.status !== 201) {
  console.error("register failed", creator.status, listener.status);
  process.exit(1);
}
const cTok = creator.body.tokens.access_token;
const lTok = listener.body.tokens.access_token;

// Content so pages aren't empty: one live session + one published episode.
const live = await api("POST", "/live", {
  title: `On Air: Latency Lab ${TAG}`, category: "tech", visibility: "public",
}, cTok);
const sid = live.body.id;
await api("POST", `/live/${sid}/schedule`, {
  scheduled_at: new Date(Date.now() + 3600_000).toISOString(),
}, cTok);

const TITLE = `Deep Dive: Building Realtime Audio ${TAG}`;
const up = await api("POST", "/audio/uploads", {
  title: TITLE, description:
    "From WebRTC to pgvector — how Goonj keeps speech under a second.",
  category: "tech", language: "en", mime_type: "audio/wav", }, cTok);
const aid = up.body.audio.id;

// Generate a short sine-tone WAV (the pipeline runs real ffmpeg on it),
// PUT it to the presigned URL, then complete + publish. Without the PUT,
// complete fails with "uploaded object not found" and the row stays UPLOADING.
function makeToneWav(seconds = 8, rate = 22050) {
  const n = seconds * rate;
  const data = Buffer.alloc(n * 2);
  for (let i = 0; i < n; i++) {
    const v = Math.round(9000 * Math.sin((2 * Math.PI * 330 * i) / rate));
    data.writeInt16LE(v, i * 2);
  }
  const h = Buffer.alloc(44);
  h.write("RIFF", 0); h.writeUInt32LE(36 + data.length, 4); h.write("WAVE", 8);
  h.write("fmt ", 12); h.writeUInt32LE(16, 16); h.writeUInt16LE(1, 20);
  h.writeUInt16LE(1, 22); h.writeUInt32LE(rate, 24); h.writeUInt32LE(rate * 2, 28);
  h.writeUInt16LE(2, 32); h.writeUInt16LE(16, 34); h.write("data", 36);
  h.writeUInt32LE(data.length, 40);
  return Buffer.concat([h, data]);
}
const put = await fetch(up.body.upload_url, {
  method: "PUT", body: makeToneWav(),
  headers: { "Content-Type": "audio/wav" },
});
if (!put.ok) { console.error("presigned PUT failed:", put.status); process.exit(1); }
await api("POST", `/audio/uploads/${aid}/complete`, null, cTok);

// Wait for the ffmpeg pipeline, then publish so search/discover show real hits.
let ready = false;
for (let i = 0; i < 60; i++) {
  const d = await api("GET", `/audio/${aid}`, null, cTok);
  if (d.status === 200 && d.body.status === "READY") { ready = true; break; }
  await new Promise((r) => setTimeout(r, 1000));
}
if (!ready) { console.error("episode never became READY"); process.exit(1); }
await api("PATCH", `/audio/${aid}`, { visibility: "public" }, cTok);

// Make the room genuinely live (join requires LIVE) with translation enabled
// and caption history so the listener room shot shows the CaptionBar.
await api("POST", `/live/${sid}/start`, null, cTok);
await api("PUT", `/live/${sid}/translate/config`, {
  enabled: true, source_lang: "en", target_langs: ["hi", "es"],
}, cTok);
await api("POST", `/live/${sid}/translate/captions`, {
  lang: "hi", text: "स्वागत है! आज हम रियल-टाइम अनुवाद के बारे में बात करेंगे।",
  final: true, start_ms: 0, end_ms: 4000,
}, cTok);
await api("POST", `/live/${sid}/translate/captions`, {
  lang: "es", text: "¡Bienvenidos! Hoy hablamos de traducción en tiempo real.",
  final: true, start_ms: 0, end_ms: 4000,
}, cTok);

console.log("Capturing pages…");
const browser = await chromium.launch({ channel: "chrome", args: ["--no-sandbox"] });
try {
  // ---------- public pages ----------
  let page = await browser.newPage();
  await capture(page, "01-home", WEB, "Goonj");
  await capture(page, "02-live-directory", `${WEB}/live`, "Latency Lab");
  await capture(page, "03-discover", `${WEB}/discover`, "Discover");
  await capture(page, "04-search", `${WEB}/search?q=Deep%20Dive`, "Deep Dive");
  await capture(page, "05-login", `${WEB}/login`, "Log");
  await page.close();

  // ---------- creator (authed) ----------
  page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await injectAuth(cTok)(page.context());
  await capture(page, "06-studio-live", `${WEB}/studio/live`, "Live");
  await capture(page, "07-studio-upload", `${WEB}/studio/upload`, "Upload");
  await capture(page, "08-studio-content", `${WEB}/studio/content`, "Deep Dive");
  await page.close();

  // ---------- listener (authed) ----------
  page = await browser.newPage();
  await injectAuth(lTok)(page.context());
  await capture(page, "09-live-room", `${WEB}/live/${sid}`, "Latency Lab");
  await capture(page, "10-audio-page", `${WEB}/audio/${aid}`, "Deep Dive");
  await capture(page, "11-library", `${WEB}/library`, "Your library");
  await page.close();

  // ---------- moderator queue (needs ADMIN role) ----------
  execSync(
    `docker compose exec -T postgres psql -U goonj -d goonj -c ` +
    `"UPDATE users SET role='ADMIN' WHERE email='shots_l_${TAG}@t.local'"`,
    { stdio: "pipe" },
  );
  const relogin = await api("POST", "/auth/login", {
    email: `shots_l_${TAG}@t.local`, password: PASSWORD,
  });
  page = await browser.newPage();
  await injectAuth(relogin.body.tokens.access_token)(page.context());
  await capture(page, "12-moderator-queue", `${WEB}/studio/reports`, "Moderation");
  await page.close();
} finally {
  await browser.close();
}

console.log(`\n${shots.length}/12 screenshots saved to screenshots/`);
if (fails.length) {
  console.error(`FAILED pages: ${fails.join(", ")}`);
  process.exit(1);
}
