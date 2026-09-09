"use client";

import { useEffect, useRef } from "react";
import { usePlayer, sourceFor } from "@/stores/player";

const fmt = (s: number) => {
  if (!isFinite(s)) return "0:00";
  const m = Math.floor(s / 60);
  const sec = Math.floor(s % 60);
  return `${m}:${String(sec).padStart(2, "0")}`;
};

/**
 * PlayerBar is mounted once in the root layout. The <audio> element lives
 * here — page navigation never unmounts it, so audio keeps playing.
 */
export function PlayerBar() {
  const audioRef = useRef<HTMLAudioElement | null>(null);

  const {
    current, playing, currentTime, duration, volume, muted, speed, quality,
    togglePlay, next, setProgress, setPlaying, seek, skipBy,
    setVolume, toggleMute, setSpeed, setQuality,
  } = usePlayer();

  const src = current ? sourceFor(current, quality) : null;

  // Load and play whenever the track or quality changes.
  useEffect(() => {
    const el = audioRef.current;
    if (!el || !current || !src) return;
    if (el.dataset.src !== src) {
      el.src = src;
      el.dataset.src = src;
      el.load();
    }
  }, [current, src]);

  useEffect(() => {
    const el = audioRef.current;
    if (!el) return;
    el.playbackRate = speed;
    el.volume = muted ? 0 : volume;
    if (playing) el.play().catch(() => setPlaying(false));
    else el.pause();
  }, [playing, speed, volume, muted, setPlaying]);

  // External seeks (progress bar, skip buttons).
  useEffect(() => {
    const el = audioRef.current;
    if (!el) return;
    if (Math.abs(el.currentTime - currentTime) > 0.3) {
      el.currentTime = currentTime;
    }
  }, [currentTime]);

  if (!current) return null;

  const pct = duration > 0 ? (currentTime / duration) * 100 : 0;

  return (
    <div className="fixed inset-x-0 bottom-0 z-50 border-t border-zinc-800 bg-zinc-950/95 backdrop-blur">
      <audio
        ref={audioRef}
        onTimeUpdate={(e) =>
          setProgress(e.currentTarget.currentTime, e.currentTarget.duration || 0)
        }
        onEnded={() => {
          const { repeat, next: playNext } = usePlayer.getState();
          if (repeat === "one") {
            const el = audioRef.current;
            if (el) {
              el.currentTime = 0;
              el.play().catch(() => {});
            }
            return;
          }
          if (repeat === "off" && usePlayer.getState().queue.length === 0) {
            setPlaying(false);
            return;
          }
          playNext();
        }}
      />

      {/* Progress */}
      <div className="h-1 w-full bg-zinc-800">
        <div
          className="h-full bg-red-500 transition-[width] duration-200"
          style={{ width: `${pct}%` }}
        />
      </div>

      <div className="mx-auto flex max-w-6xl items-center gap-4 px-4 py-3">
        {/* Track info */}
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-zinc-100">
            {current.title}
          </p>
          <p className="truncate text-xs text-zinc-500">{current.creator}</p>
        </div>

        {/* Controls */}
        <div className="flex items-center gap-2">
          <button
            onClick={() => skipBy(-10)}
            className="rounded-full p-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
            aria-label="back 10 seconds"
          >
            ↺10
          </button>
          <button
            onClick={togglePlay}
            className="rounded-full bg-red-600 p-3 text-white hover:bg-red-500"
            aria-label={playing ? "pause" : "play"}
          >
            {playing ? "⏸" : "▶"}
          </button>
          <button
            onClick={() => skipBy(15)}
            className="rounded-full p-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
            aria-label="forward 15 seconds"
          >
            15↻
          </button>
        </div>

        {/* Time + progress click-seek */}
        <div className="hidden flex-1 items-center gap-3 sm:flex">
          <span className="text-xs tabular-nums text-zinc-500">
            {fmt(currentTime)}
          </span>
          <input
            type="range"
            min={0}
            max={duration || 100}
            step={1}
            value={currentTime}
            onChange={(e) => seek(Number(e.target.value))}
            className="h-1 flex-1 accent-red-500"
            aria-label="seek"
          />
          <span className="text-xs tabular-nums text-zinc-500">
            {fmt(duration)}
          </span>
        </div>

        {/* Volume + speed + quality */}
        <div className="hidden items-center gap-3 md:flex">
          <button
            onClick={toggleMute}
            className="text-zinc-400 hover:text-zinc-100"
            aria-label="mute"
          >
            {muted || volume === 0 ? "🔇" : "🔊"}
          </button>
          <input
            type="range"
            min={0}
            max={1}
            step={0.05}
            value={muted ? 0 : volume}
            onChange={(e) => setVolume(Number(e.target.value))}
            className="h-1 w-20 accent-red-500"
            aria-label="volume"
          />
          <select
            value={speed}
            onChange={(e) => setSpeed(Number(e.target.value))}
            className="rounded bg-zinc-900 px-2 py-1 text-xs text-zinc-300"
            aria-label="playback speed"
          >
            {[0.75, 1, 1.25, 1.5, 2].map((s) => (
              <option key={s} value={s}>{s}×</option>
            ))}
          </select>
          <select
            value={quality}
            onChange={(e) => setQuality(e.target.value)}
            className="rounded bg-zinc-900 px-2 py-1 text-xs text-zinc-300"
            aria-label="quality"
          >
            {["low", "medium", "high"].map((q) => (
              <option key={q} value={q}>{q}</option>
            ))}
          </select>
        </div>

        {/* Next on small screens */}
        <button
          onClick={next}
          className="rounded-full p-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 sm:hidden"
          aria-label="next"
        >
          ⏭
        </button>
      </div>
    </div>
  );
}
