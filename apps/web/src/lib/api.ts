"use client";

/**
 * Tiny auth/token store + API client for Goonj web.
 * Access token lives in memory + localStorage (Phase 2 bridge; httpOnly
 * cookie hardening lands with Phase 1 completion).
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

export function getAccessToken(): string | null {
  const raw = localStorage.getItem(TOKEN_KEY);
  if (!raw) return null;
  try {
    return (JSON.parse(raw) as Tokens).access_token;
  } catch {
    return null;
  }
}

export function clearTokens() {
  localStorage.removeItem(TOKEN_KEY);
}

export async function api<T>(
  route: string,
  init?: RequestInit & { auth?: boolean }
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  const token = getAccessToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const res = await fetch(apiUrl(route), { ...init, headers });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((body as { error?: string }).error ?? `HTTP ${res.status}`);
  }
  return body as T;
}

export const routes = API_ROUTES;
