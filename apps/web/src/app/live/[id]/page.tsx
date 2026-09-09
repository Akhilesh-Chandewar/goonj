"use client";

import { use, useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  LiveKitRoom,
  RoomAudioRenderer,
  useTracks,
} from "@livekit/components-react";
import { Track } from "livekit-client";
import "@livekit/components-styles";

import { api, getAccessToken } from "@/lib/api";
import type { LiveSession, StreamToken } from "@/lib/live-types";

const REACTIONS = [
  { id: "heart", emoji: "❤️" },
  { id: "clap", emoji: "👏" },
  { id: "fire", emoji: "🔥" },
  { id: "laugh", emoji: "😂" },
  { id: "party", emoji: "🎉" },
  { id: "thumbsup", emoji: "👍" },
];

interface JoinResponse {
  session: LiveSession;
  stream_token: StreamToken;
  concurrent: number;
  unique_listeners: number;
}

export default function LiveRoomPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const [join, setJoin] = useState<JoinResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [joining, setJoining] = useState(true);

  useEffect(() => {
    if (!getAccessToken()) {
      setError("Log in to join live rooms.");
      setJoining(false);
      return;
    }
    api<JoinResponse>(`/live/${id}/join`, { method: "POST" })
      .then(setJoin)
      .catch((e: Error) => setError(e.message))
      .finally(() => setJoining(false));
  }, [id]);

  if (joining) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Connecting to the room…
      </main>
    );
  }

  if (error) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-4 bg-zinc-950 text-zinc-300">
        <p className="rounded-lg border border-red-500/40 bg-red-500/10 px-6 py-4 text-red-300">
          {error}
        </p>
        <a href="/live" className="text-sm text-zinc-500 underline">
          ← back to Live Now
        </a>
      </main>
    );
  }

  if (!join) return null;

  const onLeave = useCallback(() => {
    api(`/live/${id}/leave`, { method: "POST" }).catch(() => {});
  }, [id]);

  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-50">
      <LiveKitRoom
        token={join.stream_token.token}
        serverUrl={join.stream_token.ws_url}
        connect={true}
        audio={true}
        video={false}
        onDisconnected={onLeave}
        className="lk-room-container h-screen"
      >
        <RoomAudioRenderer />
        <div className="mx-auto grid max-w-6xl gap-6 px-6 py-10 lg:grid-cols-[1fr_360px]">
          <section>
            <div className="mb-6 flex items-center gap-3">
              <span className="relative flex h-3 w-3">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-500 opacity-75" />
                <span className="relative inline-flex h-3 w-3 rounded-full bg-red-600" />
              </span>
              <span className="font-semibold uppercase tracking-wider text-red-400">
                Live · {join.concurrent} listening
              </span>
            </div>
            <h1 className="text-4xl font-bold">{join.session.title}</h1>
            <p className="mt-2 text-zinc-400">
              {join.session.creator_name || join.session.handle}
            </p>
            {join.session.description && (
              <p className="mt-4 max-w-2xl leading-7 text-zinc-300">
                {join.session.description}
              </p>
            )}

            {/* Audio visualizer card */}
            <AudioStage />

            <ReactionBar sessionId={id} />
          </section>

          <ChatPanel sessionId={id} />
        </div>
      </LiveKitRoom>
    </main>
  );
}

function AudioStage() {
  const tracks = useTracks(
    [{ source: Track.Source.Microphone, withPlaceholder: false }],
    { onlySubscribed: true }
  );

  return (
    <div className="mt-8 rounded-2xl border border-zinc-800 bg-gradient-to-b from-zinc-900 to-zinc-950 p-8 text-center">
      <div className="mb-6 flex items-center justify-center gap-1">
        {[0, 1, 2, 3, 4, 5, 6, 7].map((i) => (
          <span
            key={i}
            className="w-1.5 animate-pulse rounded-full bg-red-500"
            style={{
              height: `${12 + ((i * 13) % 28)}px`,
              animationDelay: `${i * 120}ms`,
              animationDuration: "900ms",
            }}
          />
        ))}
      </div>
      <p className="text-sm text-zinc-400">
        {tracks.length > 0
          ? "🎙 Live audio streaming"
          : "Waiting for the creator's microphone…"}
      </p>
    </div>
  );
}

