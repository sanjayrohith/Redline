"use client";

import { useCallback, useRef, useState } from "react";
import type { ChatCompletionChunk } from "@/lib/api/types";

export interface ChatStreamState {
  content: string;
  streaming: boolean;
  error: string | null;
  ttftMs: number | null;
  totalMs: number | null;
}

const initialState: ChatStreamState = {
  content: "",
  streaming: false,
  error: null,
  ttftMs: null,
  totalMs: null,
};

// Drives one independent SSE chat stream against the dashboard chat
// completions proxy, tracking time-to-first-token and total generation
// time client-side from wall-clock timestamps around the fetch itself -
// each caller gets its own AbortController, so N concurrent instances
// (e.g. a side-by-side model comparison) never interfere with each other.
export function useChatStream() {
  const [state, setState] = useState<ChatStreamState>(initialState);
  const abortRef = useRef<AbortController | null>(null);

  const send = useCallback(async (model: string, messages: { role: string; content: string }[]) => {
    setState({ ...initialState, streaming: true });
    const startedAt = performance.now();
    let firstTokenAt: number | null = null;

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const res = await fetch("/api/dashboard/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model, messages, stream: true }),
        signal: controller.signal,
      });
      if (!res.ok || !res.body) throw new Error("request failed");

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let text = "";

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
          if (delta && firstTokenAt === null) {
            firstTokenAt = performance.now();
          }
          text += delta;
          setState((prev) => ({
            ...prev,
            content: text,
            ttftMs: firstTokenAt !== null ? firstTokenAt - startedAt : prev.ttftMs,
          }));
        }
      }

      setState((prev) => ({ ...prev, streaming: false, totalMs: performance.now() - startedAt }));
    } catch (err) {
      if ((err as Error).name !== "AbortError") {
        setState((prev) => ({ ...prev, streaming: false, error: "Generation failed." }));
      } else {
        setState((prev) => ({ ...prev, streaming: false }));
      }
    } finally {
      abortRef.current = null;
    }
  }, []);

  const stop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  return { state, send, stop };
}
