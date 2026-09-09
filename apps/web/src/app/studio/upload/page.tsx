"use client";

import { useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import type { AudioItem } from "@/lib/live-types";

interface InitResponse {
  audio: AudioItem;
  upload_url: string;
  storage_key: string;
}

const ACCEPTED = ".mp3,.wav,.m4a,.aac,.flac,audio/*";

export default function UploadPage() {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [stage, setStage] = useState<"form" | "uploading" | "processing" | "done">("form");
  const [error, setError] = useState<string | null>(null);
  const [audioId, setAudioId] = useState<string | null>(null);
  const [progress, setProgress] = useState(0);

  const submit = async () => {
    if (!file || !title.trim()) return;
    setError(null);
    setStage("uploading");

    try {
      // 1. Ask the API for a presigned PUT URL.
      const init = await api<InitResponse>("/audio/uploads", {
        method: "POST",
        body: JSON.stringify({
          title,
          description,
          mime_type: file.type || guessMime(file.name),
        }),
      });
      setAudioId(init.audio.id);

      // 2. Upload directly to object storage (bypasses the API server).
      await new Promise<void>((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open("PUT", init.upload_url);
        xhr.setRequestHeader("Content-Type", file.type || guessMime(file.name));
        xhr.upload.onprogress = (e) =>
          setProgress(e.total > 0 ? Math.round((e.loaded / e.total) * 100) : 0);
        xhr.onload = () => (xhr.status < 300 ? resolve() : reject(new Error(`upload failed: ${xhr.status}`)));
        xhr.onerror = () => reject(new Error("upload failed"));
        xhr.send(file);
      });

      // 3. Tell the API; it queues the FFmpeg pipeline.
      setStage("processing");
      await api(`/audio/uploads/${init.audio.id}/complete`, { method: "POST" });
      setStage("done");
    } catch (e) {
      setError((e as Error).message);
      setStage("form");
    }
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-2xl">
        <h1 className="text-3xl font-bold">Upload audio</h1>
        <p className="mt-1 text-zinc-400">
          MP3, WAV, M4A, AAC or FLAC. Processed into low/medium/high qualities.
        </p>

        {stage === "form" && (
          <div className="mt-8 space-y-4 rounded-2xl border border-zinc-800 bg-zinc-900 p-6">
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Episode title"
              maxLength={200}
              className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
            />
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Description (optional)"
              rows={3}
              className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
            />
            <input
              type="file"
              accept={ACCEPTED}
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 text-sm text-zinc-300 file:mr-4 file:rounded-full file:border-0 file:bg-red-600 file:px-4 file:py-1.5 file:text-white"
            />
            {error && <p className="text-sm text-red-400">{error}</p>}
            <button
              onClick={submit}
              disabled={!file || !title.trim()}
              className="w-full rounded-full bg-red-600 py-3 font-semibold hover:bg-red-500 disabled:opacity-40"
            >
              Upload & process
            </button>
          </div>
        )}

        {stage === "uploading" && (
          <div className="mt-8 rounded-2xl border border-zinc-800 bg-zinc-900 p-8 text-center">
            <p className="mb-4 font-medium">Uploading… {progress}%</p>
            <div className="h-2 w-full rounded-full bg-zinc-800">
              <div className="h-full rounded-full bg-red-500" style={{ width: `${progress}%` }} />
            </div>
          </div>
        )}

        {stage === "processing" && (
          <div className="mt-8 rounded-2xl border border-zinc-800 bg-zinc-900 p-8 text-center">
            <p className="animate-pulse font-medium">⚙️ Processing audio (loudness + 3 qualities + waveform)…</p>
          </div>
        )}

        {stage === "done" && audioId && (
          <div className="mt-8 rounded-2xl border border-green-500/40 bg-green-500/10 p-8 text-center">
            <p className="text-lg font-medium text-green-300">✅ Ready!</p>
            <p className="mt-1 text-sm text-zinc-400">
              Your episode passed through the pipeline and is published.
            </p>
            <Link
              href={`/audio/${audioId}`}
              className="mt-6 inline-block rounded-full bg-red-600 px-8 py-2.5 font-medium hover:bg-red-500"
            >
              Open episode
            </Link>
          </div>
        )}
      </div>
    </main>
  );
}

function guessMime(name: string): string {
  const ext = name.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "mp3": return "audio/mpeg";
    case "wav": return "audio/wav";
    case "m4a": return "audio/mp4";
    case "aac": return "audio/aac";
    case "flac": return "audio/flac";
    default: return "application/octet-stream";
  }
}
