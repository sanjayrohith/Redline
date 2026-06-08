"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import type { ModelDetailView } from "@/lib/api/types";

export default function ModelsPage() {
  const [models, setModels] = useState<ModelDetailView[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch("/api/dashboard/models")
      .then((res) => res.json())
      .then((body: { data: ModelDetailView[] }) => setModels(body.data ?? []))
      .finally(() => setLoading(false));
  }, []);

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Models</h1>
      {loading ? (
        <p>Loading…</p>
      ) : models.length === 0 ? (
        <p>
          No models ingested yet. <Link href="/dashboard/ingest">Ingest one</Link>.
        </p>
      ) : (
        <ul style={{ listStyle: "none", padding: 0 }}>
          {models.map((m) => (
            <li key={m.id} style={{ padding: "0.75rem 0", borderBottom: "1px solid #333" }}>
              <Link href={`/dashboard/models/${m.id}`}>{m.repo_url}</Link>
              <span style={{ marginLeft: "0.75rem", opacity: 0.7 }}>{m.architecture}</span>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
