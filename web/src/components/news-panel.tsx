"use client";

import { useEffect, useRef, useState } from "react";

import { ScanState, type NewsDigest } from "@/gen/lumi/chat/v1/chat_pb";
import { authHeaders, chatClient } from "@/lib/chat-client";

/**
 * The receiving end of the news agent.
 *
 * This is a subscription, not a fetch. A scan runs a dozen or more web searches
 * on xAI's servers and takes about a minute, which is far longer than a request
 * can wait — so the server holds a stream open and pushes: current state on
 * connect, a state change when a scan starts, the digest when it lands.
 *
 * The consequence worth noticing is that the panel never has to poll and never
 * has a loading state it invented. Every state it renders is one the server
 * actually reported.
 */
export function NewsPanel() {
  const [digest, setDigest] = useState<NewsDigest | null>(null);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Distinguishes "connecting" from "connected and there is genuinely nothing
  // yet", which look identical without it.
  const [connected, setConnected] = useState(false);
  // Closed by default. The rail is context beside the conversation, not the
  // conversation — six expanded news items pushed everything else out of view,
  // which is how the panel came to be hard to find in the first place. The
  // count in the label is what makes a closed panel worth opening.
  const [open, setOpen] = useState(false);

  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    abortRef.current = controller;

    // Reconnect loop. A subscription is open-ended, and the ingress closes an
    // idle connection at ten minutes — so the stream ending is the normal case,
    // not a failure. Without this the panel silently stops updating after the
    // first timeout and looks fine while being wrong.
    //
    // Reconnecting is cheap and does not re-trigger a scan: the server refuses
    // one until the interval elapses, and the fresh snapshot carries the
    // current digest.
    void (async () => {
      let backoffMs = 1_000;

      while (!controller.signal.aborted) {
        try {
          for await (const ev of chatClient.watchNews(
            {},
            { headers: authHeaders(), signal: controller.signal },
          )) {
            setConnected(true);
            backoffMs = 1_000; // a delivered event proves the connection is good
            switch (ev.event.case) {
              case "snapshot":
                setScanning(ev.event.value.state === ScanState.SCANNING);
                if (ev.event.value.digest) setDigest(ev.event.value.digest);
                break;
              case "state":
                setScanning(ev.event.value === ScanState.SCANNING);
                break;
              case "digest":
                setDigest(ev.event.value);
                setError(null);
                break;
              case "error":
                // The previous digest stays on screen: a failed refresh should
                // not blank out results that are still perfectly readable.
                setError(ev.event.value.message);
                break;
            }
          }
        } catch {
          // Includes the ordinary unmount abort, and an Unimplemented reply
          // when the deployment has no agent key. Neither deserves a visible
          // error — the panel just goes quiet.
          if (controller.signal.aborted) return;
          setConnected(false);
        }

        if (controller.signal.aborted) return;
        await sleep(backoffMs, controller.signal);
        // Capped exponential backoff, so a server that is down is retried
        // every half minute rather than in a hot loop.
        backoffMs = Math.min(backoffMs * 2, 30_000);
      }
    })();

    return () => controller.abort();
  }, []);

  // Nothing to show and nothing happening: most likely a deployment with the
  // watcher disabled. Render nothing rather than an empty box.
  if (!digest && !scanning && !connected) return null;

  const count = digest?.items.length ?? 0;

  return (
    <section className="text-xs">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls="ecosystem-watch-body"
        className="flex w-full items-center gap-2 text-left"
      >
        <svg
          aria-hidden="true"
          viewBox="0 0 12 12"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`size-3 shrink-0 text-black/40 transition-transform dark:text-white/40 ${
            open ? "rotate-90" : ""
          }`}
        >
          <path d="M4 2.5 8 6l-4 3.5" />
        </svg>

        <span className="font-medium text-black/70 dark:text-white/70">Ecosystem watch</span>

        {count > 0 && (
          <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] leading-none font-medium text-black/50 tabular-nums dark:bg-white/10 dark:text-white/50">
            {count}
          </span>
        )}

        {/* The live state stays visible while collapsed — a scan in progress is
            the one thing worth noticing without opening anything. */}
        <span className="ml-auto text-black/35 dark:text-white/35">
          {scanning ? (
            <span className="inline-flex items-center gap-1.5">
              <span className="size-1.5 animate-pulse rounded-full bg-current" />
              scanning
            </span>
          ) : digest ? (
            relative(digest.generatedAtUnix)
          ) : null}
        </span>
      </button>

      {open && (
        <div id="ecosystem-watch-body" className="mt-3 space-y-3">
          <p className="leading-relaxed text-black/45 dark:text-white/45">
            A research agent watching Go, gRPC and Protobuf. Its tool loop runs on
            xAI&rsquo;s servers — this app subscribes and is pushed the result.
          </p>

          {error && (
            <p className="leading-relaxed text-amber-700/80 dark:text-amber-500/80">{error}</p>
          )}

          {!digest && scanning && (
            <p className="text-black/40 dark:text-white/40">First scan running — about a minute.</p>
          )}

          {digest && (
            <ul className="space-y-3">
              {digest.items.map((item) => (
                <li key={item.id} className="space-y-1">
                  <a
                    href={item.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block font-medium text-black/75 underline decoration-black/15 underline-offset-2 hover:decoration-black/60 dark:text-white/75 dark:decoration-white/20 dark:hover:decoration-white/60"
                  >
                    {item.headline}
                  </a>
                  <p className="leading-relaxed text-black/45 dark:text-white/45">{item.summary}</p>
                  <p className="text-[10px] uppercase tracking-wide text-black/30 dark:text-white/30">
                    {item.topic} · {item.source}
                    {item.published ? ` · ${item.published}` : ""}
                  </p>
                </li>
              ))}
            </ul>
          )}

          {digest && digest.toolCalls > 0 && (
            // Shown on purpose. The agent bills per server-side tool call, and an
            // autonomous agent whose cost is invisible is one nobody notices
            // running away.
            <p className="text-[10px] text-black/30 dark:text-white/30">
              {digest.toolCalls} searches · {digest.totalTokens.toLocaleString()} tokens
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/** Resolves early when the effect is torn down, so unmounting never waits out a backoff. */
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}

/** Coarse on purpose: the digest refreshes every few hours, so minutes would be false precision. */
function relative(unix: bigint | number): string {
  const seconds = Math.floor(Date.now() / 1000) - Number(unix);
  if (seconds < 3600) return "just now";
  const hours = Math.floor(seconds / 3600);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
