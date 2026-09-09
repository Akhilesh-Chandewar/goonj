"use client";

import { use, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { usePlayer, type Track } from "@/stores/player";
import type { AudioItem, AudioSource } from "@/lib/live-types";

interface PlaybackResponse {
  audio: AudioItem;
  sources: AudioSource[];
  waveform?: number[];
}

export default function AudioPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [pb, setPb] = useState<PlaybackResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const playTrack = usePlayer((s) => s.playTrack);

  useEffect(() => {
    api<PlaybackResponse>(`/audio/${id}/playback`)
      .then(setPb)
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
  if (!pb) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Loading…
      </main>
    );
  }

  const a = pb.audio;
  const track: Track = {
    id: a.id,
    title: a.title,
    creator: a.creator_name || a.handle || "Unknown",
    durationMs: a.duration_ms,
    sources: pb.sources,
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-12 text-zinc-50">
      <div className="mx-auto max-w-3xl">
        <div className="rounded-3xl border border-zinc-800 bg-gradient-to-b from-zinc-900 to-zinc-950 p-10 text-center">
          <div className="mx-auto mb-6 flex h-40 w-40 items-center justify-center rounded-2xl bg-zinc-800 text-6xl">
            🎙
          </div>
          <h1 className="text-3xl font-bold">{a.title}</h1>
          <p className="mt-2 text-zinc-400">
            {a.creator_name || a.handle} · {Math.round((a.duration_ms || 0) / 60000)} min · {a.category}
          </p>

          {pb.waveform && pb.waveform.length > 0 && (
            <div className="mt-8 flex h-16 items-center justify-center gap-[2px]">
              {pb.waveform
                .filter((_, i) => i % 4 === 0)
                .map((amp, i) => (
                  <span
                    key={i}
                    className="w-1 rounded-full bg-red-500/80"
                    style={{ height: `${Math.max(amp, 4)}%` }}
                  />
                ))}
            </div>
          )}

          <button
            onClick={() => playTrack(track)}
            className="mt-8 rounded-full bg-red-600 px-10 py-3 text-lg font-semibold hover:bg-red-500"
          >
            ▶ Play
          </button>
        </div>

        {a.description && (
          <p className="mt-8 leading-7 text-zinc-300">{a.description}</p>
        )}
      </div>
    </main>
  );
}
