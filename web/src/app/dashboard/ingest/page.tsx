"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import type { IngestionJobView } from "@/lib/api/types";

const POLL_INTERVAL_MS = 1500;
const TERMINAL_STATES = new Set(["cached", "failed"]);

const STATE_LABELS: Record<string, string> = {
  queued: "Parsing repository…",
  downloading: "Downloading and reading model metadata…",
  verifying: "Verifying architecture and computing VRAM footprint…",
  cached: "Ready",
  failed: "Failed",
};

export default function IngestPage() {
  const [repoURL, setRepoURL] = useState("");
  const [job, setJob] = useState<IngestionJobView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  function startPolling(id: string) {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(async () => {
      const res = await fetch(`/api/ingestions/${encodeURIComponent(id)}`);
      if (!res.ok) return;
      const current: IngestionJobView = await res.json();
      setJob(current);
      if (TERMINAL_STATES.has(current.state) && pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    }, POLL_INTERVAL_MS);
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    setJob(null);

    const res = await fetch("/api/ingestions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ repo_url: repoURL }),
    });

    setSubmitting(false);
    if (!res.ok) {
      setError("Failed to start ingestion. Check the repository URL and try again.");
      return;
    }
    const created: IngestionJobView = await res.json();
    setJob(created);
    startPolling(created.id);
  }

  const progressPercent =
    job && job.bytes_total > 0 ? Math.round((job.bytes_downloaded / job.bytes_total) * 100) : 0;

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Ingest a model</h1>

      <form onSubmit={onSubmit} style={{ display: "flex", gap: "0.5rem", marginTop: "1rem" }}>
        <input
          type="text"
          required
          placeholder="org/model or https://huggingface.co/org/model"
          value={repoURL}
          onChange={(e) => setRepoURL(e.target.value)}
          style={{ flex: 1 }}
        />
        <button type="submit" disabled={submitting}>
          {submitting ? "Starting…" : "Ingest"}
        </button>
      </form>

      {error && (
        <p role="alert" style={{ color: "#f87171" }}>
          {error}
        </p>
      )}

      {job && (
        <section style={{ marginTop: "2rem" }}>
          <p>{STATE_LABELS[job.state] ?? job.state}</p>
          <div style={{ background: "#333", height: 8, borderRadius: 4, overflow: "hidden" }}>
            <div
              style={{
                width: `${progressPercent}%`,
                height: "100%",
                background: job.state === "failed" ? "#f87171" : "#4ade80",
                transition: "width 0.3s ease",
              }}
            />
          </div>
          {job.bytes_total > 0 && (
            <p>
              {(job.bytes_downloaded / 1e9).toFixed(2)} GB / {(job.bytes_total / 1e9).toFixed(2)} GB
            </p>
          )}
          {job.state === "failed" && job.error_message && (
            <p role="alert" style={{ color: "#f87171" }}>
              {job.error_message}
            </p>
          )}
          {job.state === "cached" && job.model_id && <p>Model ready: {job.model_id}</p>}
        </section>
      )}
    </main>
  );
}
