"use client";

import { useEffect, useState } from "react";
import { getAccessToken } from "@/lib/api";

export interface Caption {
  session_id: string;
  lang: string;
  text: string;
  final: boolean;
  start_ms?: number;
  end_ms?: number;
  ts?: number;
}

/**
 * Subscribes to the room's caption stream over the existing ws-gateway
 * channel. Keeps the latest partial line per language plus a rolling list of
 * final lines so the UI can render a live caption bar.
 */
export function useCaptions(sessionId: string) {
  const [captions, setCaptions] = useState<Caption[]>([]);
  const [partials, setPartials] = useState<Record<string, string>>({});
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const wsPath = process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8082";
    const socket = new WebSocket(
      `${wsPath}/ws/live/${sessionId}?access_token=${encodeURIComponent(
        getAccessToken() ?? ""
      )}`
    );
    socket.onopen = () => setConnected(true);
    socket.onclose = () => setConnected(false);
    socket.onmessage = (ev) => {
      try {
        const env = JSON.parse(ev.data as string);
        if (env.type !== "caption") return;
        const c = env.payload as Caption;
        if (c.final) {
          setCaptions((prev) => [
            ...prev.slice(-59),
            c,
          ]);
          setPartials((p) => {
            const next = { ...p };
            delete next[c.lang];
            return next;
          });
        } else {
          // Accumulate deltas per language; the last partial wins visually.
          setPartials((p) => ({
            ...p,
            [c.lang]: (p[c.lang] ?? "") + c.text,
          }));
        }
      } catch {
        // ignore malformed frames
      }
    };
    return () => socket.close();
  }, [sessionId]);

  return { captions, partials, connected };
}
