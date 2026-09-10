/**
 * @goonj/shared — constants and helpers shared between web apps and packages.
 * Keep this dependency-free: it must be importable from any TS runtime.
 */

export const APP_NAME = "Goonj" as const;
export const APP_TAGLINE = "Modern Day Radio" as const;

/** Default API base URL; override with NEXT_PUBLIC_API_URL. */
export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

/** API version prefix used by all REST routes. */
export const API_VERSION = "v1" as const;

/** Well-known API route names, kept in one place so the client stays typed. */
export const API_ROUTES = {
  ping: "/ping",
  health: "/health",
  healthServices: "/health/services",
  authRegister: "/auth/register",
  authLogin: "/auth/login",
} as const;

export type ApiRoute = (typeof API_ROUTES)[keyof typeof API_ROUTES];

/** Build an absolute API URL from a route path. */
export function apiUrl(route: string): string {
  return `${API_BASE_URL}${route}`;
}
