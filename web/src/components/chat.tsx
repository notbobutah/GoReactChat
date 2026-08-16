"use client";

import Image from "next/image";
import { useCallback, useEffect, useRef, useState } from "react";

import { NewsPanel } from "@/components/news-panel";
import { authHeaders, chatClient, conversationId, resetConversation } from "@/lib/chat-client";

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
  const scrollRef = useRef<HTMLDivElement>(null);
  // Whether the view is following the newest output. False once the reader
  // scrolls away from the bottom, which is the whole point: a turn streams for
  // many seconds, and pinning the view to the end made the transcript
  // unreadable while it was still being written.
  const [following, setFollowing] = useState(true);

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

  // Jump the container itself, instantly.
  //
  // Not scrollIntoView with smooth behaviour: a smooth scroll fires onScroll
  // repeatedly on the way down, each time reporting a distance that still looks
  // like "the reader has scrolled away" — so following would switch itself off
  // mid-animation, and the button to re-attach would cancel itself the moment
  // it was pressed. An instant jump fires one event, at the destination.
  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  // Auto-scroll only while the reader is at the end. Reading back through a
  // long answer used to be impossible mid-turn — every token yanked the view
  // down again.
  useEffect(() => {
    if (!following) return;
    scrollToBottom();
  }, [messages, streaming, liveBlocks, following, scrollToBottom]);

  // A small tolerance rather than an exact match: smooth scrolling and
  // sub-pixel heights mean "at the bottom" is rarely exactly zero, and a strict
  // test would drop out of follow mode on its own scrolling.
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    setFollowing(distanceFromBottom < 80);
  }, []);

  const jumpToLatest = useCallback(() => {
    scrollToBottom();
    setFollowing(true);
  }, [scrollToBottom]);

  /**
   * Start a blank thread.
   *
   * Only the id changes here — no conversation is created server-side, because
   * the service creates the row on the first message of a turn. So a reset that
   * is never followed by a question leaves nothing behind, and the new thread
   * begins when the agent actually runs.
   *
   * Any in-flight turn is aborted first: its tokens would otherwise keep
   * arriving and land in the conversation the user just walked away from.
   */
  const startNew = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    convIdRef.current = resetConversation();
    setMessages([]);
    setStreaming("");
    setLiveBlocks([]);
    setDraft("");
    setError(null);
    setBusy(false);
  }, []);

  const send = useCallback(async (raw: string) => {
    const text = raw.trim();
    if (!text || busy) return;

    setDraft("");
    setError(null);
    setBusy(true);
    // Asking a question is a request to see its answer, so re-attach to the
    // bottom even if the reader had scrolled away.
    setFollowing(true);
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
    // Column on narrow screens, row from lg. The rail used to be `hidden` below
    // lg, which took the résumé download and the agent panel off the page
    // entirely — on a phone there was no way to reach either, and this link gets
    // opened on phones.
    <div className="mx-auto flex h-dvh w-full max-w-6xl flex-col lg:flex-row">
      <Sidebar />

      <div className="flex min-w-0 flex-1 flex-col">
      <header className="flex items-baseline justify-between border-b border-black/10 px-6 py-4 dark:border-white/15">
        <h1 className="text-sm font-medium">{title}</h1>
        <span className="font-mono text-xs text-black/40 dark:text-white/40">
          Go · gRPC · local RAG
        </span>
      </header>

      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="relative flex-1 space-y-4 overflow-y-auto px-6 py-6"
      >
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

      </div>

      {/* Only while detached: a reader who has scrolled up needs to know output
          is still arriving, and needs one click back. Without it, leaving the
          bottom is a one-way trip until the turn ends. */}
      {!following && (messages.length > 0 || streaming) && (
        <div className="pointer-events-none flex justify-center px-6">
          <button
            type="button"
            onClick={jumpToLatest}
            className="pointer-events-auto -mt-2 inline-flex items-center gap-1.5 rounded-full border border-black/15 bg-white/90 px-3 py-1 text-[11px] text-black/70 shadow-sm backdrop-blur transition hover:border-black/40 hover:text-black dark:border-white/20 dark:bg-black/70 dark:text-white/70 dark:hover:border-white/50 dark:hover:text-white"
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 14 14"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="size-3 shrink-0"
            >
              <path d="M7 2.5v9m0 0L3.5 8M7 11.5 10.5 8" />
            </svg>
            {busy ? "Still writing — jump to latest" : "Jump to latest"}
          </button>
        </div>
      )}

      {messages.length > 0 && (
        <div className="flex flex-wrap items-center justify-center gap-2 border-t border-black/10 px-6 pt-3 dark:border-white/15">
          {/* Not a prompt, so it is set apart from the prompt chips rather than
              mixed in among them — clicking it does something to the page, not
              to the conversation. */}
          <button
            type="button"
            onClick={startNew}
            title="Clear this conversation and start a new one"
            className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-black/20 px-3 py-1 text-[11px] text-black/55 transition hover:border-black/45 hover:text-black dark:border-white/25 dark:text-white/55 dark:hover:border-white/50 dark:hover:text-white"
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 14 14"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="size-3 shrink-0"
            >
              <path d="M12 7a5 5 0 1 1-1.6-3.7" />
              <path d="M12 1.5V4H9.5" />
            </svg>
            New conversation
          </button>

          <Suggestions onPick={(q) => void send(q)} disabled={busy} compact />
        </div>
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
    // h-dvh + overflow-y-auto so the rail scrolls inside itself. The page is a
    // fixed-height app shell; once the news panel made this column taller than
    // the viewport, without this the whole document scrolled and took the chat
    // input off screen with it.
    // Below lg the rail moves under the conversation (order-last) with a bounded
    // height, so it is reachable without pushing the chat off the first screen.
    // From lg it is the full-height left column it has always been.
    <aside className="order-last flex max-h-72 w-full shrink-0 flex-col gap-5 overflow-y-auto border-t border-black/10 px-6 py-6 lg:order-none lg:h-dvh lg:max-h-none lg:w-72 lg:border-t-0 lg:border-r dark:border-white/15">
      <Image
        src="/pixar-pops.png"
        alt="Robert MacKay"
        width={240}
        height={240}
        priority
        // Capped. At full width this is ~290px of the rail before anything
        // else, which pushed the live agent panel below the fold on a normal
        // laptop — the reason it read as missing rather than as further down.
        className="max-h-44 w-full rounded-xl object-cover object-top"
      />

      <div className="space-y-1">
        <p className="text-sm font-medium">Robert MacKay</p>
        <p className="text-xs leading-relaxed text-black/50 dark:text-white/50">
          CTO · Software Architect · AI-Native Platforms
        </p>
      </div>

      {/* Above the static blurbs: it is the only part of this rail that changes,
          and it is what the "AI agents" prompt asks the reader to look at. */}
      <NewsPanel />

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
    // Names the xAI agent outright. "The agent running in this application"
    // was ambiguous — there are two, and the model reasonably led with the one
    // answering the question, pushing the xAI agent to the second section. The
    // reader can see that agent's output in the panel beside the chat, so it
    // is the claim to open with.
    question:
      "First, and in detail: the Ecosystem watch panel on this page is produced by an agent whose tool loop executes inside xAI, not in this application. Explain that — the single Responses API request, what runs server-side, and why there is no local agent loop or extra service. Only after that, cover the hand-written Go loop behind this chat and his wider agent work.",
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
    // Asks for the checkable artifact first. The professional Kubernetes work
    // is commercial and private, so a question phrased purely as "experience"
    // draws answers a reader cannot verify — while this repository's own
    // manifests are sitting there, public, and go unmentioned.
    question:
      "Start with the Kubernetes manifests in this repository's deploy/ directory — what they contain and what they show. Then cover his wider Kubernetes, container and cloud infrastructure experience.",
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
