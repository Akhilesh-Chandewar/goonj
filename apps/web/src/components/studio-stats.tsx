"use client";

import Link from "next/link";
import type { StudioStats } from "@/lib/live-types";

const fmtHours = (seconds: number) => {
  if (seconds < 3600) return `${Math.round(seconds / 60)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
};

/** Compact creator dashboard: totals + top episodes + live recap. */
export function StudioStatsCard({ stats }: { stats: StudioStats }) {
  return (
    <section className="mb-8 grid gap-4 lg:grid-cols-3">
      <div className="rounded-2xl border border-zinc-800 bg-zinc-900 p-5 lg:col-span-1">
        <h2 className="mb-4 text-sm font-semibold uppercase tracking-wide text-zinc-500">
          Channel
        </h2>
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-zinc-400">Published episodes</dt>
            <dd className="font-semibold">{stats.episodes}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-zinc-400">Total content</dt>
            <dd className="font-semibold">{fmtHours(stats.total_seconds)}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-zinc-400">Likes received</dt>
            <dd className="font-semibold">{stats.total_likes}</dd>
          </div>
        </dl>

        <h3 className="mb-2 mt-6 text-sm font-semibold uppercase tracking-wide text-zinc-500">
          Live sessions
        </h3>
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-zinc-400">Completed</dt>
            <dd className="font-semibold">{stats.live_sessions.total}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-zinc-400">Peak listeners</dt>
            <dd className="font-semibold">{stats.live_sessions.peak_listeners}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-zinc-400">Total tune-ins</dt>
            <dd className="font-semibold">{stats.live_sessions.total_listeners}</dd>
          </div>
        </dl>
      </div>

      <div className="rounded-2xl border border-zinc-800 bg-zinc-900 p-5">
        <h2 className="mb-4 text-sm font-semibold uppercase tracking-wide text-zinc-500">
          Top episodes
        </h2>
        {stats.top_audio.length === 0 ? (
          <p className="text-sm text-zinc-500">Publish an episode to see stats.</p>
        ) : (
          <ol className="space-y-3 text-sm">
            {stats.top_audio.map((t, i) => (
              <li key={t.id} className="flex items-center justify-between gap-3">
                <span className="min-w-0">
                  <span className="mr-2 text-zinc-500">{i + 1}.</span>
                  <Link href={`/audio/${t.id}`} className="font-medium hover:text-red-300">
                    {t.title}
                  </Link>
                </span>
                <span className="whitespace-nowrap text-zinc-400">♥ {t.likes}</span>
              </li>
            ))}
          </ol>
        )}
      </div>

      <div className="rounded-2xl border border-zinc-800 bg-zinc-900 p-5">
        <h2 className="mb-4 text-sm font-semibold uppercase tracking-wide text-zinc-500">
          Recent publishes
        </h2>
        {stats.recent_publishes.length === 0 ? (
          <p className="text-sm text-zinc-500">Nothing published yet.</p>
        ) : (
          <ul className="space-y-3 text-sm">
            {stats.recent_publishes.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-3">
                <Link href={`/audio/${p.id}`} className="min-w-0 truncate font-medium hover:text-red-300">
                  {p.title}
                </Link>
                <span className="whitespace-nowrap text-xs text-zinc-500">
                  {p.published_at ? new Date(p.published_at).toLocaleDateString() : "—"}
                </span>
              </li>
            ))}
          </ul>
        )}
        {stats.live_sessions.total > 0 && (
          <p className="mt-4 border-t border-zinc-800 pt-3 text-xs text-zinc-500">
            Live analytics roll up from your broadcasts — peak{" "}
            {stats.live_sessions.peak_listeners} concurrent listeners.
          </p>
        )}
      </div>
    </section>
  );
}
