"use client";

import Image from "next/image";
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
  const [title] = useState("Robert MacKay — Sr. Golang / AI Developer");
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

        // Conversations are still auto-titled server-side (the rail will want
        // it), but the header is an identity banner for a visitor evaluating
        // Robert — not a label for their own thread. So it stays fixed.
      } catch {
        // No history yet.
      }
    })();
  }, []);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streaming]);

  const send = useCallback(async (raw: string) => {
    const text = raw.trim();
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
            // Persisted server-side; the header stays the identity banner.
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
  }, [busy]);

  return (
    <div className="mx-auto flex h-dvh w-full max-w-6xl">
      <Sidebar />

      <div className="flex min-w-0 flex-1 flex-col">
      <header className="flex items-baseline justify-between border-b border-black/10 px-6 py-4 dark:border-white/15">
        <h1 className="text-sm font-medium">{title}</h1>
        <span className="font-mono text-xs text-black/40 dark:text-white/40">
          Go · gRPC · local RAG
        </span>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto px-6 py-6">
        {messages.length === 0 && !streaming && (
          <div className="pt-16">
            <p className="text-center text-sm text-black/40 dark:text-white/40">
              Ask about Robert&rsquo;s experience against this role — his background,
              what he has built, or how this application works.
            </p>
            <Suggestions onPick={(q) => void send(q)} disabled={busy} className="mt-6" />
          </div>
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

      {messages.length > 0 && (
        <Suggestions
          onPick={(q) => void send(q)}
          disabled={busy}
          className="border-t border-black/10 px-6 pt-3 dark:border-white/15"
          compact
        />
      )}

      <form
        className={`flex gap-2 px-6 py-4 ${
          messages.length > 0 ? "" : "border-t border-black/10 dark:border-white/15"
        }`}
        onSubmit={(e) => {
          e.preventDefault();
          void send(draft);
        }}
      >
        <input
          className="flex-1 rounded-lg border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-black/40 dark:border-white/20 dark:focus:border-white/50"
          placeholder="Ask about his Go experience, AI agent work, or this codebase…"
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
    </div>
  );
}

/**
 * Sidebar — the standing context a visitor needs without asking: who this is,
 * where the code lives, and what it is built from. The chat answers questions;
 * this answers the ones nobody types.
 *
 * Hidden below `lg`, where the conversation should have the full width.
 */
function Sidebar() {
  return (
    <aside className="hidden w-72 shrink-0 flex-col gap-5 border-r border-black/10 px-6 py-6 lg:flex dark:border-white/15">
      <Image
        src="/pixar-pops.png"
        alt="Robert MacKay"
        width={240}
        height={240}
        priority
        className="w-full rounded-xl object-cover"
      />

      <div className="space-y-1">
        <p className="text-sm font-medium">Robert MacKay</p>
        <p className="text-xs leading-relaxed text-black/50 dark:text-white/50">
          CTO · Software Architect · AI-Native Platforms
        </p>
      </div>

      <div className="space-y-2 text-xs">
        <p className="font-medium text-black/70 dark:text-white/70">Source code</p>
        <a
          href="https://github.com/notbobutah/GoReactChat"
          target="_blank"
          rel="noopener noreferrer"
          className="block break-all font-mono text-black/60 underline decoration-black/20 underline-offset-2 hover:text-black hover:decoration-black/60 dark:text-white/60 dark:decoration-white/25 dark:hover:text-white"
        >
          github.com/notbobutah/GoReactChat
        </a>
        <p className="leading-relaxed text-black/45 dark:text-white/45">
          This application is the working sample: a Go backend answering over
          gRPC, grounded in the résumé and the role.
        </p>
      </div>

      {/*
        The chat is the point, but a recruiter still has to forward something to
        a hiring manager and paste it into an applicant tracking system — so the
        underlying document has to be one click away, not a thing to ask for.
        `download` names the file in their Downloads folder rather than leaving
        it whatever the URL ends with.
      */}
      <div className="space-y-2 text-xs">
        <p className="font-medium text-black/70 dark:text-white/70">Résumé</p>
        <a
          href="/Robert-MacKay-Resume.pdf"
          download
          className="inline-flex items-center gap-1.5 text-black/60 underline decoration-black/20 underline-offset-2 hover:text-black hover:decoration-black/60 dark:text-white/60 dark:decoration-white/25 dark:hover:text-white"
        >
          <svg
            aria-hidden="true"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="size-3.5 shrink-0"
          >
            <path d="M8 2v8m0 0L5 7m3 3 3-3" />
            <path d="M2.5 11.5v1a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-1" />
          </svg>
          Download PDF
        </a>
      </div>

      <div className="mt-auto space-y-1 text-[11px] leading-relaxed text-black/40 dark:text-white/40">
        <p>Go · connect-go (gRPC / gRPC-Web / Connect)</p>
        <p>Next.js · React · streaming over fetch</p>
        <p>Postgres · local embeddings · in-process vector search</p>
      </div>
    </aside>
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

/**
 * Suggestions — one-click questions for a visitor who has not been told what
 * this thing can answer.
 *
 * Clicking sends immediately rather than filling the box: the reader is
 * evaluating, not composing, so a chip that only pastes text asks them to take
 * a second action for no gain. The questions map to the role's must-haves, so
 * the obvious first click lands on the requirement the posting emphasises most.
 */
const SUGGESTIONS: { label: string; question: string }[] = [
  {
    label: "Production Go",
    question: "Does he have production Go experience? Be specific about where and when.",
  },
  {
    label: "AI agents",
    question:
      "Has he personally built AI agents, or only consumed LLM APIs? What is the evidence?",
  },
  {
    label: "Fit for this role",
    question:
      "How does his background line up with the must-have skills in this job description?",
  },
  {
    label: "How this app works",
    question:
      "How does this application work — the architecture, the streaming, and the retrieval?",
  },
  {
    label: "Kubernetes & cloud",
    question: "What is his experience with Kubernetes, containers and cloud infrastructure?",
  },
  {
    label: "Recent work",
    question: "What has he designed and shipped most recently?",
  },
];

function Suggestions({
  onPick,
  disabled,
  className = "",
  compact = false,
}: {
  onPick: (question: string) => void;
  disabled?: boolean;
  className?: string;
  compact?: boolean;
}) {
  return (
    <div className={`flex flex-wrap justify-center gap-2 ${className}`}>
      {SUGGESTIONS.map((s) => (
        <button
          key={s.label}
          type="button"
          onClick={() => onPick(s.question)}
          disabled={disabled}
          title={s.question}
          className={`rounded-full border border-black/15 text-black/70 transition hover:border-black/40 hover:text-black disabled:opacity-40 dark:border-white/20 dark:text-white/70 dark:hover:border-white/50 dark:hover:text-white ${
            compact ? "px-3 py-1 text-[11px]" : "px-3.5 py-1.5 text-xs"
          }`}
        >
          {s.label}
        </button>
      ))}
    </div>
  );
}
