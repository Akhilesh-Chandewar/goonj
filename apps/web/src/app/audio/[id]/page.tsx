"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { getAccessToken } from "@/lib/api";
import { routes } from "@/lib/api";
import { usePlayer, type Track } from "@/stores/player";
import { useFollow, useLike, usePlaylists } from "@/lib/engagement";
import { ReportButton } from "@/components/report-button";
import type {
  AudioItem,
  AudioSource,
  CommentItem,
  CreatorPage,
} from "@/lib/live-types";

interface PlaybackResponse {
  audio: AudioItem;
  sources: AudioSource[];
  waveform?: number[];
}

interface TranscriptChunk {
  idx: number;
  start_ms: number;
  end_ms: number;
  speaker?: string;
  text: string;
}

interface TranscriptPayload {
  audio_id: string;
  chunks: TranscriptChunk[];
  summary?: { summary: string; model: string; updated_at: string };
}

const fmtTime = (ms: number) => {
  const total = Math.floor(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
};

export default function AudioPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [pb, setPb] = useState<PlaybackResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const playTrack = usePlayer((s) => s.playTrack);

  const [comments, setComments] = useState<CommentItem[]>([]);
  const [replies, setReplies] = useState<CommentItem[]>([]);
  const [draft, setDraft] = useState("");
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [playlistOpen, setPlaylistOpen] = useState(false);
  const [transcript, setTranscript] = useState<TranscriptPayload | null>(null);
  const [showTranscript, setShowTranscript] = useState(false);

  const { state: likeState, toggle: toggleLike } = useLike(id);
  const { playlists, addTo, create } = usePlaylists();

  useEffect(() => {
    api<PlaybackResponse>(`/audio/${id}/playback`)
      .then(setPb)
      .catch((e: Error) => setError(e.message));
    loadComments();
    // Transcript + AI summary are optional (produced by the AI worker).
    api<TranscriptPayload>(routes.audioTranscript(id))
      .then(setTranscript)
      .catch(() => setTranscript(null));
  }, [id]);

  const loadComments = () => {
    api<{ data: { comments: CommentItem[]; replies: CommentItem[] } }>(
      routes.audioComments(id)
    )
      .then((r) => {
        setComments(r.data?.comments ?? []);
        setReplies(r.data?.replies ?? []);
      })
      .catch(() => {});
  };

  if (error) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-300">
        <p className="rounded-lg border border-red-500/40 bg-red-500/10 px-6 py-4 text-red-300">
          {error}
        </p>
      </main>
    );
  }
  if (!pb) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Loading…
      </main>
    );
  }

  const a = pb.audio;
  const track: Track = {
    id: a.id,
    title: a.title,
    creator: a.creator_name || a.handle || "Unknown",
    durationMs: a.duration_ms,
    sources: pb.sources,
  };

  const guard = async (fn: () => Promise<void>) => {
    setActionError(null);
    try {
      await fn();
    } catch (e) {
      setActionError((e as Error).message);
    }
  };

  const submitComment = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!draft.trim()) return;
    await guard(async () => {
      if (!getAccessToken()) throw new Error("Sign in to comment");
      await api(routes.audioComments(id), {
        method: "POST",
        body: JSON.stringify({ body: draft, parent_id: replyTo }),
      });
      setDraft("");
      setReplyTo(null);
      loadComments();
    });
  };

  const deleteComment = async (commentId: string) => {
    await guard(async () => {
      await api(routes.comment(commentId), { method: "DELETE" });
      loadComments();
    });
  };

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-12 text-zinc-50">
      <div className="mx-auto max-w-3xl">
        <div className="rounded-3xl border border-zinc-800 bg-gradient-to-b from-zinc-900 to-zinc-950 p-10 text-center">
          <div className="mx-auto mb-6 flex h-40 w-40 items-center justify-center rounded-2xl bg-zinc-800 text-6xl">
            🎙
          </div>
          <h1 className="text-3xl font-bold">{a.title}</h1>
          <p className="mt-2 text-zinc-400">
            {a.creator_name || a.handle} · {Math.round((a.duration_ms || 0) / 60000)} min · {a.category}
            <span className="mx-2 text-zinc-700">|</span>
            <ReportButton audioId={a.id} />
          </p>

          {pb.waveform && pb.waveform.length > 0 && (
            <div className="mt-8 flex h-16 items-center justify-center gap-[2px]">
              {pb.waveform
                .filter((_, i) => i % 4 === 0)
                .map((amp, i) => (
                  <span
                    key={i}
                    className="w-1 rounded-full bg-red-500/80"
                    style={{ height: `${Math.max(amp, 4)}%` }}
                  />
                ))}
            </div>
          )}

          <div className="mt-8 flex items-center justify-center gap-3">
            <button
              onClick={() => playTrack(track)}
              className="rounded-full bg-red-600 px-10 py-3 text-lg font-semibold hover:bg-red-500"
            >
              ▶ Play
            </button>
            <button
              onClick={() => guard(toggleLike)}
              className={`rounded-full border px-5 py-3 text-sm ${
                likeState?.liked
                  ? "border-red-500 bg-red-500/20 text-red-300"
                  : "border-zinc-700 text-zinc-300 hover:border-zinc-500"
              }`}
              aria-pressed={likeState?.liked}
            >
              {likeState?.liked ? "♥" : "♡"} {likeState?.likes ?? 0}
            </button>
            <button
              onClick={() => setPlaylistOpen((v) => !v)}
              className="rounded-full border border-zinc-700 px-5 py-3 text-sm text-zinc-300 hover:border-zinc-500"
            >
              + Playlist
            </button>
          </div>
          {actionError && (
            <p className="mt-3 text-sm text-red-400">{actionError}</p>
          )}

          {playlistOpen && (
            <div className="mx-auto mt-4 max-w-sm rounded-xl border border-zinc-800 bg-zinc-900 p-4 text-left text-sm">
              {(playlists ?? []).length === 0 && (
                <p className="mb-3 text-zinc-400">No playlists yet.</p>
              )}
              {(playlists ?? []).map((pl) => (
                <button
                  key={pl.id}
                  onClick={() =>
                    guard(async () => {
                      await addTo(pl.id, id);
                      setPlaylistOpen(false);
                    })
                  }
                  className="block w-full rounded px-2 py-1.5 text-left hover:bg-zinc-800"
                >
                  {pl.title}{" "}
                  <span className="text-zinc-500">({pl.item_count})</span>
                </button>
              ))}
              <NewPlaylistInline onCreate={(title) => create(title, id)} />
            </div>
          )}
        </div>

        {a.description && (
          <p className="mt-8 leading-7 text-zinc-300">{a.description}</p>
        )}

        {/* AI summary + transcript (Phase 5) */}
        {transcript && (
          <section className="mt-8 rounded-2xl border border-zinc-800 bg-zinc-900 p-6">
            {transcript.summary && (
              <div className="mb-4 rounded-xl border border-purple-500/30 bg-purple-500/10 p-4">
                <p className="text-xs font-semibold uppercase tracking-wide text-purple-300">
                  ✨ AI Summary
                </p>
                <p className="mt-1.5 text-sm leading-6 text-zinc-200">
                  {transcript.summary.summary}
                </p>
              </div>
            )}

            <button
              onClick={() => setShowTranscript((v) => !v)}
              className="flex w-full items-center justify-between text-sm font-medium text-zinc-300 hover:text-zinc-100"
            >
              <span>Transcript ({transcript.chunks.length} segments)</span>
              <span>{showTranscript ? "▲" : "▼"}</span>
            </button>
            {showTranscript && (
              <div className="mt-4 max-h-96 space-y-3 overflow-y-auto pr-2">
                {transcript.chunks.map((c) => (
                  <div key={c.idx} className="flex gap-3 text-sm">
                    <span className="shrink-0 font-mono text-xs text-red-400/80">
                      {fmtTime(c.start_ms)}
                    </span>
                    <p className="leading-6 text-zinc-300">
                      {c.speaker && (
                        <span className="mr-1.5 font-medium text-zinc-400">
                          {c.speaker}:
                        </span>
                      )}
                      {c.text}
                    </p>
                  </div>
                ))}
              </div>
            )}
          </section>
        )}

        {/* Comments */}
        <section className="mt-12">
          <h2 className="mb-4 text-lg font-semibold">
            Comments {comments.length > 0 && `(${comments.length})`}
          </h2>
          <form onSubmit={submitComment} className="mb-6 flex gap-2">
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={replyTo ? "Reply…" : "Add a comment…"}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-900 px-4 py-2 text-sm outline-none focus:border-red-500"
            />
            {replyTo && (
              <button
                type="button"
                onClick={() => setReplyTo(null)}
                className="rounded-lg border border-zinc-700 px-3 text-sm text-zinc-400"
              >
                Cancel
              </button>
            )}
            <button
              type="submit"
              className="rounded-lg bg-red-600 px-4 text-sm font-medium hover:bg-red-500"
            >
              Post
            </button>
          </form>

          <ul className="space-y-4">
            {comments.map((c) => (
              <li key={c.id} className="rounded-xl border border-zinc-800 bg-zinc-900 p-4">
                <div className="flex items-center justify-between text-sm">
                  <span className="font-medium text-zinc-200">{c.username}</span>
                  <div className="flex items-center gap-3 text-xs text-zinc-500">
                    <button
                      onClick={() =>
                        guard(async () => {
                          if (!getAccessToken()) throw new Error("Sign in to react");
                          await api(routes.commentLike(c.id), { method: "POST" });
                          loadComments();
                        })
                      }
                      className="hover:text-zinc-200"
                    >
                      ♡ {c.like_count}
                    </button>
                    <button
                      onClick={() => setReplyTo(c.id)}
                      className="hover:text-zinc-200"
                    >
                      Reply
                    </button>
                    {c.mine && (
                      <button
                        onClick={() => deleteComment(c.id)}
                        className="hover:text-red-400"
                      >
                        Delete
                      </button>
                    )}
                  </div>
                </div>
                <p className="mt-2 text-sm leading-6 text-zinc-300">{c.body}</p>

                {replies
                  .filter((r) => r.parent_id === c.id)
                  .map((r) => (
                    <div
                      key={r.id}
                      className="mt-3 rounded-lg border-l-2 border-zinc-700 pl-3"
                    >
                      <div className="flex items-center justify-between text-xs">
                        <span className="font-medium text-zinc-300">{r.username}</span>
                        <div className="flex gap-3 text-zinc-500">
                          {r.mine && (
                            <button
                              onClick={() => deleteComment(r.id)}
                              className="hover:text-red-400"
                            >
                              Delete
                            </button>
                          )}
                        </div>
                      </div>
                      <p className="mt-1 text-sm text-zinc-400">{r.body}</p>
                    </div>
                  ))}
              </li>
            ))}
          </ul>
          {comments.length === 0 && (
            <p className="text-sm text-zinc-500">Be the first to comment.</p>
          )}
        </section>

        <CreatorFooter creatorId={a.creator_id} />
      </div>
    </main>
  );
}

