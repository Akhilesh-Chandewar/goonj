"use client";

/**
 * Telemetry bridge: mounts once in the root layout and wires the player
 * store's history beacons to the history API. Kept separate from PlayerBar
 * so playback logic stays free of network concerns.
 */

import { useEffect } from "react";
import { api, beacon, getAccessToken, routes } from "@/lib/api";
import { flushProgress, setPlayerTelemetry } from "@/stores/player";

export function TelemetryBridge() {
  useEffect(() => {
    setPlayerTelemetry({
      report: (audioId, positionMs, durationMs) =>
        beacon(routes.historyProgress(audioId), {
          position_ms: positionMs,
          duration_ms: durationMs,
        }),
      resume: async (audioId) => {
        if (!getAccessToken()) return 0;
        try {
          const r = await api<{ position_ms: number }>(
            routes.historyResume(audioId)
          );
          return r.position_ms ?? 0;
        } catch {
          return 0;
        }
      },
    });

    const onPageHide = () => flushProgress();
    window.addEventListener("pagehide", onPageHide);
    return () => window.removeEventListener("pagehide", onPageHide);
  }, []);

  return null;
}
