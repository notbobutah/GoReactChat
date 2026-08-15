"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { authHeaders, chatClient, conversationId } from "@/lib/chat-client";

type Block = { kind: string; id: string; rendered: string };

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  blocks: Block[];
};

export function Chat() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [streaming, setStreaming] = useState("");
  const [liveBlocks, setLiveBlocks] = useState<Block[]>([]);
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState("New conversation");
  const [error, setError] = useState<string | null>(null);

  const convIdRef = useRef<string>("");
  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // Resume the tab's conversation on mount. A brand-new id has no history
  // server-side, which comes back as not_found — expected, not an error.
  useEffect(() => {
    convIdRef.current = conversationId();
    void (async () => {
      try {
        const res = await chatClient.getMessages(
          { conversationId: convIdRef.current },
          { headers: authHeaders() },
        );
        setMessages(
          res.messages.map((m) => ({
            id: m.id,
            role: m.role === "assistant" ? "assistant" : "user",
            content: m.content,
            blocks: m.blocks.map((b) => ({ kind: b.kind, id: b.id, rendered: b.rendered })),
          })),
        );

        // The title lives on the conversation row, not on the messages, so a
        // reload has to read it back or the header regresses to "New
        // conversation" for an already-titled thread.
        const list = await chatClient.listConversations({ limit: 50 }, { headers: authHeaders() });
        const current = list.conversations.find((c) => c.id === convIdRef.current);
        if (current?.title) setTitle(current.title);
      } catch {
        // No history yet.
      }
    })();
  }, []);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streaming]);

  const send = useCallback(async () => {
    const text = draft.trim();
    if (!text || busy) return;

    setDraft("");
    setError(null);
    setBusy(true);
    setMessages((prev) => [
      ...prev,
      { id: crypto.randomUUID(), role: "user", content: text, blocks: [] },
    ]);

    // Buffers live outside React state as well, so the terminal `done` event
    // commits exactly what streamed rather than a stale closure value.
    let buffer = "";
    let blocks: Block[] = [];
    setStreaming("");
    setLiveBlocks([]);

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const stream = chatClient.sendMessage(
        { conversationId: convIdRef.current, text },
        { headers: authHeaders(), signal: controller.signal },
      );

      for await (const event of stream) {
        switch (event.event.case) {
          case "token": {
            buffer += event.event.value.content;
            setStreaming(buffer);
            break;
          }
          case "discardBuffer": {
            // Recall guard rail: the model streamed a "let me check…" preamble
            // before a lookup tool. Drop it so the user sees only the answer.
            buffer = "";
            setStreaming("");
            break;
          }
          case "block": {
            const b = event.event.value;
            blocks = [...blocks, { kind: b.kind, id: b.id, rendered: b.rendered }];
            setLiveBlocks(blocks);
            break;
          }
          case "conversationTitled": {
            setTitle(event.event.value.title);
            break;
          }
          case "error": {
            setError(`${event.event.value.code}: ${event.event.value.message}`);
            break;
          }
          case "done": {
            if (buffer.length > 0 || blocks.length > 0) {
              const committed = buffer;
              const committedBlocks = blocks;
              setMessages((prev) => [
                ...prev,
                {
                  id: crypto.randomUUID(),
                  role: "assistant",
                  content: committed,
                  blocks: committedBlocks,
                },
              ]);
            }
            buffer = "";
            blocks = [];
            setStreaming("");
            setLiveBlocks([]);
            break;
          }
        }
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : String(err));
      }
      // Keep whatever streamed before the failure — the user read it already.
      if (buffer.length > 0) {
        setMessages((prev) => [
          ...prev,
          { id: crypto.randomUUID(), role: "assistant", content: buffer, blocks },
        ]);
        setStreaming("");
        setLiveBlocks([]);
      }
    } finally {
      abortRef.current = null;
      setBusy(false);
    }
  }, [draft, busy]);

  return (
    <div className="mx-auto flex h-dvh w-full max-w-3xl flex-col">
      <header className="flex items-baseline justify-between border-b border-black/10 px-6 py-4 dark:border-white/15">
        <h1 className="text-sm font-medium">{title}</h1>
        <span className="font-mono text-xs text-black/40 dark:text-white/40">lumi-go · gRPC</span>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto px-6 py-6">
        {messages.length === 0 && !streaming && (
          <p className="pt-16 text-center text-sm text-black/40 dark:text-white/40">
            Ask Lumi something to start the conversation.
          </p>
        )}

        {messages.map((m) => (
          <Bubble key={m.id} role={m.role} content={m.content} blocks={m.blocks} />
        ))}

        {(streaming || liveBlocks.length > 0) && (
          <Bubble role="assistant" content={streaming} blocks={liveBlocks} pending />
        )}

        {error && (
          <p className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">
            {error}
          </p>
        )}

        <div ref={bottomRef} />
      </div>

      <form
        className="flex gap-2 border-t border-black/10 px-6 py-4 dark:border-white/15"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <input
          className="flex-1 rounded-lg border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-black/40 dark:border-white/20 dark:focus:border-white/50"
          placeholder="Message Lumi…"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={busy}
        />
        {busy ? (
          <button
            type="button"
            className="rounded-lg border border-black/15 px-4 py-2 text-sm dark:border-white/20"
            onClick={() => abortRef.current?.abort()}
          >
            Stop
          </button>
        ) : (
          <button
            type="submit"
            className="rounded-lg bg-black px-4 py-2 text-sm text-white disabled:opacity-40 dark:bg-white dark:text-black"
            disabled={!draft.trim()}
          >
            Send
          </button>
        )}
      </form>
    </div>
  );
}

function Bubble({
  role,
  content,
  blocks,
  pending,
}: {
  role: "user" | "assistant";
  content: string;
  blocks: Block[];
  pending?: boolean;
}) {
  const isUser = role === "user";
  return (
    <div className={isUser ? "flex justify-end" : "flex justify-start"}>
      <div className="max-w-[85%] space-y-2">
        {blocks.map((b) => (
          <details
            key={b.id}
            className="rounded-lg border border-black/10 px-3 py-2 text-xs text-black/60 dark:border-white/15 dark:text-white/60"
          >
            <summary className="cursor-pointer select-none">{b.kind}</summary>
            <p className="mt-2 whitespace-pre-wrap">{b.rendered}</p>
          </details>
        ))}
        {content && (
          <div
            className={
              isUser
                ? "rounded-2xl bg-black px-4 py-2 text-sm whitespace-pre-wrap text-white dark:bg-white dark:text-black"
                : "rounded-2xl bg-black/5 px-4 py-2 text-sm whitespace-pre-wrap dark:bg-white/10"
            }
          >
            {content}
            {pending && <span className="ml-0.5 animate-pulse">▍</span>}
          </div>
        )}
      </div>
    </div>
  );
}
