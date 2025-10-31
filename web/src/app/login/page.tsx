"use client";

import { FormEvent, useState, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });

    setSubmitting(false);
    if (!res.ok) {
      setError("Invalid email or password.");
      return;
    }
    router.push(searchParams.get("next") ?? "/dashboard");
  }

  return (
    <form onSubmit={onSubmit} style={{ display: "flex", flexDirection: "column", gap: "0.75rem", maxWidth: 320 }}>
      <label>
        Email
        <input
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
        />
      </label>
      <label>
        Password
        <input
          type="password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />
      </label>
      {error && <p role="alert" style={{ color: "#f87171" }}>{error}</p>}
      <button type="submit" disabled={submitting}>
        {submitting ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}

export default function LoginPage() {
  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Sign in</h1>
      <Suspense fallback={null}>
        <LoginForm />
      </Suspense>
    </main>
  );
}