function ReactionBar({ sessionId }: { sessionId: string }) {
  const [flying, setFlying] = useState<string[]>([]);

  const send = (reaction: string) => {
    api(`/live/${sessionId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ reaction }),
    }).catch(() => {});
    setFlying((f) => [...f, reaction]);
    setTimeout(() => setFlying((f) => f.slice(1)), 1800);
  };

  return (
    <div className="mt-6 flex gap-2">
      {REACTIONS.map((r) => (
        <button
          key={r.id}
          onClick={() => send(r.id)}
          className="rounded-full border border-zinc-800 bg-zinc-900 px-4 py-2 text-xl transition hover:scale-110 hover:border-red-500/50"
          aria-label={`react ${r.id}`}
        >
          {r.emoji}
        </button>
      ))}
      <div className="pointer-events-none relative">
        {flying.map((f, i) => (
          <span
            key={i}
            className="absolute bottom-0 left-4 animate-bounce text-2xl"
          >
            {REACTIONS.find((r) => r.id === f)?.emoji}
          </span>
        ))}
      </div>
    </div>
  );
}

function ChatPanel({ sessionId }: { sessionId: string }) {
  const [messages, setMessages] = useState<
    { user_id: string; username: string; body: string; ts?: number }[]
  >([]);
  const [draft, setDraft] = useState("");
  const [ws, setWs] = useState<WebSocket | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const wsPath = process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8082";
    const socket = new WebSocket(
      `${wsPath}/ws/live/${sessionId}?access_token=${encodeURIComponent(
        getAccessToken() ?? ""
      )}`
    );
    setWs(socket);

    socket.onmessage = (ev) => {
      try {
        const env = JSON.parse(ev.data as string);
        if (env.type === "chat") {
          setMessages((m) => [...m.slice(-199), env.payload]);
        }
        if (env.type === "reaction") {
          setMessages((m) => [
            ...m.slice(-199),
            {
              user_id: "system",
              username: "",
              body: `reacted ${env.payload.reaction}`,
              ts: Date.now(),
            },
          ]);
        }
      } catch {
        // ignore malformed frames
      }
    };

    return () => socket.close();
  }, [sessionId]);

  useEffect(() => {
    listRef.current?.scrollTo({
      top: listRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [messages]);

  const send = () => {
    const body = draft.trim();
    if (!body || !ws) return;
    ws.send(
      JSON.stringify({
        type: "chat",
        payload: { body },
      })
    );
    setDraft("");
  };

  return (
    <aside className="flex h-[80vh] flex-col rounded-2xl border border-zinc-800 bg-zinc-900">
      <div className="border-b border-zinc-800 px-4 py-3 font-medium">
        Live chat
      </div>
      <div ref={listRef} className="flex-1 space-y-3 overflow-y-auto p-4">
        {messages.length === 0 && (
          <p className="text-sm text-zinc-500">
            Say something nice — messages are real-time.
          </p>
        )}
        {messages.map((m, i) => (
          <div key={i} className="text-sm">
            <span className="font-medium text-red-300">{m.username}</span>{" "}
            <span className="text-zinc-300">{m.body}</span>
          </div>
        ))}
      </div>
      <div className="flex gap-2 border-t border-zinc-800 p-3">
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && send()}
          placeholder="Send a message"
          maxLength={500}
          className="flex-1 rounded-lg bg-zinc-950 px-3 py-2 text-sm outline-none ring-zinc-700 focus:ring-2"
        />
        <button
          onClick={send}
          className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium hover:bg-red-500"
        >
          Send
        </button>
      </div>
    </aside>
  );
}
