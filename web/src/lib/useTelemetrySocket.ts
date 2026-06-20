"use client";

import { useEffect, useRef, useState } from "react";
import { gatewayWSBaseURL } from "@/lib/config";

export interface TelemetrySample {
  run_id: string;
  ttft_ms?: number;
  tpot_ms?: number;
  vram_bytes?: number;
  vram_total_bytes?: number;
  gpu_utilization_pct?: number;
  sampled_at: string;
}

const MAX_POINTS = 200;

// Connects to the gateway's live telemetry WebSocket and keeps a bounded,
// append-only window of the most recent samples - old points fall off the
// front so a long-running session's memory stays flat, matching what a
// session-timeline chart can usefully render anyway.
export function useTelemetrySocket() {
  const [samples, setSamples] = useState<TelemetrySample[]>([]);
  const [connected, setConnected] = useState(false);
  const socketRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const res = await fetch("/api/dashboard/ws-ticket");
      if (!res.ok || cancelled) return;
      const { token } = await res.json();

      const socket = new WebSocket(`${gatewayWSBaseURL()}/v1/dashboard/ws/telemetry?access_token=${token}`);
      socketRef.current = socket;

      socket.onopen = () => setConnected(true);
      socket.onclose = () => setConnected(false);
      socket.onmessage = (event) => {
        try {
          const sample: TelemetrySample = JSON.parse(event.data);
          setSamples((prev) => {
            const next = [...prev, sample];
            return next.length > MAX_POINTS ? next.slice(next.length - MAX_POINTS) : next;
          });
        } catch {
          // heartbeat pings and non-JSON frames are not samples - ignore
        }
      };
    })();

    return () => {
      cancelled = true;
      socketRef.current?.close();
    };
  }, []);

  return { samples, connected };
}
