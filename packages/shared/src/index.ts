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
  authRefresh: "/auth/refresh",
  authLogout: "/auth/logout",
  audioFeed: "/audio",
  search: "/search",
  suggestions: "/search/suggestions",
  creatorsFollowed: "/creators/followed",
  creator: (id: string) => `/creators/${id}`,
  creatorFollow: (id: string) => `/creators/${id}/follow`,
  subscriptionsFeed: "/subscriptions/feed",
  playlists: "/playlists",
  playlist: (id: string) => `/playlists/${id}`,
  playlistItems: (id: string) => `/playlists/${id}/items`,
  playlistItem: (id: string, audioId: string) =>
    `/playlists/${id}/items/${audioId}`,
  history: "/history",
  historyProgress: (id: string) => `/history/${id}/progress`,
  historyResume: (id: string) => `/history/${id}/resume`,
  audioLiked: "/audio/liked",
  audioLike: (id: string) => `/audio/${id}/like`,
  audioComments: (id: string) => `/audio/${id}/comments`,
  comment: (commentId: string) => `/audio/comments/${commentId}`,
  commentLike: (commentId: string) => `/audio/comments/${commentId}/like`,
  studioStats: "/audio/studio/stats",
  // Phase 5 completion: notifications, reports, discovery, scheduling.
  notifications: "/notifications",
  notificationsUnread: "/notifications/unread-count",
  notificationsReadAll: "/notifications/read-all",
  notificationRead: (id: string) => `/notifications/${id}/read`,
  reports: "/reports",
  reportResolve: (id: string) => `/reports/${id}/resolve`,
  reportDismiss: (id: string) => `/reports/${id}/dismiss`,
  trending: "/trending",
  recommendations: "/recommendations",
  recommendationsSimilar: (id: string) => `/recommendations/audio/${id}`,
  liveUpcoming: "/live/upcoming",
  liveSchedule: (id: string) => `/live/${id}/schedule`,
  audioTranscript: (id: string) => `/audio/${id}/transcript`,
} as const;

export type ApiRoute = (typeof API_ROUTES)[keyof typeof API_ROUTES];

/** Build an absolute API URL from a route path. */
export function apiUrl(route: string): string {
  return `${API_BASE_URL}${route}`;
}
