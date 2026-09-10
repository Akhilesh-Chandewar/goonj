"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";
import type { AudioItem } from "@/lib/live-types";

/**
 * PublishButton flips a draft (private READY audio, e.g. a live recording)
 * into a public episode. Kept in its own file so both the content list and
 * the recording status card can use it.
 */
export function PublishButton({
  audioId,
  title,
  onPublished,
}: {
  audioId: string;
  title: string;
  onPublished?: (updated: AudioItem) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const publish = async () => {
    setBusy(true);
    setError(null);
    try {
      const updated = await api<AudioItem>(`/audio/${audioId}`, {
        method: "PATCH",
        body: JSON.stringify({ visibility: "public" }),
      });
      onPublished?.(updated);
      router.refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="flex flex-col items-end gap-1">
      <button
        onClick={publish}
        disabled={busy}
        title={`Publish “${title}”`}
        className="rounded-full bg-green-600 px-4 py-1.5 text-xs font-semibold text-white transition hover:bg-green-500 disabled:opacity-40"
      >
        {busy ? "Publishing…" : "Publish"}
      </button>
      {error && <span className="text-xs text-red-400">{error}</span>}
    </span>
  );
}
