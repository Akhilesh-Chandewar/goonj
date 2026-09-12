"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import type { LiveSession } from "@/lib/live-types";

export default function LivePage() {
  const [sessions, setSessions] = useState<LiveSession[] | null>(null);
  const [upcoming, setUpcoming] = useState<LiveSession[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api<{ data: LiveSession[] }>("/live")
      .then((r) => setSessions(r.data ?? []))
      .catch((e: Error) => setError(e.message));
    api<{ data: LiveSession[] }>("/live/upcoming")
      .then((r) => setUpcoming(r.data ?? []))
      .catch(() => setUpcoming([]));
  }, []);

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <h1 className="mb-2 text-4xl font-bold">🔴 Live Now</h1>
        <p className="mb-8 text-zinc-400">
          Audio rooms happening right now — jump in and listen.
        </p>

        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}

        {sessions && sessions.length === 0 && (
          <div className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center">
            <p className="text-lg text-zinc-300">No one is live right now.</p>
            <p className="mt-2 text-sm text-zinc-500">
              Creators: start a broadcast from your Studio.
            </p>
            <Link
              href="/studio/live"
              className="mt-6 inline-block rounded-full bg-red-600 px-6 py-2 font-medium hover:bg-red-500"
            >
              🔴 Go Live
            </Link>
          </div>
        )}

        {upcoming && upcoming.length > 0 && (
          <section className="mb-10">
            <h2 className="mb-4 text-xl font-semibold text-zinc-200">
              🗓 Upcoming shows
            </h2>
            <ul className="space-y-2">
              {upcoming.map((s) => (
                <li
                  key={s.id}
                  className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-zinc-800 bg-zinc-900 px-5 py-3"
                >
                  <span className="text-sm font-medium text-zinc-100">
                    {s.title}
                  </span>
                  <span className="text-sm text-zinc-400">
                    {s.creator_name || s.handle || "Creator"}
                  </span>
                  <span className="ml-auto text-sm text-red-300">
                    {s.scheduled_at
                      ? new Date(s.scheduled_at).toLocaleString([], {
                          weekday: "short",
                          month: "short",
                          day: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })
                      : ""}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        )}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {sessions?.map((s) => (
            <Link
              key={s.id}
              href={`/live/${s.id}`}
              className="group rounded-xl border border-zinc-800 bg-zinc-900 p-5 transition hover:border-red-500/50"
            >
              <div className="mb-3 flex items-center gap-2">
                <span className="relative flex h-2.5 w-2.5">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-500 opacity-75" />
                  <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-red-600" />
                </span>
                <span className="text-xs font-semibold uppercase tracking-wide text-red-400">
                  Live
                </span>
                <span className="ml-auto text-xs text-zinc-500">
                  {s.category}
                </span>
              </div>
              <h2 className="font-semibold group-hover:text-red-300">
                {s.title}
              </h2>
              <p className="mt-1 text-sm text-zinc-400">
                {s.creator_name || s.handle || "Creator"}
              </p>
            </Link>
          ))}
        </div>
      </div>
    </main>
  );
}