function NewPlaylistInline({ onCreate }: { onCreate: (title: string) => Promise<unknown> }) {
  const [title, setTitle] = useState("");
  return (
    <form
      className="mt-3 flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (!title.trim()) return;
        void onCreate(title).then(() => setTitle(""));
      }}
    >
      <input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="New playlist…"
        className="w-full rounded border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-xs outline-none focus:border-red-500"
      />
      <button className="rounded bg-zinc-800 px-2 py-1.5 text-xs hover:bg-zinc-700">
        Create
      </button>
    </form>
  );
}

function CreatorFooter({ creatorId }: { creatorId: string }) {
  const [page, setPage] = useState<CreatorPage | null>(null);
  const { state: followState, toggle: toggleFollow } = useFollow(creatorId);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    api<CreatorPage>(routes.creator(creatorId))
      .then(setPage)
      .catch(() => {});
  }, [creatorId]);

  if (!page) return null;

  return (
    <div className="mt-10 rounded-2xl border border-zinc-800 bg-zinc-900 p-6">
      <div className="flex items-center justify-between">
        <div>
          <Link
            href={`/creators/${creatorId}`}
            className="font-semibold hover:text-red-300"
          >
            {page.creator.channel_name}
          </Link>
          <p className="text-sm text-zinc-500">
            @{page.creator.handle} · {followState?.subscribers ?? page.creator.subscriber_count} subscribers
          </p>
        </div>
        <button
          onClick={() =>
            toggleFollow().catch((e: Error) => setActionError(e.message))
          }
          className={`rounded-full px-5 py-2 text-sm font-medium ${
            followState?.following
              ? "bg-zinc-800 text-zinc-200 hover:bg-zinc-700"
              : "bg-red-600 text-white hover:bg-red-500"
          }`}
        >
          {followState?.following ? "Following" : "Follow"}
        </button>
      </div>
      {actionError && <p className="mt-2 text-sm text-red-400">{actionError}</p>}
    </div>
  );
}
