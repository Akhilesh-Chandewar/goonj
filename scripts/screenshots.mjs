/**
 * Screenshot tour: captures the whole app surface into screenshots/ using
 * Playwright against the running compose stack (web :13000, api :18080).
 *
 *   bun scripts/screenshots.mjs
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

function auth(localStorageDump) {
  return async (context) => {
    await context.addInitScript((dump) => {
      for (const [k, v] of Object.entries(dump)) localStorage.setItem(k, v);
    }, { "goonj.tokens": JSON.stringify(localStorageDump) });
  };
}

const shots = [];
async function shot(page, name) {
  await page.waitForTimeout(600); // let client fetches land
  await page.screenshot({ path: `${OUT}${name}.png`, fullPage: true });
  shots.push(name);
  console.log(`  ✓ ${name}`);
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

const up = await api("POST", "/audio/uploads", {
  title: `Deep Dive: Building Realtime Audio ${TAG}`, description:
    "From WebRTC to pgvector — how Goonj keeps speech under a second.",
  category: "tech", language: "en", mime_type: "audio/mpeg", }, cTok);
const aid = up.body.audio.id;
await api("POST", `/audio/uploads/${aid}/complete`, null, cTok);

console.log("Capturing pages…");
const browser = await chromium.launch({
  channel: "chrome", // system Chrome; falls back to bundled if omitted
  args: ["--no-sandbox"],
});
try {
  // ---------- public pages ----------
  let page = await browser.newPage();
  await page.goto(WEB, { waitUntil: "networkidle" });
  await shot(page, "01-home");
  await page.goto(`${WEB}/live`, { waitUntil: "networkidle" });
  await shot(page, "02-live-directory");
  await page.goto(`${WEB}/discover`, { waitUntil: "networkidle" });
  await shot(page, "03-discover");
  await page.goto(`${WEB}/search?q=Deep%20Dive`, { waitUntil: "networkidle" });
  await shot(page, "04-search");
  await page.goto(`${WEB}/login`, { waitUntil: "networkidle" });
  await shot(page, "05-login");
  await page.close();

  // ---------- creator (authed) ----------
  page = await browser.newPage({ storageState: undefined, viewport: { width: 1440, height: 900 } });
  await auth({ access_token: cTok, refresh_token: cTok })(page.context());
  await page.goto(`${WEB}/studio/live`, { waitUntil: "networkidle" });
  await shot(page, "06-studio-live");
  await page.goto(`${WEB}/studio/upload`, { waitUntil: "networkidle" });
  await shot(page, "07-studio-upload");
  await page.goto(`${WEB}/studio/content`, { waitUntil: "networkidle" });
  await shot(page, "08-studio-content");
  await page.close();

  // ---------- listener (authed) ----------
  page = await browser.newPage();
  await auth({ access_token: lTok, refresh_token: lTok })(page.context());
  await page.goto(`${WEB}/live/${sid}`, { waitUntil: "networkidle" });
  await shot(page, "09-live-room");
  await page.goto(`${WEB}/audio/${aid}`, { waitUntil: "networkidle" });
  await shot(page, "10-audio-page");
  await page.goto(`${WEB}/library`, { waitUntil: "networkidle" });
  await shot(page, "11-library");
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
  await auth({ access_token: relogin.body.tokens.access_token, refresh_token: relogin.body.tokens.access_token })(page.context());
  await page.goto(`${WEB}/studio/reports`, { waitUntil: "networkidle" });
  await shot(page, "12-moderator-queue");
  await page.close();

  console.log(`\n${shots.length} screenshots saved to screenshots/`);
} finally {
  await browser.close();
}
