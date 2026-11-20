"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";

import {
  STAGES,
  cardUrl,
  describe,
  fetchFeed,
  label,
  ms,
  project,
  type CardFile,
  type FeedIndex,
  type Plan,
  type View,
  type Who,
} from "@/lib/pipeline-feed";

/**
 * A window onto the agentic pipeline: tracker cards, the agents working them,
 * and every event in between.
 *
 * Two modes over one event list. Live renders the fold over the whole log and
 * refreshes from the published snapshot. Replay renders the fold over a growing
 * prefix, so a visitor can watch a finished run unfold step by step without the
 * pipeline running (and without anyone paying for it).
 */
export function PipelineView({ feedUrl }: { feedUrl: string }) {
  const [index, setIndex] = useState<FeedIndex | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [card, setCard] = useState<CardFile | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [mode, setMode] = useState<"live" | "replay">("live");
  const [cursor, setCursor] = useState(0); // index into `steps` while replaying
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);

  // The index changes when a card is added or moves; poll it slowly. The CDN in
  // front of the feed caches for minutes anyway, so faster polling buys nothing.
  useEffect(() => {
    const controller = new AbortController();
    const load = async () => {
      try {
        const idx = await fetchFeed<FeedIndex>(feedUrl, controller.signal);
        setIndex(idx);
        setError(null);
        setSelected((cur) => cur ?? idx.cards[0]?.key ?? null);
      } catch (err) {
        if (!controller.signal.aborted) setError(String(err));
      }
    };
    void load();
    const timer = setInterval(load, 30_000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [feedUrl]);

  const summary = index?.cards.find((c) => c.key === selected) ?? null;

  // The selected card's full history: refetched when the index says it moved.
  useEffect(() => {
    if (!summary) return;
    const controller = new AbortController();
    void (async () => {
      try {
        setCard(await fetchFeed<CardFile>(cardUrl(feedUrl, summary.file), controller.signal));
      } catch (err) {
        if (!controller.signal.aborted) setError(String(err));
      }
    })();
    return () => controller.abort();
  }, [feedUrl, summary?.file, summary?.events, summary?.updated, summary]);

  // Replay steps: every distinct instant at which something happened.
  const steps = useMemo(() => {
    if (!card) return [];
    const all = [...card.events.map((e) => ms(e.time)), ...card.telemetry.map((t) => ms(t.time))];
    return [...new Set(all)].sort((a, b) => a - b);
  }, [card]);

  // One step per tick. Steps are distinct instants, so long idle stretches in
  // the real run (an agent thinking for a minute) collapse to a single tick.
  const atEnd = cursor >= steps.length - 1;
  useEffect(() => {
    if (!playing || atEnd) return;
    const timer = setInterval(() => setCursor((c) => Math.min(c + 1, steps.length - 1)), 600 / speed);
    return () => clearInterval(timer);
  }, [playing, atEnd, speed, steps.length]);

  const until = mode === "replay" && steps.length ? steps[Math.min(cursor, steps.length - 1)] : null;
  const view = useMemo(() => (card ? project(card.events, card.telemetry, until) : null), [card, until]);
  const visible = useMemo(
    () => (card ? card.events.filter((e) => until === null || ms(e.time) <= until) : []),
    [card, until],
  );

  const startReplay = () => {
    setMode("replay");
    setCursor(0);
    setPlaying(true);
  };

  return (
    <main className="mx-auto w-full max-w-6xl space-y-8 px-6 py-10">
      <Header index={index} />
      <HowItWorks index={index} />
      <ProjectLinks />

      {error && <p className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">Feed unavailable: {error}</p>}

      <div className="grid gap-6 lg:grid-cols-[15rem_1fr]">
        <aside className="space-y-2">
          <h2 className="text-xs font-medium tracking-wide text-black/50 uppercase dark:text-white/50">Cards</h2>
          {index?.cards.length === 0 && <p className="text-sm text-black/50 dark:text-white/50">No cards yet.</p>}
          {index?.cards.map((c) => (
            <button
              key={c.key}
              onClick={() => {
                setSelected(c.key);
                setMode("live");
                setPlaying(false);
              }}
              className={`w-full rounded-lg border px-3 py-2 text-left text-sm transition ${
                c.key === selected
                  ? "border-black/40 bg-black/[0.04] dark:border-white/40 dark:bg-white/[0.06]"
                  : "border-black/10 hover:border-black/30 dark:border-white/15 dark:hover:border-white/35"
              }`}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs">{c.key}</span>
                <StatusPill status={c.status} />
              </div>
              <div className="mt-1 line-clamp-2 text-black/70 dark:text-white/70">{c.summary}</div>
              <div className="mt-1 text-[11px] text-black/40 dark:text-white/40">
                {c.events} events · ${c.costUsd.toFixed(2)}
              </div>
            </button>
          ))}
        </aside>

        {card && view ? (
          <section className="min-w-0 space-y-6">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <div className="font-mono text-xs text-black/50 dark:text-white/50">
                  {card.card.key} · {card.card.issueType} · sponsor {card.card.sponsor}
                </div>
                <h2 className="mt-1 text-xl font-medium">{card.card.summary}</h2>
              </div>
              <ReplayBar
                mode={mode}
                playing={playing}
                speed={speed}
                cursor={cursor}
                steps={steps.length}
                until={until}
                onLive={() => {
                  setMode("live");
                  setPlaying(false);
                }}
                onReplay={startReplay}
                onToggle={() => {
                  if (mode === "live" || atEnd) startReplay();
                  else setPlaying((p) => !p);
                }}
                onSpeed={setSpeed}
                onSeek={(c) => {
                  setMode("replay");
                  setPlaying(false);
                  setCursor(c);
                }}
              />
            </div>

            <Stages view={view} />

            <div className="grid gap-6 md:grid-cols-2">
              <CardPanel card={card} />
              <AgentsPanel view={view} />
            </div>

            <PlanPanel view={view} />
            <EventStream events={visible} live={mode === "live"} />
          </section>
        ) : (
          <p className="text-sm text-black/50 dark:text-white/50">{index ? "Select a card." : "Loading the pipeline feed…"}</p>
        )}
      </div>

    </main>
  );
}

/**
 * Where everything lives. Static on purpose: these are the fixed parts of the
 * demo, and a visitor should be able to find the code without the feed loading.
 */
const PROJECT_LINKS = [
  {
    title: "agentic-pipeline",
    href: "https://github.com/notbobutah/agentic-pipeline",
    kind: "Pipeline",
    text: "The orchestrator: event log, handlers, agent sandboxes and the CLI that runs cards, plus the full design docs.",
  },
  {
    title: "Design docs",
    href: "https://github.com/notbobutah/agentic-pipeline/tree/main/docs",
    kind: "Docs",
    text: "Event model, catalog, handlers, work plane and integration testing: what this page is a slice of.",
  },
  {
    title: "PGDataLayer",
    href: "https://github.com/notbobutah/PGDataLayer",
    kind: "Target repo",
    text: "Sample data and JSON Schemas published as an npm package. Agents write its data, validators and tests.",
  },
  {
    title: "GoReactChat",
    href: "https://github.com/notbobutah/GoReactChat",
    kind: "Target repo",
    text: "This site: a Go + Next.js app. Agents add pages to its web client; its CI verifies every agent commit.",
  },
  {
    title: "rob.expona.ai",
    href: "/",
    kind: "Live site",
    text: "The interactive résumé chat this page belongs to. Pages built by the pipeline deploy here.",
  },
  {
    title: "Pipeline feed",
    href: "https://github.com/notbobutah/agentic-pipeline/tree/feed",
    kind: "Data",
    text: "The JSON snapshots of the event log that this page reads. Published by the orchestrator; read-only here.",
  },
] as const;

function ProjectLinks() {
  return (
    <Panel title="Project links">
      <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {PROJECT_LINKS.map((l) => {
          const external = l.href.startsWith("http");
          const body = (
            <>
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium">{l.title}</span>
                <span className="rounded-full bg-black/[0.06] px-2 py-0.5 text-[10px] text-black/60 dark:bg-white/10 dark:text-white/60">
                  {l.kind}
                </span>
              </div>
              <p className="mt-1 text-xs leading-relaxed text-black/60 dark:text-white/60">{l.text}</p>
            </>
          );
          const cls =
            "block h-full rounded-lg border border-black/10 px-3 py-2 transition hover:border-black/35 dark:border-white/15 dark:hover:border-white/40";
          return (
            <li key={l.title}>
              {external ? (
                <a href={l.href} target="_blank" rel="noopener noreferrer" className={cls}>
                  {body}
                </a>
              ) : (
                <Link href={l.href} className={cls}>
                  {body}
                </Link>
              )}
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

function Header({ index }: { index: FeedIndex | null }) {
  return (
    <header className="space-y-3">
      <Link href="/" className="text-xs text-black/50 hover:text-black dark:text-white/50 dark:hover:text-white">
        ← back to the chat
      </Link>
      <h1 className="text-2xl font-medium">Agentic pipeline</h1>
      <p className="max-w-3xl text-sm leading-relaxed text-black/70 dark:text-white/70">
        A developer assigns a tracker card and gives some guidance. An agent reads the repositories and proposes an implementation
        plan. Nothing is written until the developer approves that exact plan. Agents then write the code and the tests in
        isolated sandboxes, the build pipeline verifies every commit, and failures flow back to the agents until the change is
        ready for human review. Every step below is an event in one log, and that log is all this page reads.
      </p>
      <div className="flex flex-wrap gap-2 text-[11px]">
        {["1 · Developer assigns + guides", "2 · Developer approves the plan", "3 · Developer reviews the PR"].map((t) => (
          <span key={t} className="rounded-full border border-black/15 px-3 py-1 text-black/70 dark:border-white/20 dark:text-white/70">
            {t}
          </span>
        ))}
        <span className="rounded-full border border-dashed border-black/20 px-3 py-1 text-black/50 dark:border-white/25 dark:text-white/50">
          everything else is automated
        </span>
      </div>
      {index && (
        <p className="text-[11px] text-black/40 dark:text-white/40">
          Snapshot {new Date(index.generatedAt).toLocaleString()} · updates every few minutes ·{" "}
          <a className="underline" href={index.pipeline.repo}>
            source
          </a>
        </p>
      )}
    </header>
  );
}

const STATUS_TONE: Record<string, string> = {
  "plan-review": "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  planning: "bg-sky-500/15 text-sky-800 dark:text-sky-300",
  implementing: "bg-violet-500/15 text-violet-800 dark:text-violet-300",
  done: "bg-emerald-500/15 text-emerald-800 dark:text-emerald-300",
  blocked: "bg-red-500/15 text-red-800 dark:text-red-300",
  rejected: "bg-red-500/15 text-red-800 dark:text-red-300",
  cancelled: "bg-black/10 text-black/60 dark:bg-white/10 dark:text-white/60",
};

function StatusPill({ status }: { status: string }) {
  return (
    <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${STATUS_TONE[status] ?? "bg-black/[0.06] text-black/60 dark:bg-white/10 dark:text-white/60"}`}>
      {status.replace("-", " ")}
    </span>
  );
}

function ReplayBar(props: {
  mode: "live" | "replay";
  playing: boolean;
  speed: number;
  cursor: number;
  steps: number;
  until: number | null;
  onLive: () => void;
  onReplay: () => void;
  onToggle: () => void;
  onSpeed: (s: number) => void;
  onSeek: (c: number) => void;
}) {
  const btn = "rounded-lg border border-black/15 px-3 py-1.5 text-xs transition hover:border-black/40 dark:border-white/20 dark:hover:border-white/50";
  return (
    <div className="flex flex-wrap items-center gap-2">
      <button className={`${btn} ${props.mode === "live" ? "bg-black text-white dark:bg-white dark:text-black" : ""}`} onClick={props.onLive}>
        Live
      </button>
      <button className={btn} onClick={props.onToggle}>
        {props.mode === "replay" && props.playing && props.cursor < props.steps - 1 ? "Pause" : props.mode === "replay" ? "Play" : "Replay this run"}
      </button>
      {props.mode === "replay" && (
        <>
          <button className={btn} onClick={props.onReplay}>
            Restart
          </button>
          <select
            className={`${btn} bg-transparent`}
            value={props.speed}
            onChange={(e) => props.onSpeed(Number(e.target.value))}
            aria-label="Replay speed"
          >
            {[1, 3, 10].map((s) => (
              <option key={s} value={s}>
                {s}×
              </option>
            ))}
          </select>
          <input
            type="range"
            min={0}
            max={Math.max(props.steps - 1, 0)}
            value={props.cursor}
            onChange={(e) => props.onSeek(Number(e.target.value))}
            className="w-40"
            aria-label="Replay position"
          />
          <span className="font-mono text-[11px] text-black/50 dark:text-white/50">
            {props.until ? new Date(props.until).toLocaleTimeString() : ""}
          </span>
        </>
      )}
    </div>
  );
}

function Stages({ view }: { view: View }) {
  const current = STAGES.findIndex((s) => s.id === view.stage);
  return (
    <ol className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
      {STAGES.map((s, i) => {
        const state = i < current ? "done" : i === current ? "current" : "todo";
        const waiting = state === "current" && s.id === "approval" && view.waitingOnSponsor;
        return (
          <li
            key={s.id}
            className={`rounded-lg border px-3 py-2 text-xs ${
              state === "current"
                ? "border-black/50 dark:border-white/60"
                : state === "done"
                  ? "border-black/10 text-black/60 dark:border-white/15 dark:text-white/60"
                  : "border-dashed border-black/10 text-black/35 dark:border-white/15 dark:text-white/35"
            }`}
          >
            <div className="font-mono text-[10px]">{state === "done" ? "✓" : i + 1}</div>
            <div className="mt-0.5 font-medium">{s.label}</div>
            {waiting && <div className="mt-1 animate-pulse text-[10px] text-amber-700 dark:text-amber-300">waiting on the sponsor</div>}
          </li>
        );
      })}
    </ol>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-black/10 p-4 dark:border-white/15">
      <h3 className="mb-3 text-xs font-medium tracking-wide text-black/50 uppercase dark:text-white/50">{title}</h3>
      {children}
    </div>
  );
}

function CardPanel({ card }: { card: CardFile }) {
  const c = card.card;
  return (
    <Panel title="The card">
      <p className="text-sm text-black/80 dark:text-white/80">{c.description}</p>
      <ul className="mt-3 space-y-1 text-xs text-black/70 dark:text-white/70">
        {c.acceptanceCriteria.map((ac) => (
          <li key={ac}>{ac}</li>
        ))}
      </ul>
      {c.guidance && c.guidance.length > 0 && (
        <div className="mt-3 space-y-1 border-t border-black/10 pt-3 text-xs dark:border-white/15">
          <div className="font-medium text-black/60 dark:text-white/60">Developer guidance</div>
          {c.guidance.map((g) => (
            <p key={g} className="text-black/60 dark:text-white/60">
              “{g}”
            </p>
          ))}
        </div>
      )}
    </Panel>
  );
}

function AgentsPanel({ view }: { view: View }) {
  return (
    <Panel title="Agents">
      {view.tasks.length === 0 && <p className="text-xs text-black/50 dark:text-white/50">No agent has run yet.</p>}
      <ul className="space-y-3">
        {view.tasks.map((t) => (
          <li key={t.taskId} className="text-xs">
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{label(t.agent)}</span>
              <span
                className={
                  t.state === "running"
                    ? "animate-pulse text-sky-700 dark:text-sky-300"
                    : t.state === "done"
                      ? "text-emerald-700 dark:text-emerald-300"
                      : "text-red-700 dark:text-red-300"
                }
              >
                {t.state === "running" ? "working…" : t.state}
              </span>
            </div>
            <div className="mt-0.5 font-mono text-[10px] text-black/40 dark:text-white/40">
              {t.model} · {t.turns || "–"} turns · ${t.costUsd.toFixed(2)}
            </div>
            {Object.keys(t.tools).length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1">
                {Object.entries(t.tools).map(([name, n]) => (
                  <span key={name} className="rounded bg-black/[0.06] px-1.5 py-0.5 font-mono text-[10px] dark:bg-white/10">
                    {name} ×{n}
                  </span>
                ))}
              </div>
            )}
            {t.lastText && <p className="mt-1 line-clamp-2 text-black/50 italic dark:text-white/50">{t.lastText}</p>}
          </li>
        ))}
      </ul>
      {view.sessions.length > 0 && (
        <div className="mt-4 space-y-1 border-t border-black/10 pt-3 text-xs dark:border-white/15">
          <div className="font-medium text-black/60 dark:text-white/60">Coding sessions</div>
          {view.sessions.map((s) => (
            <div key={s.component} className="text-black/60 dark:text-white/60">
              {s.repo} <span className="text-black/40 dark:text-white/40">· may write only: {s.pathGlobs.join(", ")}</span>
            </div>
          ))}
        </div>
      )}
      <div className="mt-4 text-[11px] text-black/40 dark:text-white/40">Agent spend on this card: ${view.costUsd.toFixed(2)}</div>
    </Panel>
  );
}

function PlanPanel({ view }: { view: View }) {
  const [picked, setPicked] = useState<number | null>(null);
  if (view.plans.length === 0) return null;
  const shown = view.plans.find((p) => p.version === picked) ?? view.plans[view.plans.length - 1];
  const approved = view.approved?.version === shown.version;
  return (
    <Panel title="Implementation plan">
      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
        {view.plans.map((p) => (
          <button
            key={p.version}
            onClick={() => setPicked(p.version)}
            className={`rounded-full border px-2.5 py-0.5 ${p.version === shown.version ? "border-black/50 dark:border-white/60" : "border-black/15 dark:border-white/20"}`}
          >
            v{p.version}
          </button>
        ))}
        <span className="font-mono text-[10px] text-black/40 dark:text-white/40">{shown.digest.slice(0, 19)}</span>
        {approved ? (
          <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] text-emerald-800 dark:text-emerald-300">
            approved by {view.approved?.approver}
          </span>
        ) : view.waitingOnSponsor && shown.version === view.plans[view.plans.length - 1].version ? (
          <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] text-amber-800 dark:text-amber-300">awaiting the sponsor’s approval</span>
        ) : null}
      </div>
      <PlanBody plan={shown.plan} />
    </Panel>
  );
}

function PlanBody({ plan }: { plan: Plan }) {
  return (
    <div className="space-y-4 text-xs">
      <p className="text-sm leading-relaxed text-black/80 dark:text-white/80">{plan.summary}</p>
      <details>
        <summary className="cursor-pointer text-black/60 dark:text-white/60">Approach ({plan.approach.length} steps)</summary>
        <ol className="mt-2 list-decimal space-y-1 pl-5 text-black/70 dark:text-white/70">
          {plan.approach.map((a) => (
            <li key={a}>{a}</li>
          ))}
        </ol>
      </details>
      <div className="grid gap-4 md:grid-cols-2">
        {plan.repos.map((r) => (
          <div key={r.component}>
            <div className="font-medium">{r.repo}</div>
            <ul className="mt-1 space-y-0.5 font-mono text-[11px]">
              {r.changes.map((c) => (
                <li key={c.path} title={c.purpose} className="text-black/70 dark:text-white/70">
                  <span className={c.action === "create" ? "text-emerald-700 dark:text-emerald-300" : c.action === "delete" ? "text-red-700 dark:text-red-300" : "text-amber-700 dark:text-amber-300"}>
                    {c.action === "create" ? "+" : c.action === "delete" ? "−" : "~"}
                  </span>{" "}
                  {c.path}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      <div>
        <div className="font-medium">Tests, each mapped to the acceptance criteria it proves</div>
        <ul className="mt-1 space-y-1">
          {plan.tests.map((t) => (
            <li key={t.name} className="text-black/70 dark:text-white/70">
              {t.covers.map((ac) => (
                <span key={ac} className="mr-1 rounded bg-black/[0.06] px-1 font-mono text-[10px] dark:bg-white/10">
                  {ac}
                </span>
              ))}
              {t.name}
            </li>
          ))}
        </ul>
      </div>
      <div>
        <div className="font-medium">Scope envelope: the only paths agents may write</div>
        {plan.scopeEnvelope.map((e) => (
          <div key={e.component} className="mt-1 font-mono text-[11px] text-black/60 dark:text-white/60">
            {e.component}: {e.pathGlobs.join("  ")}
          </div>
        ))}
      </div>
      {plan.risks.length > 0 && (
        <details>
          <summary className="cursor-pointer text-black/60 dark:text-white/60">Risks ({plan.risks.length})</summary>
          <ul className="mt-2 list-disc space-y-1 pl-5 text-black/70 dark:text-white/70">
            {plan.risks.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}

const WHO_TONE: Record<Who, string> = {
  human: "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  agent: "bg-violet-500/15 text-violet-800 dark:text-violet-300",
  pipeline: "bg-black/[0.06] text-black/60 dark:bg-white/10 dark:text-white/60",
};

function EventStream({ events, live }: { events: CardFile["events"]; live: boolean }) {
  const rows = events.map((e) => ({ e, d: describe(e) })).filter((r) => r.d !== null);
  const ordered = live ? [...rows].reverse() : rows;
  return (
    <Panel title={`Event log (${events.length} events${live ? ", newest first" : ""})`}>
      <ul className="max-h-[28rem] space-y-1.5 overflow-y-auto pr-1">
        {ordered.map(({ e, d }) => (
          <li key={e.id} className="grid grid-cols-[4.5rem_4.5rem_1fr] items-start gap-2 text-xs">
            <span className="font-mono text-[10px] text-black/40 dark:text-white/40">{new Date(e.time).toLocaleTimeString()}</span>
            <span className={`justify-self-start rounded-full px-2 py-0.5 text-[10px] ${WHO_TONE[d!.who]}`}>{d!.who}</span>
            <span>
              <span className="text-black/80 dark:text-white/80">{d!.text}</span>
              <span className="ml-2 font-mono text-[10px] text-black/35 dark:text-white/35">{e.type.replace(/^ap\./, "").replace(/\.v\d+$/, "")}</span>
            </span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

const FLOW = [
  { who: "human", label: "Card assigned" },
  { who: "pipeline", label: "Intake checks" },
  { who: "agent", label: "Planner reads the repos" },
  { who: "pipeline", label: "Plan validated" },
  { who: "human", label: "Sponsor approves" },
  { who: "agent", label: "Tests, then code" },
  { who: "pipeline", label: "CI on every commit" },
  { who: "human", label: "PR review + merge" },
] as const;

const PRINCIPLES = [
  {
    title: "Everything is an event",
    body: (types: number) =>
      `Each step (an assignment, a plan, an approval, a failed test) is written to one append-only log, as one of ${types || "a fixed set of"} catalogued event types. Anything not in the catalog is rejected, which is what keeps the pipeline's scope explicit. This page, its status and its replay are all computed from that log.`,
  },
  {
    title: "Deterministic handlers decide",
    body: () =>
      "Small handlers subscribe to events and emit new ones: intake, comment parsing, orchestration, plan approval, reporting. They are idempotent, so the whole log can be replayed without changing anything, and they decide when an agent is worth invoking at all.",
  },
  {
    title: "Agents propose, policy disposes",
    body: () =>
      "Agents run in disposable Docker sandboxes with read-only checkouts and no GitHub credentials. Their output is a proposal, never an action: a plan must cover every acceptance criterion and stay inside an allowed set of paths before it reaches a human.",
  },
  {
    title: "Humans hold three controls",
    body: () =>
      "The sponsoring developer assigns the card and gives guidance, approves one exact plan version (bound to its content hash, so a changed plan needs re-approval), and reviews the resulting pull request. Nothing merges without a person.",
  },
  {
    title: "The pipeline is the judge",
    building: true,
    body: () =>
      "Every agent commit goes through the same CI as human code: lint, typecheck, tests, build. Failures come back to the agent as structured events, not raw logs, and it iterates until green or escalates to the sponsor.",
  },
] as const;

const FLOW_TONE: Record<Who, string> = {
  human: "border-amber-500/40 bg-amber-500/10 text-amber-900 dark:text-amber-200",
  agent: "border-violet-500/40 bg-violet-500/10 text-violet-900 dark:text-violet-200",
  pipeline: "border-black/15 bg-black/[0.04] text-black/75 dark:border-white/20 dark:bg-white/[0.06] dark:text-white/75",
};

/**
 * The explanation a first-time visitor needs before the live view makes sense.
 * Placed above the cards and set at reading size: it is the point of the page,
 * not a footnote.
 */
function HowItWorks({ index }: { index: FeedIndex | null }) {
  const p = index?.pipeline;
  return (
    <section className="space-y-5 rounded-xl border border-black/10 p-6 dark:border-white/15">
      <div>
        <h2 className="text-lg font-medium">How it works</h2>
        <p className="mt-1 text-sm text-black/60 dark:text-white/60">
          One card&rsquo;s journey, colour-coded by who acts: <span className="text-amber-700 dark:text-amber-300">people</span>,{" "}
          <span className="text-violet-700 dark:text-violet-300">agents</span> or the <span className="text-black/80 dark:text-white/80">pipeline</span>.
        </p>
      </div>

      <ol className="flex flex-wrap items-center gap-2 text-sm">
        {FLOW.map((f, i) => (
          <li key={f.label} className="flex items-center gap-2">
            <span className={`rounded-lg border px-3 py-1.5 ${FLOW_TONE[f.who]}`}>{f.label}</span>
            {i < FLOW.length - 1 && <span className="text-black/30 dark:text-white/30">→</span>}
          </li>
        ))}
      </ol>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {PRINCIPLES.map((pr) => (
          <div key={pr.title} className="rounded-lg bg-black/[0.03] p-4 dark:bg-white/[0.04]">
            <h3 className="flex items-center gap-2 text-base font-medium">
              {pr.title}
              {"building" in pr && pr.building && (
                <span className="rounded-full bg-sky-500/15 px-2 py-0.5 text-[11px] font-normal text-sky-800 dark:text-sky-300">
                  being built: cards currently stop at plan approval
                </span>
              )}
            </h3>
            <p className="mt-2 text-sm leading-relaxed text-black/70 dark:text-white/70">{pr.body(p?.catalogTypes ?? 0)}</p>
          </div>
        ))}
      </div>

      {p && p.handlers.length > 0 && (
        <div>
          <h3 className="text-sm font-medium">Handlers running in this deployment</h3>
          <ul className="mt-2 flex flex-wrap gap-2">
            {p.handlers.map((h) => (
              <li key={h.id + h.name} className="rounded-full border border-black/15 px-3 py-1 text-xs dark:border-white/20">
                <span className="font-mono text-black/50 dark:text-white/50">{h.id}</span> {h.name.replace(/-/g, " ")}
              </li>
            ))}
          </ul>
        </div>
      )}

      <p className="text-sm text-black/60 dark:text-white/60">
        This page is read-only: runs are started from the command line and plans are approved by the sponsoring developer, never from here.
      </p>
    </section>
  );
}
