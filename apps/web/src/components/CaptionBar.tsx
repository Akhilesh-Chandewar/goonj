"use client";

import { useMemo, useState } from "react";
import { useCaptions } from "@/lib/use-captions";

const LANG_LABELS: Record<string, string> = {
  src: "Original",
  en: "English", es: "Español", fr: "Français", de: "Deutsch",
  hi: "हिन्दी", pt: "Português", zh: "中文", ja: "日本語",
  ko: "한국어", it: "Italiano", ru: "Русский", ar: "العربية",
};

/**
 * Listener-facing live captions: a language picker plus the caption bar.
 * Shows the latest final line and the accumulating partial for the chosen
 * language. Deltas arrive while the creator is still speaking.
 */
export default function CaptionBar({ sessionId }: { sessionId: string }) {
  const { captions, partials, connected } = useCaptions(sessionId);
  const [lang, setLang] = useState("src");

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
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
          Live captions
          {connected ? "" : " (connecting…)"}
        </span>
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
      </div>
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
