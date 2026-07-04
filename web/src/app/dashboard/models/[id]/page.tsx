"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import type { ModelDetailView } from "@/lib/api/types";

function gigabytes(bytes: number): string {
  return (bytes / 1e9).toFixed(2) + " GB";
}

export default function ModelDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [model, setModel] = useState<ModelDetailView | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch(`/api/dashboard/models/${encodeURIComponent(id)}`)
      .then((res) => {
        if (!res.ok) throw new Error("not found");
        return res.json();
      })
      .then(setModel)
      .catch(() => setError("Model not found."));
  }, [id]);

  if (error) {
    return (
      <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
        <p role="alert">{error}</p>
      </main>
    );
  }
  if (!model) {
    return (
      <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
        <p>Loading…</p>
      </main>
    );
  }

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>{model.repo_url}</h1>
      <p>
        Revision <code>{model.revision}</code> ({model.revision_sha.slice(0, 12)})
      </p>

      <dl style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: "0.5rem 1rem" }}>
        <dt>Architecture</dt>
        <dd>{model.architecture}</dd>

        <dt>Parameters</dt>
        <dd>{(model.parameter_count / 1e9).toFixed(2)}B</dd>

        <dt>Tensor dtype</dt>
        <dd>{model.dtype}</dd>

        <dt>License</dt>
        <dd>{model.license || "unspecified"}</dd>

        <dt>KV cache</dt>
        <dd>{gigabytes(model.kv_cache_bytes)}</dd>
      </dl>

      <h2>VRAM footprint by precision</h2>
      <table style={{ width: "100%", borderCollapse: "collapse" }}>
        <thead>
          <tr>
            <th style={{ textAlign: "left" }}>Precision</th>
            <th style={{ textAlign: "left" }}>Total VRAM</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>FP16</td>
            <td>{gigabytes(model.vram_estimate_fp16_bytes)}</td>
          </tr>
          <tr>
            <td>FP8</td>
            <td>{gigabytes(model.vram_estimate_fp8_bytes)}</td>
          </tr>
          <tr>
            <td>INT4</td>
            <td>{gigabytes(model.vram_estimate_int4_bytes)}</td>
          </tr>
        </tbody>
      </table>

      <p style={{ marginTop: "1.5rem" }}>
        <Link href={`/dashboard/benchmarks/${model.id}`}>View benchmark history →</Link>
      </p>
    </main>
  );
}
