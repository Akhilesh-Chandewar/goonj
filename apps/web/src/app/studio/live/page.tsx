"use client";

import { useEffect, useRef, useState } from "react";
import { LiveKitRoom, useLocalMicTrack } from "@/lib/livekit-hooks";
import { api } from "@/lib/api";
import type {
  AudioItem,
  LiveRecording,
  LiveSession,
  StreamToken,
} from "@/lib/live-types";
import { PublishButton } from "@/app/studio/publish-button";

interface StartResponse {
  session: LiveSession;
  stream_token: StreamToken;
}

export default function StudioLivePage() {
  const [session, setSession] = useState<LiveSession | null>(null);
  const [token, setToken] = useState<StreamToken | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [endedSessionId, setEndedSessionId] = useState<string | null>(null);

  const createAndStart = async () => {
    setBusy(true);
    setError(null);
    try {
      const created = await api<LiveSession>("/live", {
        method: "POST",
        body: JSON.stringify({ title, description, visibility: "public" }),
      });
      const started = await api<StartResponse>(`/live/${created.id}/start`, {
        method: "POST",
      });
      setSession(started.session);
      setToken(started.stream_token);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const endStream = async () => {
    if (!session) return;
    setBusy(true);
    try {
      await api(`/live/${session.id}/end`, { method: "POST" });
      // Phase 3: the recording is being finalized into a draft episode —
      // poll its status so the creator sees progress and can publish.
      setEndedSessionId(session.id);
    } finally {
      setSession(null);
      setToken(null);
      setBusy(false);
    }
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-3xl">
        <h1 className="text-3xl font-bold">🔴 Go Live</h1>
        <p className="mt-1 text-zinc-400">
          Broadcast your voice. Listeners join in real time.
        </p>

        {error && (
          <p className="mt-6 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            {error} — you need a creator account (register with as_creator).
          </p>
        )}

        {!session && (
          <div className="mt-8 space-y-4 rounded-2xl border border-zinc-800 bg-zinc-900 p-6">
            <div>
              <label className="mb-1 block text-sm text-zinc-400">Title</label>
              <input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                maxLength={200}
                placeholder="Late night chat — ask me anything"
                className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
              />
            </div>
            <div>
              <label className="mb-1 block text-sm text-zinc-400">
                Description (optional)
              </label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
              />
            </div>
            <button
              onClick={createAndStart}
              disabled={busy || !title.trim()}
              className="w-full rounded-full bg-red-600 py-3 font-semibold transition hover:bg-red-500 disabled:opacity-40"
            >
              {busy ? "Starting…" : "🔴 Start Broadcast"}
            </button>
          </div>
        )}

        {session && token && (
          <OnAir session={session} token={token} onEnd={endStream} busy={busy} />
        )}

        {endedSessionId && (
          <RecordingStatusCard
            sessionId={endedSessionId}
            onDismiss={() => setEndedSessionId(null)}
          />
        )}
      </div>
    </main>
  );
}

function RecordingStatusCard({
  sessionId,
  onDismiss,
}: {
  sessionId: string;
  onDismiss: () => void;
}) {
  const [rec, setRec] = useState<LiveRecording | null>(null);
  const [missing, setMissing] = useState(false);
  const [draft, setDraft] = useState<AudioItem | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const r = await api<LiveRecording>(`/live/${sessionId}/recording`);
        if (cancelled) return;
        setRec(r);
        if (r.audio_id) {
          // Follow the draft episode until it is READY (publishable).
          try {
            const a = await api<AudioItem>(`/audio/${r.audio_id}`);
            if (!cancelled) setDraft(a);
          } catch {
            /* draft not visible yet; keep polling */
          }
        }
        if (r.status === "CONVERTED" && r.audio_id) {
          // Draft exists; stop once the audio row is READY.
          if (draft?.status === "READY") return;
        }
      } catch {
        if (!cancelled) setMissing(true);
      }
    };
    load();
    const t = setInterval(load, 4000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [sessionId, rec?.status, draft?.status]);

  if (missing) {
    return (
      <div className="mt-6 rounded-xl border border-zinc-800 bg-zinc-900 p-4 text-sm text-zinc-400">
        Recording info unavailable for this session.
        <button onClick={onDismiss} className="ml-3 text-red-400 hover:underline">
          dismiss
        </button>
      </div>
    );
  }

  if (!rec) return null;

  const label: Record<LiveRecording["status"], string> = {
    RECORDING: "Recording…",
    ENDING: "Saving your stream — processing recording…",
    COMPLETED: "Recording saved — creating draft episode…",
    CONVERTED: draft && draft.status !== "READY" ? "Draft episode processing…" : "Draft episode ready!",
    FAILED: "Recording failed",
  };

  return (
    <div className="mt-6 rounded-xl border border-zinc-800 bg-zinc-900 p-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-zinc-300">
          {rec.status === "FAILED" ? "⚠️ " : "⏳ "}
          {label[rec.status] ?? rec.status}
        </p>
        {rec.status === "CONVERTED" && draft?.status === "READY" ? (
          <PublishButton
            audioId={draft.id}
            title={draft.title}
            onPublished={() => setDraft({ ...draft, visibility: "public" })}
          />
        ) : (
          <button onClick={onDismiss} className="text-xs text-zinc-500 hover:text-zinc-300">
            dismiss
          </button>
        )}
      </div>
      {rec.status === "FAILED" && rec.error && (
        <p className="mt-2 text-xs text-red-400">{rec.error}</p>
      )}
    </div>
  );
}

