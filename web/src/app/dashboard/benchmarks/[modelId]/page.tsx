"use client";

import { use, useEffect, useState } from "react";
import type { BenchmarkRunView } from "@/lib/api/types";

function groupByPrecision(runs: BenchmarkRunView[]): Map<string, BenchmarkRunView[]> {
  const groups = new Map<string, BenchmarkRunView[]>();
  for (const run of runs) {
    const list = groups.get(run.precision) ?? [];
    list.push(run);
    groups.set(run.precision, list);
  }
  return groups;
}

export default function BenchmarkHistoryPage({ params }: { params: Promise<{ modelId: string }> }) {
  const { modelId } = use(params);
  const [runs, setRuns] = useState<BenchmarkRunView[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch(`/api/dashboard/benchmarks?model_id=${encodeURIComponent(modelId)}`)
      .then((res) => res.json())
      .then((body: { data: BenchmarkRunView[] }) => setRuns(body.data ?? []))
      .finally(() => setLoading(false));
  }, [modelId]);

  if (loading) {
    return (
      <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
        <p>Loading…</p>
      </main>
    );
  }

  const byPrecision = groupByPrecision(runs);

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Benchmark history</h1>

      {runs.length === 0 ? (
        <p>No benchmark runs yet for this model.</p>
      ) : (
        <>
          <section>
            <h2>By precision</h2>
            <table style={{ width: "100%", borderCollapse: "collapse", marginBottom: "2rem" }}>
              <thead>
                <tr>
                  <th style={{ textAlign: "left" }}>Precision</th>
                  <th style={{ textAlign: "left" }}>Runs</th>
                  <th style={{ textAlign: "left" }}>Best score</th>
                  <th style={{ textAlign: "left" }}>Latest score</th>
                </tr>
              </thead>
              <tbody>
                {[...byPrecision.entries()].map(([precision, precisionRuns]) => (
                  <tr key={precision}>
                    <td>{precision}</td>
                    <td>{precisionRuns.length}</td>
                    <td>{(Math.max(...precisionRuns.map((r) => r.score)) * 100).toFixed(0)}%</td>
                    <td>{((precisionRuns[0]?.score ?? 0) * 100).toFixed(0)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>

          <section>
            <h2>All runs</h2>
            <table style={{ width: "100%", borderCollapse: "collapse" }}>
              <thead>
                <tr>
                  <th style={{ textAlign: "left" }}>Date</th>
                  <th style={{ textAlign: "left" }}>Precision</th>
                  <th style={{ textAlign: "left" }}>Temp</th>
                  <th style={{ textAlign: "left" }}>Top-p</th>
                  <th style={{ textAlign: "left" }}>Score</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id}>
                    <td>{new Date(r.created_at * 1000).toLocaleString()}</td>
                    <td>{r.precision}</td>
                    <td>{r.temperature}</td>
                    <td>{r.top_p}</td>
                    <td>
                      {(r.score * 100).toFixed(0)}% ({r.passed_count}/{r.task_count})
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        </>
      )}
    </main>
  );
}
