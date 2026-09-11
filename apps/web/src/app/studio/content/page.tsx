"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import type { AudioItem, StudioStats } from "@/lib/live-types";
import { PublishButton } from "@/app/studio/publish-button";
import { StudioStatsCard } from "@/components/studio-stats";

const statusColor: Record<string, string> = {
  READY: "text-green-400 border-green-500/40 bg-green-500/10",
  PROCESSING: "text-yellow-400 border-yellow-500/40 bg-yellow-500/10",
  UPLOADING: "text-zinc-400 border-zinc-600/40 bg-zinc-500/10",
  FAILED: "text-red-400 border-red-500/40 bg-red-500/10",
};

const isLiveRecording = (a: AudioItem) => a.source === "live_recording";
const isDraft = (a: AudioItem) =>
  a.visibility === "private" || (a.status === "READY" && isLiveRecording(a));

export default function StudioContentPage() {
  const [items, setItems] = useState<AudioItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [stats, setStats] = useState<StudioStats | null>(null);

  useEffect(() => {
    const load = () =>
      api<{ data: AudioItem[] }>("/audio/mine")
        .then((r) => setItems(r.data ?? []))
        .catch((e: Error) => setError(e.message));
    load();
    api<StudioStats>("/audio/studio/stats")
      .then(setStats)
      .catch(() => {});
    const t = setInterval(load, 5000); // watch processing statuses
    return () => clearInterval(t);
  }, []);

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex items-center justify-between">
          <h1 className="text-3xl font-bold">Your content</h1>
          <Link
            href="/studio/upload"
            className="rounded-full bg-red-600 px-6 py-2 font-medium hover:bg-red-500"
          >
            + Upload
          </Link>
        </div>

        {error && (
          <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error} — log in as a creator.
          </p>
        )}

        {stats && <StudioStatsCard stats={stats} />}

        {items && items.length === 0 && (
          <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
            No uploads yet.
          </p>
        )}

        <div className="space-y-3">
          {items?.map((a) => (
            <div
              key={a.id}
              className="flex items-center gap-4 rounded-xl border border-zinc-800 bg-zinc-900 p-4"
            >
              <div className="min-w-0 flex-1">
                <Link href={`/audio/${a.id}`} className="truncate font-medium hover:text-red-300">
                  {isLiveRecording(a) && "🔴 "}
                  {a.title}
                </Link>
                <p className="text-xs text-zinc-500">
                  {a.category} · {new Date(a.created_at).toLocaleDateString()}
                  {isDraft(a) && " · draft (only you can see this)"}
                </p>
              </div>
              {a.status === "READY" && isDraft(a) && (
                <PublishButton audioId={a.id} title={a.title} />
              )}
              <span
                className={`rounded-full border px-3 py-1 text-xs font-medium ${
                  statusColor[a.status] ?? "text-zinc-400 border-zinc-700"
                }`}
              >
                {a.status === "PROCESSING" && "⚙️ "}
                {a.status}
              </span>
            </div>
          ))}
        </div>
      </div>
    </main>
  );
}
