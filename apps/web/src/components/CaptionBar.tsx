"use client";

import { useMemo, useState } from "react";
import { useCaptions } from "@/lib/use-captions";
import { useTranslatedAudio } from "@/lib/use-translated-audio";

const LANG_LABELS: Record<string, string> = {
  src: "Original",
  en: "English", es: "Español", fr: "Français", de: "Deutsch",
  hi: "हिन्दी", pt: "Português", zh: "中文", ja: "日本語",
  ko: "한국어", it: "Italiano", ru: "Русский", ar: "العربية",
};

/**
 * Listener-facing live captions + translated audio: a language picker, the
 * caption bar (latest final + accumulating partial), and an opt-in 🔊 toggle
 * that plays the translated SPEECH for the chosen language in realtime.
 * Deltas arrive while the creator is still speaking.
 */
export default function CaptionBar({ sessionId }: { sessionId: string }) {
  const { captions, partials, connected } = useCaptions(sessionId);
  const [lang, setLang] = useState("src");
  // Translated audio only exists for target languages (not "src").
  const { active, setActive, level } = useTranslatedAudio(
    sessionId,
    lang !== "src" ? lang : null
  );

  const availableLangs = useMemo(() => {
    const set = new Set<string>(["src"]);
    for (const c of captions) set.add(c.lang);
    for (const k of Object.keys(partials)) set.add(k);
    return [...set];
  }, [captions, partials]);

  const finalLine = [...captions].reverse().find((c) => c.lang === lang);
  const partial = partials[lang] ?? "";

  return (
    <div className="mt-6 rounded-2xl border border-zinc-800 bg-zinc-900/80 p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
          Live captions
          {connected ? "" : " (connecting…)"}
        </span>
        <div className="flex items-center gap-2">
          {availableLangs.length > 1 && (
            <select
              value={lang}
              onChange={(e) => setLang(e.target.value)}
              className="rounded-lg bg-zinc-950 px-2 py-1 text-xs text-zinc-300 outline-none ring-zinc-700"
            >
              {availableLangs.map((l) => (
                <option key={l} value={l}>
                  {LANG_LABELS[l] ?? l}
                </option>
              ))}
            </select>
          )}
          {lang !== "src" && (
            <button
              onClick={() => setActive((v) => !v)}
              className={`rounded-full px-3 py-1 text-xs font-medium transition ${
                active
                  ? "bg-red-600 text-white"
                  : "border border-zinc-700 text-zinc-300 hover:border-zinc-500"
              }`}
              aria-pressed={active}
              title="Play the translated voice in realtime"
            >
              🔊 {active ? "Voice on" : "Voice off"}
            </button>
          )}
        </div>
      </div>

      {/* Live level meter while translated voice plays */}
      {active && (
        <div className="mb-2 flex items-center gap-2">
          <div className="h-1 flex-1 overflow-hidden rounded-full bg-zinc-800">
            <div
              className="h-full rounded-full bg-red-500 transition-[width] duration-100"
              style={{ width: `${Math.min(100, level * 140)}%` }}
            />
          </div>
          <span className="text-[0.65rem] text-zinc-500">
            hearing {LANG_LABELS[lang] ?? lang}
          </span>
        </div>
      )}

      <p className="min-h-[3rem] text-lg leading-snug text-zinc-100">
        {partial || finalLine?.text || (
          <span className="text-sm text-zinc-500">
            Captions appear here the moment the creator speaks.
          </span>
        )}
      </p>
    </div>
  );
}
