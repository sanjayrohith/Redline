"use client";

import { FormEvent, useRef, useState } from "react";
import type { ChatCompletionChunk } from "@/lib/api/types";
import { renderMarkdown } from "@/lib/markdown";

interface Message {
  role: "user" | "assistant";
  content: string;
}

export default function ChatPlaygroundPage() {
  const [model, setModel] = useState("mock-model");
  const [input, setInput] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  async function send(e: FormEvent) {
    e.preventDefault();
    if (!input.trim() || streaming) return;

    const history = [...messages, { role: "user" as const, content: input }];
    setMessages([...history, { role: "assistant", content: "" }]);
    setInput("");
    setError(null);
    setStreaming(true);

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const res = await fetch("/api/dashboard/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model, messages: history, stream: true }),
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        throw new Error("request failed");
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let assistantText = "";

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        const frames = buffer.split("\n\n");
        buffer = frames.pop() ?? "";
        for (const frame of frames) {
          const line = frame.trim();
          if (!line.startsWith("data:")) continue;
          const data = line.slice(5).trim();
          if (data === "[DONE]") continue;

          const chunk: ChatCompletionChunk = JSON.parse(data);
          const delta = chunk.choices[0]?.delta.content ?? "";
          assistantText += delta;
          setMessages([...history, { role: "assistant", content: assistantText }]);
        }
      }
    } catch (err) {
      if ((err as Error).name !== "AbortError") {
        setError("Generation failed.");
      }
    } finally {
      setStreaming(false);
      abortRef.current = null;
    }
  }

  function stop() {
    abortRef.current?.abort();
  }

  return (
    <main style={{ padding: "3rem", maxWidth: 720, margin: "0 auto" }}>
      <h1>Chat playground</h1>

      <label>
        Model{" "}
        <input value={model} onChange={(e) => setModel(e.target.value)} disabled={streaming} />
      </label>

      <div style={{ marginTop: "1.5rem", display: "flex", flexDirection: "column", gap: "1rem" }}>
        {messages.map((m, i) => (
          <div key={i} style={{ opacity: m.role === "user" ? 0.85 : 1 }}>
            <strong>{m.role === "user" ? "You" : "Assistant"}</strong>
            <div>{renderMarkdown(m.content)}</div>
          </div>
        ))}
      </div>

      {error && <p role="alert" style={{ color: "#f87171" }}>{error}</p>}

      <form onSubmit={send} style={{ display: "flex", gap: "0.5rem", marginTop: "1.5rem" }}>
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Say something…"
          disabled={streaming}
          style={{ flex: 1 }}
        />
        {streaming ? (
          <button type="button" onClick={stop}>
            Stop
          </button>
        ) : (
          <button type="submit">Send</button>
        )}
      </form>
    </main>
  );
}
