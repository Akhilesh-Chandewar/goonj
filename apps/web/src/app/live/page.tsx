"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import type { LiveSession } from "@/lib/live-types";

export default function LivePage() {
  const [sessions, setSessions] = useState<LiveSession[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api<{ data: LiveSession[] }>("/live")
      .then((r) => setSessions(r.data ?? []))
      .catch((e: Error) => setError(e.message));
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
