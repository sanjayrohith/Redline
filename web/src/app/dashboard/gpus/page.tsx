"use client";

import { useMemo } from "react";
import { useTelemetrySocket } from "@/lib/useTelemetrySocket";

function Gauge({ label, percent, sublabel }: { label: string; percent: number; sublabel: string }) {
  const clamped = Math.max(0, Math.min(100, percent));
  const color = clamped > 90 ? "#f87171" : clamped > 70 ? "#facc15" : "#4ade80";

  return (
    <div style={{ width: 220 }}>
      <h3 style={{ marginBottom: "0.25rem" }}>{label}</h3>
      <div style={{ background: "#333", height: 12, borderRadius: 6, overflow: "hidden" }}>
        <div style={{ width: `${clamped}%`, height: "100%", background: color, transition: "width 0.3s ease" }} />
      </div>
      <p style={{ margin: "0.25rem 0 0", opacity: 0.8, fontSize: "0.85rem" }}>{sublabel}</p>
    </div>
  );
}

function gigabytes(bytes: number): string {
  return (bytes / 1e9).toFixed(1) + " GB";
}

export default function GPUsPage() {
  const { samples, connected } = useTelemetrySocket();

  const byDevice = useMemo(() => {
    const latest = new Map<string, (typeof samples)[number]>();
    for (const s of samples) {
      if (s.run_id.startsWith("gpu:")) latest.set(s.run_id, s);
    }
    return [...latest.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [samples]);

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>GPU utilization</h1>
      <p style={{ opacity: 0.7 }}>{connected ? "Connected" : "Connecting…"}</p>

      {byDevice.length === 0 ? (
        <p style={{ opacity: 0.6 }}>Waiting for GPU telemetry…</p>
      ) : (
        <div style={{ display: "flex", gap: "2rem", flexWrap: "wrap", marginTop: "1.5rem" }}>
          {byDevice.map(([id, s]) => (
            <div key={id} style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
              <strong>Device {id.replace("gpu:", "")}</strong>
              <Gauge
                label="Utilization"
                percent={s.gpu_utilization_pct ?? 0}
                sublabel={`${(s.gpu_utilization_pct ?? 0).toFixed(0)}%`}
              />
              <Gauge
                label="VRAM"
                percent={s.vram_total_bytes ? ((s.vram_bytes ?? 0) / s.vram_total_bytes) * 100 : 0}
                sublabel={`${gigabytes(s.vram_bytes ?? 0)} / ${gigabytes(s.vram_total_bytes ?? 0)}`}
              />
            </div>
          ))}
        </div>
      )}
    </main>
  );
}
