"use client";

/**
 * Moderator queue (Phase 5 completion): work reports left by listeners.
 * MODERATOR/ADMIN only server-side — this page surfaces the API's 403s
 * as a sign-in hint.
 */

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { api, getAccessToken, routes } from "@/lib/api";

interface Report {
  id: string;
  reporter_id: string;
  audio_id?: string;
  session_id?: string;
  reason: string;
  details: string;
  status: "PENDING" | "RESOLVED" | "DISMISSED";
  resolved_by?: string;
  resolved_at?: string;
  created_at: string;
}

type QueueTab = "PENDING" | "RESOLVED" | "DISMISSED";

const TABS: [QueueTab, string][] = [
  ["PENDING", "Pending"],
  ["RESOLVED", "Resolved"],
  ["DISMISSED", "Dismissed"],
];

export default function ReportsQueuePage() {
  const [tab, setTab] = useState<QueueTab>("PENDING");
  const [reports, setReports] = useState<Report[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [authed, setAuthed] = useState(false);

  useEffect(() => setAuthed(!!getAccessToken()), []);

  const load = useCallback((t: QueueTab) => {
    setReports(null);
    setError(null);
    api<{ data: Report[] }>(`${routes.reports}?status=${t}`)
      .then((r) => setReports(r.data ?? []))
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(() => {
    if (authed) load(tab);
  }, [tab, authed, load]);

  const close = async (id: string, action: "resolve" | "dismiss") => {
    await api(
      action === "resolve" ? routes.reportResolve(id) : routes.reportDismiss(id),
      { method: "POST" }
    ).catch((e: Error) => setError(e.message));
    load(tab);
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-10 text-zinc-50">
      <div className="mx-auto max-w-4xl">
        <h1 className="text-3xl font-bold">⚑ Moderation queue</h1>
        <p className="mt-1 text-zinc-400">
          Reports filed by listeners — resolve or dismiss each one.
        </p>

        {!authed && (
          <p className="mt-6 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
            Sign in with a moderator account to view the queue.
          </p>
        )}

        {authed && (
          <>
            <div className="mb-6 mt-6 flex gap-2 text-sm">
              {TABS.map(([key, label]) => (
                <button
                  key={key}
                  onClick={() => setTab(key)}
                  className={`rounded-full px-4 py-1.5 ${
                    tab === key
                      ? "bg-zinc-100 text-zinc-900"
                      : "bg-zinc-900 text-zinc-400 hover:text-zinc-100"
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>

            {error && (
              <p className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-red-300">
                {error}
              </p>
            )}

            {reports && reports.length === 0 && (
              <p className="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center text-zinc-400">
                Nothing in this queue.
              </p>
            )}

            <ul className="space-y-3">
              {reports?.map((r) => (
                <li
                  key={r.id}
                  className="rounded-xl border border-zinc-800 bg-zinc-900 p-5"
                >
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="rounded-full bg-red-950/60 px-2.5 py-0.5 text-xs font-medium text-red-300">
                      {r.reason}
                    </span>
                    {r.audio_id ? (
                      <Link
                        href={`/audio/${r.audio_id}`}
                        className="text-xs text-zinc-400 underline hover:text-zinc-200"
                      >
                        episode {r.audio_id.slice(0, 8)}…
                      </Link>
                    ) : (
                      <Link
                        href={`/live/${r.session_id}`}
                        className="text-xs text-zinc-400 underline hover:text-zinc-200"
                      >
                        live session {r.session_id?.slice(0, 8)}…
                      </Link>
                    )}
                    <span className="ml-auto text-xs text-zinc-600">
                      {new Date(r.created_at).toLocaleString()}
                    </span>
                  </div>
                  {r.details && (
                    <p className="mt-2 text-sm leading-6 text-zinc-300">
                      {r.details}
                    </p>
                  )}
                  {r.status === "PENDING" ? (
                    <div className="mt-3 flex gap-2">
                      <button
                        onClick={() => close(r.id, "resolve")}
                        className="rounded-full bg-green-700 px-4 py-1.5 text-xs font-medium hover:bg-green-600"
                      >
                        Resolve
                      </button>
                      <button
                        onClick={() => close(r.id, "dismiss")}
                        className="rounded-full border border-zinc-700 px-4 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800"
                      >
                        Dismiss
                      </button>
                    </div>
                  ) : (
                    <p className="mt-2 text-xs text-zinc-500">
                      {r.status.toLowerCase()}{" "}
                      {r.resolved_at && `· ${new Date(r.resolved_at).toLocaleString()}`}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </main>
  );
}
