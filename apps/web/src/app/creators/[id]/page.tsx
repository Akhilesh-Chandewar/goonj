"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { api, routes } from "@/lib/api";
import { useFollow } from "@/lib/engagement";
import { TrackCard, type CardItem } from "@/components/track-card";
import type { CreatorPage } from "@/lib/live-types";

export default function CreatorPageView({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const [page, setPage] = useState<CreatorPage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { state: followState, toggle: toggleFollow } = useFollow(id);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    api<CreatorPage>(routes.creator(id))
      .then(setPage)
      .catch((e: Error) => setError(e.message));
  }, [id]);

  if (error) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-300">
        <p className="rounded-lg border border-red-500/40 bg-red-500/10 px-6 py-4 text-red-300">
          {error}
        </p>
      </main>
    );
  }
  if (!page) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Loading…
      </main>
    );
  }

  const c = page.creator;
  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <Link href="/" className="text-sm text-zinc-400 hover:text-zinc-100">
          ← Home
        </Link>

        <header className="mt-4 flex items-center gap-5 rounded-2xl border border-zinc-800 bg-zinc-900 p-6">
          <div className="flex h-16 w-16 items-center justify-center rounded-full bg-zinc-800 text-3xl">
            {c.is_live ? "🔴" : "🎙"}
          </div>
          <div className="flex-1">
            <h1 className="text-2xl font-bold">{c.channel_name}</h1>
            <p className="text-sm text-zinc-500">
              @{c.handle} ·{" "}
              {followState?.subscribers ?? c.subscriber_count} subscribers
            </p>
            {c.tagline && <p className="mt-1 text-zinc-400">{c.tagline}</p>}
          </div>
          {c.is_live && (
            <Link
              href="/live"
              className="rounded-full bg-red-600 px-4 py-2 text-sm font-medium hover:bg-red-500"
            >
              🔴 Live now
            </Link>
          )}
          <button
            onClick={() =>
              toggleFollow().catch((e: Error) => setActionError(e.message))
            }
            className={`rounded-full px-6 py-2 text-sm font-medium ${
              followState?.following
                ? "bg-zinc-800 text-zinc-200 hover:bg-zinc-700"
                : "bg-red-600 text-white hover:bg-red-500"
            }`}
          >
            {followState?.following ? "Following" : "Follow"}
          </button>
        </header>
        {actionError && <p className="mt-3 text-sm text-red-400">{actionError}</p>}

        <h2 className="mb-4 mt-10 text-xl font-semibold">Episodes</h2>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {page.episodes.map((e) => (
            <TrackCard key={e.id} item={e as CardItem} queue={page.episodes} />
          ))}
        </div>
        {page.episodes.length === 0 && (
          <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-8 text-center text-zinc-400">
            No episodes published yet.
          </p>
        )}
      </div>
    </main>
  );
}
