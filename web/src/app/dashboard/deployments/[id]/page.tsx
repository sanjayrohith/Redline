"use client";

import { use, useEffect, useState } from "react";
import type { SessionCostView } from "@/lib/api/types";

const POLL_INTERVAL_MS = 5000;

function formatCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export default function DeploymentSessionPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [session, setSession] = useState<SessionCostView | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      const res = await fetch(`/api/dashboard/deployments/${encodeURIComponent(id)}/session`);
      if (cancelled) return;
      if (!res.ok) {
        setError("Deployment not found.");
        return;
      }
      setSession(await res.json());
    }

    poll();
    const interval = setInterval(poll, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [id]);

  if (error) {
    return (
      <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
        <p role="alert">{error}</p>
      </main>
    );
  }
  if (!session) {
    return (
      <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
        <p>Loading…</p>
      </main>
    );
  }

  const reapSoon = session.seconds_until_reap < 60;

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Session</h1>
      <p style={{ opacity: 0.7 }}>Deployment {session.deployment_id}</p>

      <div style={{ display: "flex", gap: "2rem", marginTop: "1.5rem", flexWrap: "wrap" }}>
        <div>
          <h3>Accumulated cost</h3>
          <p style={{ fontSize: "1.5rem" }}>
            {session.cost_usd !== undefined ? `$${session.cost_usd.toFixed(2)}` : "unpriced"}
          </p>
        </div>
        <div>
          <h3>Projected hourly burn</h3>
          <p style={{ fontSize: "1.5rem" }}>
            {session.projected_hourly_usd !== undefined ? `$${session.projected_hourly_usd.toFixed(2)}/hr` : "—"}
          </p>
        </div>
        <div>
          <h3>Idle termination</h3>
          <p style={{ fontSize: "1.5rem", color: reapSoon ? "#f87171" : undefined }}>
            {formatCountdown(session.seconds_until_reap)}
          </p>
        </div>
      </div>
    </main>
  );
}
