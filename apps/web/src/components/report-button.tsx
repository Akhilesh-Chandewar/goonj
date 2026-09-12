"use client";

/**
 * Report/flag buttons (Phase 5 completion): listeners flag audio or live
 * sessions for moderation. One open report per (reporter, target) is
 * accepted server-side; duplicates collapse silently.
 */

import { useState } from "react";
import { api, getAccessToken, routes } from "@/lib/api";

const REASONS = [
  "spam",
  "harassment",
  "copyright",
  "explicit",
  "misinformation",
  "other",
] as const;

type Reason = (typeof REASONS)[number];

export function ReportButton({
  audioId,
  sessionId,
  label = "⚑ Report",
}: {
  audioId?: string;
  sessionId?: string;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState<Reason>("spam");
  const [details, setDetails] = useState("");
  const [state, setState] = useState<"idle" | "sending" | "sent" | "error">("idle");
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setState("sending");
    setError(null);
    try {
      if (!getAccessToken()) throw new Error("Sign in to report content");
      await api(routes.reports, {
        method: "POST",
        body: JSON.stringify({ audio_id: audioId, session_id: sessionId, reason, details }),
      });
      setState("sent");
      setTimeout(() => {
        setOpen(false);
        setState("idle");
        setDetails("");
      }, 1600);
    } catch (e) {
      setError((e as Error).message);
      setState("error");
    }
  };

  return (
    <span className="relative inline-block">
      <button
        onClick={() => setOpen((v) => !v)}
        className="text-xs text-zinc-500 transition hover:text-red-400"
        aria-label="Report content"
      >
        {label}
      </button>

      {open && (
        <div className="absolute bottom-full right-0 z-50 mb-2 w-72 rounded-xl border border-zinc-700 bg-zinc-900 p-4 shadow-xl">
          {state === "sent" ? (
            <p className="text-sm text-green-400">
              Report sent — a moderator will take a look.
            </p>
          ) : (
            <>
              <p className="mb-2 text-sm font-medium text-zinc-200">
                Why are you reporting this?
              </p>
              <div className="mb-3 flex flex-wrap gap-1.5">
                {REASONS.map((r) => (
                  <button
                    key={r}
                    onClick={() => setReason(r)}
                    className={`rounded-full px-2.5 py-1 text-xs ${
                      reason === r
                        ? "bg-red-600 text-white"
                        : "bg-zinc-800 text-zinc-300 hover:bg-zinc-700"
                    }`}
                  >
                    {r}
                  </button>
                ))}
              </div>
              <textarea
                value={details}
                onChange={(e) => setDetails(e.target.value)}
                rows={2}
                maxLength={1000}
                placeholder="Details (optional)"
                className="w-full rounded-lg bg-zinc-950 px-2.5 py-1.5 text-xs outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
              />
              {error && <p className="mt-2 text-xs text-red-400">{error}</p>}
              <div className="mt-3 flex justify-end gap-2">
                <button
                  onClick={() => setOpen(false)}
                  className="rounded px-2.5 py-1 text-xs text-zinc-400 hover:text-zinc-200"
                >
                  Cancel
                </button>
                <button
                  onClick={submit}
                  disabled={state === "sending"}
                  className="rounded bg-red-600 px-3 py-1 text-xs font-medium text-white hover:bg-red-500 disabled:opacity-40"
                >
                  {state === "sending" ? "Sending…" : "Send report"}
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </span>
  );
}
