"use client";

/**
 * Discover (Phase 5): trending + personalized recommendations, served by
 * /trending and /recommendations. This page consumes the GENERATED types
 * from @goonj/api-client (openapi.yaml → schema.gen.ts) — the response
 * shapes below are inferred from the spec, not hand-written.
 */

import { useEffect, useState } from "react";
import Link from "next/link";
import { ApiClient } from "@goonj/api-client";
import { API_BASE_URL } from "@goonj/shared";
import { getAccessToken } from "@/lib/api";
import { TrackCard, type CardItem } from "@/components/track-card";

// One shared instance: token provider is read per request, so a refresh
// rotation elsewhere in the app is picked up on the next call.
const client = new ApiClient({
  baseUrl: API_BASE_URL,
  token: getAccessToken,
});

type TrendingPayload = Awaited<ReturnType<typeof client.get<"/trending">>>;
type RecsPayload = Awaited<ReturnType<typeof client.get<"/recommendations">>>;

export default function DiscoverPage() {
  const [trending, setTrending] = useState<TrendingPayload | null>(null);
  const [recs, setRecs] = useState<RecsPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [authed, setAuthed] = useState(false);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    client
      .get("/trending")
      .then(setTrending)
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(() => {
    if (!authed) return; // /recommendations requires auth; falls back server-side anyway
    client
      .get("/recommendations")
      .then(setRecs)
      .catch(() => setRecs(null));
  }, [authed]);

  const trendingItems = (trending?.data ?? []) as CardItem[];
  const recItems = (recs?.data ?? []) as CardItem[];

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <Link href="/" className="text-sm text-zinc-400 hover:text-zinc-100">
            ← Home
          </Link>
          <h1 className="mt-2 text-4xl font-bold">Discover</h1>
          <p className="text-zinc-400">
            What the room is listening to right now.
          </p>
        </header>

        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}

        {authed && recItems.length > 0 && (
          <section className="mb-12">
            <h2 className="mb-1 text-xl font-semibold">🧭 For you</h2>
            {recs?.seed && (
              <p className="mb-4 text-xs text-zinc-500">
                Because you listened to an episode recently.
              </p>
            )}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {recItems.map((a) => (
                <TrackCard key={a.id} item={a} queue={recItems} />
              ))}
            </div>
          </section>
        )}

        <section>
          <h2 className="mb-4 text-xl font-semibold">🔥 Trending</h2>
          {trendingItems.length === 0 ? (
            <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
              Nothing trending yet — likes, comments and listens feed this
              list.
            </p>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {trendingItems.map((a) => (
                <TrackCard key={a.id} item={a} queue={trendingItems} />
              ))}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
