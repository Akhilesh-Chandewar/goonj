"use client";

/**
 * Notifications bell + unread badge + dropdown list (Phase 5 completion).
 * Polls lazily: the badge pings every 30s, the list loads only when opened.
 * Delivery server-side is lazy too (clients poll; no push infra).
 */

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { api, getAccessToken, routes } from "@/lib/api";

export interface NotificationItem {
  id: string;
  type: string;
  title: string;
  body: string;
  session_id?: string;
  audio_id?: string;
  actor_id?: string;
  read: boolean;
  created_at: string;
}

const ICONS: Record<string, string> = {
  live_started: "🔴",
  recording_ready: "🎙",
  new_follower: "👤",
  session_reminder: "⏰",
};

function hrefFor(n: NotificationItem): string | null {
  if (n.audio_id) return `/audio/${n.audio_id}`;
  if (n.session_id) return `/live/${n.session_id}`;
  return null;
}

export function NotificationBell() {
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<NotificationItem[] | null>(null);
  const [authed, setAuthed] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // localStorage is browser-only — flip after mount so SSR prerender is safe.
  useEffect(() => {
    setAuthed(!!getAccessToken());
  }, []);

  // Badge: cheap poll; only when signed in.
  useEffect(() => {
    if (!authed) return;
    let alive = true;
    const ping = () =>
      api<{ unread: number }>(routes.notificationsUnread)
        .then((r) => alive && setUnread(r.unread ?? 0))
        .catch(() => {});
    ping();
    const t = setInterval(ping, 30_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [authed]);

  // List: loaded on first open, then refreshed on each open.
  useEffect(() => {
    if (!open || !authed) return;
    api<{ data: NotificationItem[] }>(routes.notifications)
      .then((r) => setItems(r.data ?? []))
      .catch(() => setItems([]));
  }, [open, authed]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  const markRead = useCallback(async (id: string) => {
    await api(routes.notificationRead(id), { method: "POST" }).catch(() => {});
  }, []);

  const markAll = useCallback(async () => {
    await api(routes.notificationsReadAll, { method: "POST" }).catch(() => {});
    setItems((xs) => xs?.map((n) => ({ ...n, read: true })) ?? xs);
    setUnread(0);
  }, []);

  if (!authed) return null;

  const onOpen = (n: NotificationItem) => {
    if (!n.read) void markRead(n.id);
    setOpen(false);
    setUnread((u) => Math.max(0, u - (n.read ? 0 : 1)));
  };

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="relative rounded-full px-2 py-1 text-lg hover:bg-zinc-800"
        aria-label={`Notifications${unread ? ` (${unread} unread)` : ""}`}
      >
        🔔
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 min-w-[1.1rem] rounded-full bg-red-600 px-1 text-center text-[0.65rem] font-bold leading-4 text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 z-50 mt-2 w-80 rounded-xl border border-zinc-800 bg-zinc-900 shadow-xl">
          <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-2.5 text-sm">
            <span className="font-medium">Notifications</span>
            <button
              onClick={markAll}
              className="text-xs text-zinc-400 hover:text-zinc-200"
            >
              Mark all read
            </button>
          </div>
          <div className="max-h-96 overflow-y-auto">
            {items === null && (
              <p className="px-4 py-6 text-center text-sm text-zinc-500">
                Loading…
              </p>
            )}
            {items?.length === 0 && (
              <p className="px-4 py-6 text-center text-sm text-zinc-500">
                Nothing yet — follow creators to hear when they go live.
              </p>
            )}
            {items?.map((n) => {
              const href = hrefFor(n);
              const row = (
                <div
                  className={`border-b border-zinc-800/60 px-4 py-3 text-sm last:border-0 ${
                    n.read ? "" : "bg-red-950/20"
                  }`}
                >
                  <div className="flex items-start gap-2">
                    <span aria-hidden>{ICONS[n.type] ?? "🔔"}</span>
                    <div className="min-w-0">
                      <p className={`truncate font-medium ${n.read ? "text-zinc-300" : "text-zinc-100"}`}>
                        {n.title}
                      </p>
                      {n.body && (
                        <p className="mt-0.5 line-clamp-2 text-xs text-zinc-400">
                          {n.body}
                        </p>
                      )}
                      <p className="mt-1 text-[0.65rem] text-zinc-600">
                        {new Date(n.created_at).toLocaleString()}
                      </p>
                    </div>
                    {!n.read && (
                      <span className="ml-auto mt-1 h-2 w-2 shrink-0 rounded-full bg-red-500" />
                    )}
                  </div>
                </div>
              );
              return href ? (
                <Link key={n.id} href={href} onClick={() => onOpen(n)}>
                  {row}
                </Link>
              ) : (
                <div key={n.id} onClick={() => onOpen(n)} role="button">
                  {row}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
