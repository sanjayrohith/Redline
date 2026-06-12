"use client";

import { FormEvent, useEffect, useState } from "react";
import { useChatStream } from "@/lib/useChatStream";
import { renderMarkdown } from "@/lib/markdown";

function ComparePane({ model, prompt, generation }: { model: string; prompt: string; generation: number }) {
  const { state, send, stop } = useChatStream();

  // generation changes on every new submission - re-running the effect
  // each time re-issues this pane's own independent request.
  useEffect(() => {
    if (generation > 0 && prompt.trim()) {
      send(model, [{ role: "user", content: prompt }]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [generation]);

  return (
    <div style={{ flex: 1, minWidth: 0, border: "1px solid #333", padding: "1rem" }}>
      <h3 style={{ marginTop: 0 }}>{model}</h3>
      <div style={{ display: "flex", gap: "1rem", fontSize: "0.85rem", opacity: 0.8 }}>
        <span>TTFT: {state.ttftMs !== null ? `${Math.round(state.ttftMs)}ms` : "—"}</span>
        <span>Total: {state.totalMs !== null ? `${Math.round(state.totalMs)}ms` : "—"}</span>
        {state.streaming && (
          <button type="button" onClick={stop} style={{ marginLeft: "auto" }}>
            Stop
          </button>
        )}
      </div>
      {state.error && <p role="alert" style={{ color: "#f87171" }}>{state.error}</p>}
      <div style={{ marginTop: "0.5rem", wordBreak: "break-word" }}>{renderMarkdown(state.content)}</div>
    </div>
  );
}

export default function ComparePage() {
  const [models, setModels] = useState(["mock-model", "mock-model-b"]);
  const [prompt, setPrompt] = useState("");
  const [submittedPrompt, setSubmittedPrompt] = useState("");
  const [generation, setGeneration] = useState(0);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmittedPrompt(prompt);
    setGeneration((g) => g + 1);
  }

  return (
    <main style={{ padding: "3rem", maxWidth: 1100, margin: "0 auto" }}>
      <h1>Compare models</h1>

      <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem" }}>
        {models.map((m, i) => (
          <input
            key={i}
            value={m}
            onChange={(e) => setModels(models.map((v, j) => (j === i ? e.target.value : v)))}
            placeholder={`Model ${i + 1}`}
          />
        ))}
        <button type="button" onClick={() => setModels([...models, ""])}>
          + Add model
        </button>
      </div>

      <form onSubmit={onSubmit} style={{ display: "flex", gap: "0.5rem", marginBottom: "1.5rem" }}>
        <input
          type="text"
          required
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Prompt sent to every model above"
          style={{ flex: 1 }}
        />
        <button type="submit">Run</button>
      </form>

      <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap" }}>
        {models
          .filter((m) => m.trim())
          .map((m) => (
            <ComparePane key={m} model={m} prompt={submittedPrompt} generation={generation} />
          ))}
      </div>
    </main>
  );
}
