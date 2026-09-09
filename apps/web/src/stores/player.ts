"use client";

/**
 * Global player state (zustand). The <audio> element lives in PlayerBar,
 * mounted in the root layout so playback survives navigation — the core
 * "persistent player" requirement.
 */

import { create } from "zustand";

export interface Track {
  id: string;
  title: string;
  creator: string;
  durationMs: number;
  /** Presigned URLs by quality. */
  sources: { quality: string; url: string; bitrate_kbps: number }[];
}

export type RepeatMode = "off" | "all" | "one";

interface PlayerState {
  current: Track | null;
  queue: Track[];
  playing: boolean;
  currentTime: number;
  duration: number;
  volume: number;
  muted: boolean;
  speed: number;
  quality: string;
  shuffle: boolean;
  repeat: RepeatMode;

  playTrack: (track: Track, queue?: Track[]) => void;
  enqueue: (track: Track) => void;
  togglePlay: () => void;
  next: () => void;
  prev: () => void;
  seek: (t: number) => void;
  skipBy: (seconds: number) => void;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  setSpeed: (s: number) => void;
  setQuality: (q: string) => void;
  toggleShuffle: () => void;
  cycleRepeat: () => void;
  setProgress: (currentTime: number, duration: number) => void;
  setPlaying: (p: boolean) => void;
}

export const usePlayer = create<PlayerState>((set, get) => ({
  current: null,
  queue: [],
  playing: false,
  currentTime: 0,
  duration: 0,
  volume: 0.8,
  muted: false,
  speed: 1,
  quality: "medium",
  shuffle: false,
  repeat: "off",

  playTrack: (track, queue) =>
    set((s) => ({
      current: track,
      queue: queue ?? (s.current ? [s.current, ...s.queue] : s.queue),
      playing: true,
      currentTime: 0,
    })),

  enqueue: (track) => set((s) => ({ queue: [...s.queue, track] })),

  togglePlay: () => set((s) => ({ playing: !s.playing && !!s.current })),

  next: () => {
    const { queue, current, shuffle, repeat } = get();
    if (queue.length === 0) {
      set({ playing: false });
      return;
    }
    let idx = 0;
    if (shuffle) {
      idx = Math.floor(Math.random() * queue.length);
    }
    const [nextTrack] = queue.splice(idx, 1);
    set({
      current: nextTrack ?? current,
      queue,
      playing: true,
      currentTime: 0,
      repeat: repeat === "one" ? "off" : repeat,
    });
  },

  prev: () => set({ currentTime: 0 }), // restart current; full history later

  seek: (t) => set({ currentTime: t }),

  skipBy: (seconds) => {
    const { currentTime, duration } = get();
    const target = Math.min(Math.max(currentTime + seconds, 0), duration || seconds);
    set({ currentTime: target });
  },

  setVolume: (v) => set({ volume: Math.min(Math.max(v, 0), 1), muted: false }),
  toggleMute: () => set((s) => ({ muted: !s.muted })),
  setSpeed: (speed) => set({ speed }),
  setQuality: (quality) => set({ quality }),
  toggleShuffle: () => set((s) => ({ shuffle: !s.shuffle })),
  cycleRepeat: () =>
    set((s) => ({
      repeat: s.repeat === "off" ? "all" : s.repeat === "all" ? "one" : "off",
    })),

  setProgress: (currentTime, duration) => set({ currentTime, duration }),
  setPlaying: (playing) => set({ playing }),
}));

/** Pick the best available source URL for the requested quality. */
export function sourceFor(track: Track, quality: string): string | null {
  if (track.sources.length === 0) return null;
  const exact = track.sources.find((s) => s.quality === quality);
  if (exact) return exact.url;
  const order = ["medium", "high", "low"];
  for (const q of order) {
    const s = track.sources.find((x) => x.quality === q);
    if (s) return s.url;
  }
  return track.sources[0].url;
}
