"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { usePlayer } from "@/stores/player";
import { TrackCard, toTrack, type CardItem } from "@/components/track-card";
import type { AudioItem, HitAudio } from "@/lib/live-types";
import { APP_NAME } from "@goonj/shared";

type Tab = "latest" | "search" | "following";

export default function Home() {
  const [items, setItems] = useState<AudioItem[] | null>(null);
  const [feed, setFeed] = useState<HitAudio[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("latest");
  const [q, setQ] = useState("");
  const [hasToken, setHasToken] = useState(false);
  const playTrack = usePlayer((s) => s.playTrack);

  useEffect(() => {
    setHasToken(!!localStorage.getItem("goonj.tokens"));
    api<{ data: AudioItem[] }>("/audio")
      .then((r) => setItems(r.data ?? []))
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(() => {
    if (tab !== "following") return;
    setFeed(null);
    api<{ data: HitAudio[] }>("/subscriptions/feed")
      .then((r) => setFeed(r.data ?? []))
      .catch((e: Error) => setError(e.message));
  }, [tab]);

  const runSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!q.trim()) return;
    setError(null);
    setTab("search");
    setFeed(null);
    try {
      const r = await api<{ audio: HitAudio[] }>(
        `/search?q=${encodeURIComponent(q)}`
      );
      setFeed(r.audio ?? []);
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const shown: CardItem[] =
    tab === "latest" ? (items ?? []) : (feed ?? []);

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8 flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold">{APP_NAME}</h1>
            <p className="text-zinc-400">Tune in. Go live.</p>
          </div>
          <nav className="flex gap-4 text-sm">
            <Link href="/live" className="text-red-400 hover:underline">🔴 Live</Link>
            <Link href="/library" className="text-zinc-400 hover:underline">Library</Link>
            <Link href="/studio/upload" className="text-zinc-400 hover:underline">Upload</Link>
            <Link href="/login" className="text-zinc-400 hover:underline">
              {hasToken ? "Account" : "Login"}
            </Link>
          </nav>
        </header>

        <form onSubmit={runSearch} className="mb-6 flex gap-2">
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search episodes and creators…"
            className="w-full rounded-lg border border-zinc-800 bg-zinc-900 px-4 py-2.5 text-sm outline-none focus:border-red-500"
            aria-label="search"
          />
          <button
            type="submit"
            className="rounded-lg bg-red-600 px-5 text-sm font-medium hover:bg-red-500"
          >
            Search
          </button>
        </form>

        <div className="mb-4 flex gap-2 text-sm">
          {(
            [
              ["latest", "Latest"],
              ["following", "Following"],
            ] as [Tab, string][]
          ).map(([key, label]) => (
            <button
              key={key}
              onClick={() => setTab(key)}
              className={`rounded-full px-4 py-1.5 ${
                tab === key
                  ? "bg-zinc-100 text-zinc-900"
                  : "bg-zinc-900 text-zinc-400 hover:text-zinc-100"
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}

        {tab === "latest" && items && items.length === 0 && (
          <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
            Nothing published yet — upload the first episode!
          </p>
        )}
        {tab !== "latest" && feed && feed.length === 0 && (
          <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
            {tab === "following"
              ? "Follow creators to build your feed."
              : "No results."}
          </p>
        )}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((a) => (
            <TrackCard
              key={`${tab}-${a.id}`}
              item={a}
              queue={shown}
            />
          ))}
        </div>
      </div>
    </main>
  );
}
