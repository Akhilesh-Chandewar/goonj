"use client";

import { useEffect, useRef, useState } from "react";
import { api, getAccessToken } from "@/lib/api";

interface TranslationConfig {
  session_id: string;
  source_lang: string;
  target_langs: string[];
  enabled: boolean;
}

interface MintResponse {
  client_secret: string;
  model: string;
  webrtc_url: string;
  events_channel: string;
  target_lang: string;
}

const LANGS: Record<string, string> = {
  en: "English", es: "Español", fr: "Français", de: "Deutsch",
  hi: "हिन्दी", pt: "Português", zh: "中文", ja: "日本語",
  ko: "한국어", it: "Italiano", ru: "Русский", ar: "العربية",
};

/**
 * Creator-side live translation. Flow:
 *  1. pick target languages → PUT /live/{id}/translate/config
 *  2. mint an ephemeral OpenAI session per language → POST .../translate/session
 *  3. browser opens a WebRTC peer connection DIRECTLY to OpenAI (mic audio
 *     never touches Goonj servers — that is the latency win)
 *  4. caption deltas arriving on the oai-events data channel are relayed to
 *     POST .../translate/captions, which fans out to every listener via the
 *     existing room channel.
 */
export default function TranslationStudio({ sessionId }: { sessionId: string }) {
  const [langs, setLangs] = useState<string[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [running, setRunning] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const pcsRef = useRef<RTCPeerConnection[]>([]);
  const sessionRef = useRef<string | null>(null);

  // Fetch current config on mount.
  useEffect(() => {
    api<TranslationConfig>(`/live/${sessionId}/translate/config`)
      .then((c) => {
        setLangs(c.target_langs ?? []);
        setEnabled(c.enabled);
      })
      .catch(() => {});
  }, [sessionId]);

  // Tear down peer connections on unmount.
  useEffect(() => {
    const pcs = pcsRef.current;
    return () => pcs.forEach((pc) => pc.close());
  }, []);

  const toggleLang = (l: string) => {
    setLangs((cur) =>
      cur.includes(l) ? cur.filter((x) => x !== l) : [...cur, l].slice(0, 3)
    );
  };

  const saveConfig = async () => {
    setStatus(null);
    try {
      await api(`/live/${sessionId}/translate/config`, {
        method: "PUT",
        body: JSON.stringify({
          enabled: enabled && langs.length > 0,
          target_langs: langs,
        }),
      });
      setStatus(enabled && langs.length > 0 ? "Translation on" : "Translation off");
    } catch (e) {
      setStatus((e as Error).message);
    }
  };

  const start = async () => {
    setStatus(null);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      for (const lang of langs) {
        const mint = await api<MintResponse>(`/live/${sessionId}/translate/session`, {
          method: "POST",
          body: JSON.stringify({ target_lang: lang }),
        });
        await openSession(sessionId, mint, stream);
      }
      setRunning(true);
      setStatus(`Translating to ${langs.length} language(s)`);
    } catch (e) {
      setStatus((e as Error).message);
    }
  };

  const stop = () => {
    pcsRef.current.forEach((pc) => pc.close());
    pcsRef.current = [];
    setRunning(false);
    setStatus("Translation stopped");
  };

  /** Opens one browser↔OpenAI WebRTC translation session and relays captions. */
  async function openSession(sessionId: string, mint: MintResponse, stream: MediaStream) {
    const pc = new RTCPeerConnection();
    pcsRef.current.push(pc);

    // Send the creator's mic as the source track.
    stream.getAudioTracks().forEach((t) => pc.addTrack(t, stream));

    // Caption events arrive on the oai-events data channel.
    const events = pc.createDataChannel(mint.events_channel);
    events.onmessage = (ev) => {
      try {
        const e = JSON.parse(ev.data as string);
        const delta =
          e.type === "session.output_transcript.delta" ? e.delta :
          e.type === "session.output_transcript.done" ? e.text : null;
        if (delta == null) return;
        api(`/live/${sessionId}/translate/captions`, {
          method: "POST",
          body: JSON.stringify({
            lang: mint.target_lang,
            text: delta,
            final: e.type === "session.output_transcript.done",
          }),
        }).catch(() => {});
      } catch {
        // ignore malformed events
      }
    };

    // SDP exchange with OpenAI using the ephemeral secret.
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    const res = await fetch(mint.webrtc_url, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${mint.client_secret}`,
        "Content-Type": "application/sdp",
      },
      body: offer.sdp ?? "",
    });
    if (!res.ok) throw new Error(`OpenAI SDP exchange failed (${res.status})`);
    await pc.setRemoteDescription({ type: "answer", sdp: await res.text() });
  }

  return (
    <div className="mt-6 rounded-2xl border border-zinc-800 bg-zinc-900/80 p-4">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
          Live translation
        </span>
        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="h-4 w-4 accent-red-600"
          />
          Enabled
        </label>
      </div>

      <div className="flex flex-wrap gap-2">
        {Object.entries(LANGS).map(([code, label]) => (
          <button
            key={code}
            onClick={() => toggleLang(code)}
            className={`rounded-full border px-3 py-1 text-xs transition ${
              langs.includes(code)
                ? "border-red-500 bg-red-500/10 text-red-300"
                : "border-zinc-700 text-zinc-400 hover:border-zinc-500"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="mt-3 flex items-center gap-2">
        {!running ? (
          <button
            onClick={start}
            disabled={!enabled || langs.length === 0}
            className="rounded-full bg-red-600 px-4 py-2 text-sm font-medium hover:bg-red-500 disabled:opacity-40"
          >
            Start translating
          </button>
        ) : (
          <button
            onClick={stop}
            className="rounded-full border border-zinc-700 px-4 py-2 text-sm text-zinc-300"
          >
            Stop
          </button>
        )}
        <button
          onClick={saveConfig}
          className="rounded-full border border-zinc-700 px-4 py-2 text-sm text-zinc-300"
        >
          Save
        </button>
      </div>
      {status && <p className="mt-2 text-xs text-zinc-400">{status}</p>}
    </div>
  );
}
