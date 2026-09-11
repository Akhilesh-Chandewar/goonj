"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, getAccessToken, routes } from "@/lib/api";
import { TrackCard, toTrack, type CardItem } from "@/components/track-card";
import { usePlayer } from "@/stores/player";
import type {
  Creator,
  HistoryEntry,
  Playlist,
} from "@/lib/live-types";

type Tab = "likes" | "following" | "playlists" | "history";

export default function LibraryPage() {
  const [tab, setTab] = useState<Tab>("likes");
  const [signedIn, setSignedIn] = useState(true);
  const [liked, setLiked] = useState<CardItem[] | null>(null);
  const [followed, setFollowed] = useState<Creator[] | null>(null);
  const [playlists, setPlaylists] = useState<Playlist[] | null>(null);
  const [history, setHistory] = useState<HistoryEntry[] | null>(null);
  const [openPlaylist, setOpenPlaylist] = useState<Playlist | null>(null);
  const [error, setError] = useState<string | null>(null);
  const playTrack = usePlayer((s) => s.playTrack);

  useEffect(() => {
    if (!getAccessToken()) {
      setSignedIn(false);
      return;
    }
    api<{ data: CardItem[] }>(routes.audioLiked)
      .then((r) => setLiked(r.data ?? []))
      .catch((e: Error) => setError(e.message));
    api<{ data: Creator[] }>(routes.creatorsFollowed)
      .then((r) => setFollowed(r.data ?? []))
      .catch(() => setFollowed([]));
    api<{ data: Playlist[] }>(routes.playlists)
      .then((r) => setPlaylists(r.data ?? []))
      .catch(() => setPlaylists([]));
    api<{ data: HistoryEntry[] }>(routes.history)
      .then((r) => setHistory(r.data ?? []))
      .catch(() => setHistory([]));
  }, []);

  if (!signedIn) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-4 bg-zinc-950 text-zinc-300">
        <p>Sign in to see your library.</p>
        <Link href="/login" className="rounded-full bg-red-600 px-6 py-2 text-sm font-medium hover:bg-red-500">
          Login
        </Link>
      </main>
    );
  }

  const openList = (pl: Playlist) => {
    api<Playlist>(routes.playlist(pl.id))
      .then(setOpenPlaylist)
      .catch((e: Error) => setError(e.message));
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <Link href="/" className="text-sm text-zinc-400 hover:text-zinc-100">← Home</Link>
          <h1 className="mt-2 text-3xl font-bold">Your library</h1>
        </header>

        <div className="mb-6 flex flex-wrap gap-2 text-sm">
          {(
            [
              ["likes", "Liked"],
              ["following", "Following"],
              ["playlists", "Playlists"],
              ["history", "History"],
            ] as [Tab, string][]
          ).map(([key, label]) => (
            <button
              key={key}
              onClick={() => { setTab(key); setOpenPlaylist(null); }}
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
          <p className="mb-6 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error}
          </p>
        )}

        {/* Liked */}
        {tab === "likes" && (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {(liked ?? []).map((a) => (
              <TrackCard key={a.id} item={a} queue={liked ?? []} />
            ))}
            {liked && liked.length === 0 && (
              <p className="text-zinc-400">No liked episodes yet.</p>
            )}
          </div>
        )}

        {/* Following */}
        {tab === "following" && (
          <div className="grid gap-4 sm:grid-cols-2">
            {(followed ?? []).map((c) => (
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
                  <p className="text-sm text-zinc-500">@{c.handle}</p>
                </div>
              </Link>
            ))}
            {followed && followed.length === 0 && (
              <p className="text-zinc-400">You are not following anyone yet.</p>
            )}
          </div>
        )}

        {/* Playlists */}
        {tab === "playlists" && !openPlaylist && (
          <div className="space-y-3">
            {(playlists ?? []).map((pl) => (
              <button
                key={pl.id}
                onClick={() => openList(pl)}
                className="block w-full rounded-xl border border-zinc-800 bg-zinc-900 p-4 text-left hover:border-zinc-600"
              >
                <p className="font-semibold">{pl.title}</p>
                <p className="text-sm text-zinc-500">
                  {pl.item_count} episodes · {pl.visibility}
                </p>
              </button>
            ))}
            {playlists && playlists.length === 0 && (
              <p className="text-zinc-400">No playlists yet — add episodes from any audio page.</p>
            )}
          </div>
        )}
        {tab === "playlists" && openPlaylist && (
          <div>
            <button
              onClick={() => setOpenPlaylist(null)}
              className="mb-4 text-sm text-zinc-400 hover:text-zinc-100"
            >
              ← All playlists
            </button>
            <h2 className="mb-4 text-xl font-semibold">{openPlaylist.title}</h2>
            <div className="space-y-2">
              {(openPlaylist.items ?? []).map((it) => (
                <div
                  key={it.audio_id}
                  className="flex items-center justify-between rounded-lg border border-zinc-800 bg-zinc-900 px-4 py-3"
                >
                  <div>
                    <Link href={`/audio/${it.audio_id}`} className="font-medium hover:text-red-300">
                      {it.title}
                    </Link>
                    <p className="text-xs text-zinc-500">{it.creator_name || it.handle}</p>
                  </div>
                  <div className="flex gap-2">
                    <button
                      onClick={() =>
                        playTrack(
                          {
                            id: it.audio_id,
                            title: it.title,
                            creator: it.creator_name || it.handle,
                            durationMs: it.duration_ms,
                            sources: [],
                          },
                          (openPlaylist.items ?? []).map((x) => ({
                            id: x.audio_id,
                            title: x.title,
                            creator: x.creator_name || x.handle,
                            durationMs: x.duration_ms,
                            sources: [],
                          }))
                        )
                      }
                      className="rounded-full bg-red-600 px-4 py-1.5 text-sm hover:bg-red-500"
                    >
                      ▶
                    </button>
                    <button
                      onClick={() =>
                        api(routes.playlistItem(openPlaylist.id, it.audio_id), { method: "DELETE" })
                          .then(() => openList(openPlaylist))
                          .catch((e: Error) => setError(e.message))
                      }
                      className="rounded-full border border-zinc-700 px-4 py-1.5 text-sm text-zinc-400 hover:border-red-500 hover:text-red-400"
                    >
                      Remove
                    </button>
                  </div>
                </div>
              ))}
              {openPlaylist.items?.length === 0 && (
                <p className="text-zinc-400">This playlist is empty.</p>
              )}
            </div>
          </div>
        )}

        {/* History */}
        {tab === "history" && (
          <div className="space-y-2">
            {(history ?? []).map((h) => {
              const pct = h.duration_ms > 0 ? Math.min((h.position_ms / h.duration_ms) * 100, 100) : 0;
              return (
                <div
                  key={h.audio_id}
                  className="rounded-lg border border-zinc-800 bg-zinc-900 px-4 py-3"
                >
                  <div className="flex items-center justify-between">
                    <Link href={`/audio/${h.audio_id}`} className="font-medium hover:text-red-300">
                      {h.title}
                    </Link>
                    <button
                      onClick={() =>
                        playTrack({
                          id: h.audio_id,
                          title: h.title,
                          creator: h.creator_name || h.handle,
                          durationMs: h.duration_ms,
                          sources: [],
                        })
                      }
                      className="rounded-full bg-red-600 px-4 py-1.5 text-sm hover:bg-red-500"
                    >
                      {h.completed ? "Replay" : `Resume ${Math.round(pct)}%`}
                    </button>
                  </div>
                  <div className="mt-2 h-1 w-full rounded-full bg-zinc-800">
                    <div className="h-full rounded-full bg-red-500" style={{ width: `${pct}%` }} />
                  </div>
                </div>
              );
            })}
            {history && history.length === 0 && (
              <p className="text-zinc-400">Nothing listened to yet.</p>
            )}
          </div>
        )}
      </div>
    </main>
  );
}
