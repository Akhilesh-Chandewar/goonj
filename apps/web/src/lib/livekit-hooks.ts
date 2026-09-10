"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Room, RoomEvent, Track } from "livekit-client";

export { LiveKitRoom } from "@livekit/components-react";

/**
 * useLocalMicTrack connects a creator's room and publishes their microphone.
 * Kept intentionally small: connect once, publish, mute/unmute, cleanup on
 * unmount. The LiveKit React components handle the listener side.
 * Recording (room-composite egress) runs server-side and does not depend on
 * this track; the ready flag simply sequences the recording-start request
 * right after go-live so silent lead-in stays short.
 */
export function useLocalMicTrack(token: string, wsUrl: string, _roomName: string) {
  const roomRef = useRef<Room | null>(null);
  const [enabled, setEnabled] = useState(true);
  const [track, setTrack] = useState<unknown>(null);
  // ready: connected AND microphone published — the signal for the page to
  // ask the API to start participant-egress recording.
  const [ready, setReady] = useState(false);

  useEffect(() => {
    const room = new Room({ audioCaptureDefaults: { echoCancellation: true } });
    roomRef.current = room;

    let cancelled = false;
    room
      .connect(wsUrl, token, { autoSubscribe: false })
      .then(async () => {
        if (cancelled) return;
        await room.localParticipant.setMicrophoneEnabled(true);
        setTrack(room.localParticipant.getTrackPublication(Track.Source.Microphone));
        setReady(true);
      })
      .catch(() => {
        setEnabled(false);
        setReady(false);
      });

    return () => {
      cancelled = true;
      room.disconnect();
      roomRef.current = null;
    };
  }, [token, wsUrl]);

  const toggle = useCallback(() => {
    const room = roomRef.current;
    if (!room) return;
    const next = !enabled;
    void room.localParticipant.setMicrophoneEnabled(next);
    setEnabled(next);
  }, [enabled]);

  return { enabled, toggle, track, ready };
}
