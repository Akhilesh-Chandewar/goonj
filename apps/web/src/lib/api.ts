"use client";

/**
 * Tiny auth/token store + API client for Goonj web.
 * Access token lives in memory + localStorage (Phase 2 bridge; httpOnly
 * cookie hardening lands with Phase 1 completion). On a 401 the client
 * rotates the refresh token once and retries — PROBLEMS.md #15.
 */

import { API_ROUTES, apiUrl } from "@goonj/shared";

const TOKEN_KEY = "goonj.tokens";

export interface Tokens {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

export interface AuthUser {
  user_id?: string;
  username?: string;
  role?: string;
}

export function saveTokens(t: Tokens) {
  localStorage.setItem(TOKEN_KEY, JSON.stringify(t));
}

export function getTokens(): Tokens | null {
  const raw = localStorage.getItem(TOKEN_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as Tokens;
  } catch {
    return null;
  }
}

export function getAccessToken(): string | null {
  return getTokens()?.access_token ?? null;
}

export function clearTokens() {
  localStorage.removeItem(TOKEN_KEY);
}

/** Single-flight refresh so parallel 401s share one rotation. */
let refreshPromise: Promise<boolean> | null = null;

async function rotateRefresh(): Promise<boolean> {
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    const tokens = getTokens();
    if (!tokens?.refresh_token) return false;
    try {
      const res = await fetch(apiUrl(API_ROUTES.authRefresh), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: tokens.refresh_token }),
      });
      if (!res.ok) {
        clearTokens();
        return false;
      }
      const body = (await res.json()) as { tokens?: Tokens };
      if (!body.tokens) {
        clearTokens();
        return false;
      }
      saveTokens(body.tokens);
      return true;
    } catch {
      return false;
    } finally {
      refreshPromise = null;
    }
  })();
  return refreshPromise;
}

export async function api<T>(
  route: string,
  init?: RequestInit & { auth?: boolean }
): Promise<T> {
  const doFetch = () => {
    const headers = new Headers(init?.headers);
    if (!(init?.body instanceof FormData)) {
      headers.set("Content-Type", "application/json");
    }
    const token = getAccessToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    return fetch(apiUrl(route), { ...init, headers });
  };

  let res = await doFetch();
  if (res.status === 401 && getTokens()?.refresh_token) {
    const ok = await rotateRefresh();
    if (ok) res = await doFetch();
  }
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((body as { error?: string }).error ?? `HTTP ${res.status}`);
  }
  return body as T;
}

/** Fire-and-forget JSON POST used by the player's progress beacon. */
export function beacon(route: string, payload: unknown): void {
  const token = getAccessToken();
  void fetch(apiUrl(route), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(payload),
    keepalive: true,
  }).catch(() => {});
}

export const routes = API_ROUTES;