function OnAir({
  session,
  token,
  onEnd,
  busy,
}: {
  session: LiveSession;
  token: StreamToken;
  onEnd: () => void;
  busy: boolean;
}) {
  const [elapsed, setElapsed] = useState(0);
  const startRef = useRef(Date.now());

  useEffect(() => {
    startRef.current = Date.now();
    setElapsed(0);
    const id = setInterval(
      () => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)),
      1000
    );
    return () => clearInterval(id);
  }, [session.id]);

  const { enabled, toggle, track, ready } = useLocalMicTrack(
    token.token,
    token.ws_url,
    token.room_name
  );
  void track;

  // Phase 3: once the mic is published, start participant-egress recording.
  // Recording is what turns the stream into a saved episode later.
  const [recState, setRecState] = useState<"idle" | "starting" | "on" | "unavailable">("idle");
  useEffect(() => {
    if (!ready || recState !== "idle") return;
    setRecState("starting");
    api<LiveRecording>(`/live/${session.id}/recording/start`, { method: "POST" })
      .then(() => setRecState("on"))
      .catch(() => {
        setRecState("unavailable");
      });
  }, [ready, recState, session.id]);

  const mm = String(Math.floor(elapsed / 60)).padStart(2, "0");
  const ss = String(elapsed % 60).padStart(2, "0");

  return (
    <div className="mt-8 rounded-2xl border border-red-500/40 bg-gradient-to-b from-red-950/40 to-zinc-900 p-6">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-2 font-semibold text-red-400">
          <span className="relative flex h-3 w-3">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-500 opacity-75" />
            <span className="relative inline-flex h-3 w-3 rounded-full bg-red-600" />
          </span>
          ON AIR · {mm}:{ss}
        </span>
        <button
          onClick={onEnd}
          disabled={busy}
          className="rounded-full bg-zinc-800 px-5 py-2 text-sm font-medium hover:bg-zinc-700 disabled:opacity-40"
        >
          End stream
        </button>
      </div>

      <h2 className="mt-4 text-2xl font-bold">{session.title}</h2>

      <p className="mt-1 text-xs text-zinc-400">
        {recState === "on" && "⏺ Recording — it will be saved as a draft episode when you end the stream."}
        {recState === "starting" && "Starting recording…"}
        {recState === "unavailable" && "⚠️ Recording unavailable — the stream will not be saved."}
        {recState === "idle" && " "}
      </p>

      <button
        onClick={toggle}
        className="mt-6 rounded-full border border-zinc-700 px-6 py-3 font-medium hover:bg-zinc-800"
      >
        {enabled ? "🎙 Mute microphone" : "🔇 Unmute microphone"}
      </button>

      <LiveKitRoom
        token={token.token}
        serverUrl={token.ws_url}
        connect={true}
        audio={false}
        video={false}
        className="hidden"
      >
        <audio id="goonj-self-monitor" className="hidden" />
      </LiveKitRoom>
    </div>
  );
}
