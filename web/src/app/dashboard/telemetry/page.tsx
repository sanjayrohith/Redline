"use client";

import { useMemo } from "react";
import { useTelemetrySocket, type TelemetrySample } from "@/lib/useTelemetrySocket";

const CHART_WIDTH = 640;
const CHART_HEIGHT = 160;

function LineChart({
  points,
  color,
  label,
}: {
  points: { x: number; y: number }[];
  color: string;
  label: string;
}) {
  if (points.length === 0) {
    return (
      <div>
        <h3>{label}</h3>
        <p style={{ opacity: 0.6 }}>Waiting for data…</p>
      </div>
    );
  }

  const maxY = Math.max(...points.map((p) => p.y), 1);
  const latest = points[points.length - 1]!;
  const path = points
    .map((p, i) => {
      const x = (i / Math.max(points.length - 1, 1)) * CHART_WIDTH;
      const y = CHART_HEIGHT - (p.y / maxY) * CHART_HEIGHT;
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <div>
      <h3>
        {label} <span style={{ fontWeight: "normal", opacity: 0.7 }}>(latest: {latest.y.toFixed(1)}ms)</span>
      </h3>
      <svg width={CHART_WIDTH} height={CHART_HEIGHT} role="img" aria-label={`${label} over time`}>
        <path d={path} fill="none" stroke={color} strokeWidth={2} />
      </svg>
    </div>
  );
}

function toPoints(samples: TelemetrySample[], field: "ttft_ms" | "tpot_ms") {
  return samples.filter((s) => s[field] !== undefined).map((s) => ({ x: Date.parse(s.sampled_at), y: s[field]! }));
}

export default function TelemetryPage() {
  const { samples, connected } = useTelemetrySocket();

  const ttftPoints = useMemo(() => toPoints(samples, "ttft_ms"), [samples]);
  const tpotPoints = useMemo(() => toPoints(samples, "tpot_ms"), [samples]);

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Live latency</h1>
      <p style={{ opacity: 0.7 }}>{connected ? "Connected" : "Connecting…"}</p>

      <div style={{ display: "flex", flexDirection: "column", gap: "2rem", marginTop: "1.5rem" }}>
        <LineChart points={ttftPoints} color="#4ade80" label="Time to first token" />
        <LineChart points={tpotPoints} color="#60a5fa" label="Time per output token (rolling)" />
      </div>
    </main>
  );
}
