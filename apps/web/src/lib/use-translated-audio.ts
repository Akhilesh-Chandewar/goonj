"use client";

/**
 * useTranslatedAudio — listener-side realtime translated speech.
 *
 * Connects to ws-gateway's /ws/translate/{session}/{lang} socket, which
 * streams OpenAI-translated speech as binary PCM16 frames (24kHz mono).
 * Frames are queued into an AudioContext and played back seamlessly.
 *
 * Design notes:
 *  - Playback is opt-in per language (the listener picks a target in the
 *    caption bar); the socket is opened only when active, so idle listeners
 *    cost nothing.
 *  - Scheduling uses AudioContext currentTime for gapless chaining; the
 *    queue is trimmed so latency stays under ~600ms. If the network hiccups,
 *    we drop ahead rather than grow the buffer (realtime-first, matching the
 *    translator's drop-oldest pump semantics).
 *  - PCM16 → Float32 conversion happens per chunk on the main thread; at
 *    24kHz mono this is trivially cheap.
 */

import { useEffect, useRef, useState } from "react";
import { getAccessToken } from "@/lib/api";

const SAMPLE_RATE = 24000;
const MAX_QUEUE_SECONDS = 0.6;

export function useTranslatedAudio(sessionId: string, lang: string | null) {
  const [active, setActive] = useState(false);
  const [level, setLevel] = useState(0); // 0..1 meter for UI feedback
  const ctxRef = useRef<AudioContext | null>(null);
  const queueRef = useRef<{ buf: AudioBuffer; at: number }[]>([]);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!lang || !active) {
      // Tear down when the listener switches off / changes language.
      wsRef.current?.close();
      wsRef.current = null;
      queueRef.current = [];
      ctxRef.current?.close().catch(() => {});
      ctxRef.current = null;
      setLevel(0);
      return;
    }

    const ctx = new AudioContext({ sampleRate: SAMPLE_RATE });
    ctxRef.current = ctx;
    const gain = ctx.createGain();
    gain.gain.value = 0.9;
    gain.connect(ctx.destination);

    const wsPath = process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8082";
    const ws = new WebSocket(
      `${wsPath}/ws/translate/${sessionId}/${lang}?access_token=${encodeURIComponent(
        getAccessToken() ?? ""
      )}`
    );
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    let keepalive: ReturnType<typeof setInterval> | null = null;

    ws.onopen = () => {
      keepalive = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "ping" }));
      }, 15000);
    };

    ws.onmessage = (ev) => {
      if (!(ev.data instanceof ArrayBuffer)) return; // pong / control text
      const pcm16 = new Int16Array(ev.data);
      if (pcm16.length === 0) return;

      // PCM16 → Float32 in [-1, 1].
      const f32 = new Float32Array(pcm16.length);
      let peak = 0;
      for (let i = 0; i < pcm16.length; i++) {
        f32[i] = pcm16[i] / 32768;
        const a = Math.abs(f32[i]);
        if (a > peak) peak = a;
      }
      setLevel(peak);

      const buf = ctx.createBuffer(1, f32.length, SAMPLE_RATE);
      buf.copyToChannel(f32, 0);

      // Schedule gapless: next start = max(now, end of last queued chunk),
      // trimming the queue when it grows past the latency budget.
      const now = ctx.currentTime;
      let at = now + 0.05;
      const q = queueRef.current;
      while (q.length && q[0].at + q[0].buf.duration < now) q.shift();
      if (q.length) at = Math.max(at, q[q.length - 1].at + q[q.length - 1].buf.duration);
      // Drop our own queued tail if latency budget is blown (stay realtime).
      while (q.length && at - now > MAX_QUEUE_SECONDS) q.shift();
      if (q.length) at = Math.max(at, q[q.length - 1].at + q[q.length - 1].buf.duration);

      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(gain);
      src.start(at);
      q.push({ buf, at });
    };

    ws.onclose = () => {
      if (keepalive) clearInterval(keepalive);
    };

    return () => {
      if (keepalive) clearInterval(keepalive);
      ws.close();
      ctx.close().catch(() => {});
      if (wsRef.current === ws) wsRef.current = null;
      if (ctxRef.current === ctx) ctxRef.current = null;
    };
  }, [sessionId, lang, active]);

  return { active, setActive, level };
}
