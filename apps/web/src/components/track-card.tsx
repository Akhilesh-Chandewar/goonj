"use client";

import Link from "next/link";
import { usePlayer, type Track } from "@/stores/player";

/** Shape shared by AudioItem and the Phase 4 list types. */
export interface CardItem {
  id: string;
  title: string;
  category?: string;
  creator_name?: string;
  handle?: string;
  duration_ms?: number;
  sources?: { quality: string; url: string; bitrate_kbps: number }[];
}

export function toTrack(a: CardItem): Track {
  return {
    id: a.id,
    title: a.title,
    creator: a.creator_name || a.handle || "Unknown",
    durationMs: a.duration_ms ?? 0,
    sources: a.sources ?? [],
  };
}

export function TrackCard({
  item,
  queue,
}: {
  item: CardItem;
  queue?: CardItem[];
}) {
  const playTrack = usePlayer((s) => s.playTrack);
  const playable = (item.sources?.length ?? 0) > 0;

  return (
    <div className="rounded-xl border border-zinc-800 bg-zinc-900 p-5">
      <Link href={`/audio/${item.id}`} className="block">
        <h3 className="font-semibold hover:text-red-300">{item.title}</h3>
        <p className="mt-1 text-sm text-zinc-400">
          {item.creator_name || item.handle || "Unknown"}
          {item.duration_ms ? ` · ${Math.round(item.duration_ms / 60000)} min` : ""}
          {item.category ? ` · ${item.category}` : ""}
        </p>
      </Link>
      {playable && (
        <button
          onClick={() => playTrack(toTrack(item), queue?.map(toTrack))}
          className="mt-4 rounded-full bg-red-600 px-5 py-1.5 text-sm font-medium hover:bg-red-500"
        >
          ▶ Play
        </button>
      )}
    </div>
  );
}
