"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { usePlayer, type Track } from "@/stores/player";
import type { AudioItem } from "@/lib/live-types";
import { APP_NAME } from "@goonj/shared";

export default function Home() {
  const [items, setItems] = useState<AudioItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const playTrack = usePlayer((s) => s.playTrack);

  useEffect(() => {
    api<{ data: AudioItem[] }>("/audio")
      .then((r) => setItems(r.data ?? []))
      .catch((e: Error) => setError(e.message));
  }, []);

  const toTrack = (a: AudioItem): Track => ({
    id: a.id,
    title: a.title,
    creator: a.creator_name || a.handle || "Unknown",
    durationMs: a.duration_ms,
    sources: a.sources ?? [],
  });

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <header className="mb-10 flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold">{APP_NAME}</h1>
            <p className="text-zinc-400">Listen. Follow. Go live.</p>
          </div>
          <nav className="flex gap-4 text-sm">
            <Link href="/live" className="text-red-400 hover:underline">🔴 Live</Link>
            <Link href="/studio/upload" className="text-zinc-400 hover:underline">Upload</Link>
            <Link href="/login" className="text-zinc-400 hover:underline">Login</Link>
          </nav>
        </header>

        <h2 className="mb-4 text-xl font-semibold">Latest audio</h2>
        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}
        {items && items.length === 0 && (
          <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
            Nothing published yet — upload the first episode!
          </p>
        )}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items?.map((a) => (
            <div
              key={a.id}
              className="rounded-xl border border-zinc-800 bg-zinc-900 p-5"
            >
              <Link href={`/audio/${a.id}`} className="block">
                <h3 className="font-semibold hover:text-red-300">{a.title}</h3>
                <p className="mt-1 text-sm text-zinc-400">
                  {a.creator_name || a.handle} · {Math.round((a.duration_ms || 0) / 60000)} min
                </p>
              </Link>
              {(a.sources?.length ?? 0) > 0 && (
                <button
                  onClick={() => playTrack(toTrack(a), items.map(toTrack))}
                  className="mt-4 rounded-full bg-red-600 px-5 py-1.5 text-sm font-medium hover:bg-red-500"
                >
                  ▶ Play
                </button>
              )}
            </div>
          ))}
        </div>
      </div>
    </main>
  );
}
