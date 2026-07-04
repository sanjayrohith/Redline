"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import type { BenchmarkRunView } from "@/lib/api/types";

export default function BenchmarksPage() {
  const [deploymentID, setDeploymentID] = useState("");
  const [precision, setPrecision] = useState("fp16");
  const [temperature, setTemperature] = useState(0.7);
  const [topP, setTopP] = useState(0.9);
  const [seed, setSeed] = useState("");
  const [result, setResult] = useState<BenchmarkRunView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    setResult(null);

    const res = await fetch("/api/dashboard/benchmarks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        deployment_id: deploymentID,
        precision,
        temperature,
        top_p: topP,
        seed: seed ? Number(seed) : undefined,
      }),
    });

    setSubmitting(false);
    if (!res.ok) {
      setError("Benchmark run failed. Check the deployment id and try again.");
      return;
    }
    setResult(await res.json());
  }

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Benchmark runner</h1>
      <p style={{ opacity: 0.7 }}>
        Dispatches a fixed, deterministically scored task suite against a pinned deployment. See{" "}
        {result && <Link href={`/dashboard/benchmarks/${result.model_id}`}>results history</Link>}
        {!result && "results history after your first run"}.
      </p>

      <form onSubmit={onSubmit} style={{ display: "flex", flexDirection: "column", gap: "0.75rem", maxWidth: 360 }}>
        <label>
          Deployment ID
          <input required value={deploymentID} onChange={(e) => setDeploymentID(e.target.value)} />
        </label>
        <label>
          Precision
          <select value={precision} onChange={(e) => setPrecision(e.target.value)}>
            <option value="fp16">FP16</option>
            <option value="fp8">FP8</option>
            <option value="int4">INT4</option>
          </select>
        </label>
        <label>
          Temperature ({temperature.toFixed(2)})
          <input
            type="range" min={0} max={2} step={0.05}
            value={temperature}
            onChange={(e) => setTemperature(Number(e.target.value))}
          />
        </label>
        <label>
          Top-p ({topP.toFixed(2)})
          <input
            type="range" min={0.01} max={1} step={0.01}
            value={topP}
            onChange={(e) => setTopP(Number(e.target.value))}
          />
        </label>
        <label>
          Seed (optional, for reproducibility)
          <input type="number" value={seed} onChange={(e) => setSeed(e.target.value)} />
        </label>
        <button type="submit" disabled={submitting}>
          {submitting ? "Running…" : "Run benchmark"}
        </button>
      </form>

      {error && <p role="alert" style={{ color: "#f87171" }}>{error}</p>}

      {result && (
        <section style={{ marginTop: "2rem" }}>
          <h2>
            Score: {(result.score * 100).toFixed(0)}% ({result.passed_count}/{result.task_count})
          </h2>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                <th style={{ textAlign: "left" }}>Task</th>
                <th style={{ textAlign: "left" }}>Response</th>
                <th style={{ textAlign: "left" }}>Result</th>
              </tr>
            </thead>
            <tbody>
              {result.tasks?.map((t) => (
                <tr key={t.task.name}>
                  <td>{t.task.name}</td>
                  <td style={{ maxWidth: 240, overflow: "hidden", textOverflow: "ellipsis" }}>{t.response}</td>
                  <td style={{ color: t.passed ? "#4ade80" : "#f87171" }}>{t.passed ? "pass" : "fail"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </main>
  );
}
