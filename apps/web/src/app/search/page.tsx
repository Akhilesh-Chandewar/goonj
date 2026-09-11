"use client";

import { Suspense, useEffect, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { api } from "@/lib/api";
import { TrackCard, type CardItem } from "@/components/track-card";
import type { HitCreator, SearchResults } from "@/lib/live-types";

function SearchInner() {
  const params = useSearchParams();
  const q = params.get("q") ?? "";
  const [results, setResults] = useState<SearchResults | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!q.trim()) {
      setResults(null);
      return;
    }
    api<SearchResults>(`/search?q=${encodeURIComponent(q)}`)
      .then(setResults)
      .catch((e: Error) => setError(e.message));
  }, [q]);

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <Link href="/" className="text-sm text-zinc-400 hover:text-zinc-100">
            ← Home
          </Link>
          <h1 className="mt-2 text-2xl font-bold">
            {q ? <>Results for “{q}”</> : "Search"}
          </h1>
        </header>

        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}

        {results && (
          <div className="space-y-10">
            {results.creators.length > 0 && (
              <section>
                <h2 className="mb-4 text-lg font-semibold">Creators</h2>
                <div className="grid gap-4 sm:grid-cols-2">
                  {results.creators.map((c: HitCreator) => (
                    <Link
                      key={c.id}
                      href={`/creators/${c.id}`}
                      className="flex items-center gap-4 rounded-xl border border-zinc-800 bg-zinc-900 p-4 hover:border-zinc-600"
                    >
                      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-zinc-800 text-xl">
                        {c.is_live ? "🔴" : "🎙"}
                      </div>
                      <div>
                        <p className="font-semibold">{c.channel_name}</p>
                        <p className="text-sm text-zinc-500">
                          @{c.handle} · {c.subscriber_count} subscribers
                        </p>
                      </div>
                    </Link>
                  ))}
                </div>
              </section>
            )}

            <section>
              <h2 className="mb-4 text-lg font-semibold">Episodes</h2>
              {results.audio.length === 0 ? (
                <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-8 text-center text-zinc-400">
                  No episodes matched.
                </p>
              ) : (
                <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                  {results.audio.map((a) => (
                    <TrackCard key={a.id} item={a as CardItem} queue={results.audio} />
                  ))}
                </div>
              )}
            </section>
          </div>
        )}

        {!results && !error && q && <p className="text-zinc-400">Searching…</p>}
      </div>
    </main>
  );
}

export default function SearchPage() {
  return (
    <Suspense fallback={
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Loading…
      </main>
    }>
      <SearchInner />
    </Suspense>
  );
}
