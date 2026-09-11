"use client";

/**
 * Client-side helpers for the Phase 4 engagement surface: likes, follows,
 * and playlist adds. All server state, no local caching — TanStack Query
 * arrives with the Phase 4 polish pass if lists grow.
 */

import { useCallback, useEffect, useState } from "react";
import { api, getAccessToken } from "@/lib/api";
import { routes } from "@/lib/api";
import type { FollowState, LikeState, Playlist } from "@/lib/live-types";

/** Like state + optimistic toggle for one episode. */
export function useLike(audioId: string) {
  const [state, setState] = useState<LikeState | null>(null);

  useEffect(() => {
    if (!audioId) return;
    api<LikeState>(routes.audioLike(audioId))
      .then(setState)
      .catch(() => setState(null));
  }, [audioId]);

  const toggle = useCallback(async () => {
    if (!getAccessToken()) {
      throw new Error("Sign in to like episodes");
    }
    if (!state) return;
    const next = state.liked
      ? await api<LikeState>(routes.audioLike(audioId), { method: "DELETE" })
      : await api<LikeState>(routes.audioLike(audioId), { method: "POST" });
    setState(next);
  }, [audioId, state]);

  return { state, toggle };
}

/** Follow state + toggle for one creator. */
export function useFollow(creatorId: string) {
  const [state, setState] = useState<FollowState | null>(null);

  useEffect(() => {
    if (!creatorId) return;
    api<FollowState>(routes.creatorFollow(creatorId))
      .then(setState)
      .catch(() => setState(null));
  }, [creatorId]);

  const toggle = useCallback(async () => {
    if (!getAccessToken()) {
      throw new Error("Sign in to follow creators");
    }
    if (!state) return;
    const next = state.following
      ? await api<FollowState>(routes.creatorFollow(creatorId), {
          method: "DELETE",
        })
      : await api<FollowState>(routes.creatorFollow(creatorId), {
          method: "POST",
        });
    setState(next);
  }, [creatorId, state]);

  return { state, toggle };
}

/** The caller's playlists + add-to-playlist helper. */
export function usePlaylists() {
  const [playlists, setPlaylists] = useState<Playlist[] | null>(null);

  const reload = useCallback(() => {
    if (!getAccessToken()) {
      setPlaylists([]);
      return;
    }
    api<{ data: Playlist[] }>(routes.playlists)
      .then((r) => setPlaylists(r.data ?? []))
      .catch(() => setPlaylists([]));
  }, []);

  useEffect(reload, [reload]);

  const addTo = useCallback(
    async (playlistId: string, audioId: string) => {
      await api(routes.playlistItems(playlistId), {
        method: "POST",
        body: JSON.stringify({ audio_id: audioId }),
      });
    },
    []
  );

  const create = useCallback(async (title: string, audioId?: string) => {
    const pl = await api<Playlist>(routes.playlists, {
      method: "POST",
      body: JSON.stringify({ title }),
    });
    if (audioId) await addTo(pl.id, audioId);
    reload();
    return pl;
  }, [addTo, reload]);

  return { playlists, addTo, create, reload };
}
