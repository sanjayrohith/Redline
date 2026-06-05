"use client";

import { useCallback, useEffect, useState } from "react";
import type { APIKeyListResponse, APIKeyView } from "@/lib/api/types";

export default function APIKeysPage() {
  const [keys, setKeys] = useState<APIKeyView[]>([]);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    const res = await fetch("/api/keys");
    if (res.ok) {
      const body: APIKeyListResponse = await res.json();
      setKeys(body.data ?? []);
    } else {
      setError("Failed to load API keys.");
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function createKey() {
    setError(null);
    const res = await fetch("/api/keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scopes: ["chat"] }),
    });
    if (!res.ok) {
      setError("Failed to create API key.");
      return;
    }
    const created: APIKeyView = await res.json();
    // The plaintext key is only ever present in this one response - it is
    // never returned again, including by the list endpoint.
    setNewlyCreatedKey(created.key ?? null);
    await load();
  }

  async function revokeKey(id: string) {
    if (!confirm("Revoke this API key? Requests using it will stop working immediately.")) {
      return;
    }
    const res = await fetch(`/api/keys/${encodeURIComponent(id)}`, { method: "DELETE" });
    if (!res.ok) {
      setError("Failed to revoke API key.");
      return;
    }
    await load();
  }

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>API keys</h1>

      {newlyCreatedKey && (
        <div role="alert" style={{ border: "1px solid #4ade80", padding: "1rem", marginBottom: "1rem" }}>
          <p>Copy this key now - it will not be shown again:</p>
          <code style={{ userSelect: "all" }}>{newlyCreatedKey}</code>
          <div>
            <button type="button" onClick={() => setNewlyCreatedKey(null)}>
              Done
            </button>
          </div>
        </div>
      )}

      {error && <p role="alert" style={{ color: "#f87171" }}>{error}</p>}

      <button type="button" onClick={createKey}>
        Create new key
      </button>

      {loading ? (
        <p>Loading…</p>
      ) : (
        <table style={{ width: "100%", marginTop: "1.5rem", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th style={{ textAlign: "left" }}>Key</th>
              <th style={{ textAlign: "left" }}>Created</th>
              <th style={{ textAlign: "left" }}>Last used</th>
              <th style={{ textAlign: "left" }}>Status</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id}>
                <td>
                  <code>{k.display_prefix}…</code>
                </td>
                <td>{new Date(k.created_at).toLocaleString()}</td>
                <td>{k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "never"}</td>
                <td>{k.revoked_at ? "revoked" : "active"}</td>
                <td>
                  {!k.revoked_at && (
                    <button type="button" onClick={() => revokeKey(k.id)}>
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}
