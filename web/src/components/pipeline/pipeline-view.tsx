"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  STAGES,
  cardUrl,
  describe,
  fetchFeed,
  label,
  ms,
  project,
  type CardFile,
  type FeedEvent,
  type FeedIndex,
  type Plan,
  type View,
  type Who,
} from "@/lib/pipeline-feed";

/**
 * A window onto the agentic pipeline, in five tabs:
 *
 *   Overview  what it is and the rules it runs by
 *   Process   one card's run: stages, agents, PRs, the approved plan
 *   Events    the raw event log the rest of the page is computed from
 *   Stack     the technology, layer by layer
 *   Links     the repositories and the feed
 *
 * Process and Events share one replay cursor. Live shows the fold over the
 * whole log; Replay shows it over a growing prefix, so a visitor can watch a
 * finished run unfold without the pipeline running.
 */

const TABS = [
  { id: "overview", label: "Overview" },
  { id: "process", label: "Process" },
  { id: "events", label: "Events" },
  { id: "stack", label: "Stack" },
  { id: "links", label: "Links" },
] as const;
type TabId = (typeof TABS)[number]["id"];

export function PipelineView({ feedUrl }: { feedUrl: string }) {
  const [tab, setTab] = useTab();
  const [index, setIndex] = useState<FeedIndex | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [card, setCard] = useState<CardFile | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [mode, setMode] = useState<"live" | "replay">("live");
  const [cursor, setCursor] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(3);

  // The CDN in front of the feed caches for minutes, so slow polling is enough.
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

  // Replay steps are distinct instants, so a minute of agent thinking is one tick.
  const steps = useMemo(() => {
    if (!card) return [];
    const all = [...card.events.map((e) => ms(e.time)), ...card.telemetry.map((t) => ms(t.time))];
    return [...new Set(all)].sort((a, b) => a - b);
  }, [card]);

  const atEnd = cursor >= steps.length - 1;
  useEffect(() => {
    if (!playing || atEnd) return;
    const timer = setInterval(() => setCursor((c) => Math.min(c + 1, steps.length - 1)), 600 / speed);
    return () => clearInterval(timer);
  }, [playing, atEnd, speed, steps.length]);

  const until = mode === "replay" && steps.length ? steps[Math.min(cursor, steps.length - 1)] : null;
  const view = useMemo(() => (card ? project(card.events, card.telemetry, until) : null), [card, until]);
  const visible = useMemo(() => (card ? card.events.filter((e) => until === null || ms(e.time) <= until) : []), [card, until]);
  // The overview always describes the finished run, whatever the replay cursor says.
  const full = useMemo(() => (card ? project(card.events, card.telemetry, null) : null), [card]);

  const startReplay = () => {
    setMode("replay");
    setCursor(0);
    setPlaying(true);
  };
  const goLive = () => {
    setMode("live");
    setPlaying(false);
  };

  const runControls = card && view && (
    <RunBar
      index={index}
      selected={selected}
      onSelect={(k) => {
        setSelected(k);
        goLive();
      }}
    >
      <ReplayBar
        mode={mode}
        playing={playing}
        speed={speed}
        cursor={cursor}
        steps={steps.length}
        until={until}
        onLive={goLive}
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
    </RunBar>
  );

  return (
    <main className="mx-auto w-full max-w-6xl px-4 py-8 sm:px-6">
      <Header index={index} card={card} view={full} />

      {error && <p className="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">Feed unavailable: {error}</p>}

      <Tabs tab={tab} onChange={setTab} />

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} className="mt-6">
        {tab === "overview" && <Overview index={index} card={card} view={full} onOpen={setTab} />}
        {tab === "stack" && <Stack index={index} />}
        {tab === "links" && <ProjectLinks />}
        {(tab === "process" || tab === "events") &&
          (card && view ? (
            <div className="space-y-6">
              {runControls}
              {tab === "process" ? <Process card={card} view={view} /> : <EventLog events={visible} live={mode === "live"} />}
            </div>
          ) : (
            <p className="text-sm text-black/50 dark:text-white/50">{index ? "No cards yet." : "Loading the pipeline feed…"}</p>
          ))}
      </div>
    </main>
  );
}

// ---------------------------------------------------------------------------
// Shell: header, tabs, run controls

/** The selected tab lives in the URL hash so a tab can be linked to directly. */
function useTab(): [TabId, (t: TabId) => void] {
  const [tab, setTabState] = useState<TabId>("overview");
  useEffect(() => {
    const read = () => {
      const h = window.location.hash.slice(1);
      if (TABS.some((t) => t.id === h)) setTabState(h as TabId);
    };
    read();
    window.addEventListener("hashchange", read);
    return () => window.removeEventListener("hashchange", read);
  }, []);
  const setTab = useCallback((t: TabId) => {
    setTabState(t);
    history.replaceState(null, "", t === "overview" ? window.location.pathname : `#${t}`);
  }, []);
  return [tab, setTab];
}

function Tabs({ tab, onChange }: { tab: TabId; onChange: (t: TabId) => void }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: React.KeyboardEvent, i: number) => {
    const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!d) return;
    e.preventDefault();
    const next = (i + d + TABS.length) % TABS.length;
    onChange(TABS[next].id);
    refs.current[next]?.focus();
  };
  return (
    <div role="tablist" aria-label="Pipeline" className="mt-6 flex gap-1 overflow-x-auto border-b border-black/10 dark:border-white/15">
      {TABS.map((t, i) => {
        const active = t.id === tab;
        return (
          <button
            key={t.id}
            ref={(el) => {
              refs.current[i] = el;
            }}
            role="tab"
            id={`tab-${t.id}`}
            aria-selected={active}
            aria-controls={`panel-${t.id}`}
            tabIndex={active ? 0 : -1}
            onClick={() => onChange(t.id)}
            onKeyDown={(e) => onKey(e, i)}
            className={`-mb-px border-b-2 px-4 py-2 text-sm whitespace-nowrap transition ${
              active
                ? "border-black font-medium text-black dark:border-white dark:text-white"
                : "border-transparent text-black/55 hover:text-black dark:text-white/55 dark:hover:text-white"
            }`}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

function Header({ index, card, view }: { index: FeedIndex | null; card: CardFile | null; view: View | null }) {
  return (
    <header className="space-y-3">
      <Link href="/" className="text-xs text-black/50 hover:text-black dark:text-white/50 dark:hover:text-white">
        ← back to the chat
      </Link>
      <h1 className="text-2xl font-medium">Agentic pipeline</h1>
      <p className="max-w-3xl text-sm leading-relaxed text-black/70 dark:text-white/70">
        An event-driven build pipeline where AI agents do the implementation work. A developer assigns a tracker card and
        approves the agent&rsquo;s plan; agents write the tests and code in sandboxes; CI judges every commit; a person reviews
        and merges. This page is a read-only view of the real run.
      </p>
      {card && view && <RunStats card={card} view={view} />}
      {index && (
        <p className="text-[11px] text-black/40 dark:text-white/40">
          Snapshot {new Date(index.generatedAt).toLocaleString()} · refreshes every few minutes
        </p>
      )}
    </header>
  );
}

/** Headline numbers for the selected card, from the complete log. */
function RunStats({ card, view }: { card: CardFile; view: View }) {
  const times = card.events.map((e) => ms(e.time));
  const approved = card.events.find((e) => e.type === "ap.work.plan.approved.v1");
  const end = Math.max(...times);
  const minutes = (from: number) => Math.round((end - from) / 60_000);
  const firstPass = view.changes.length > 0 && view.changes.every((c) => c.iterations === 0);
  const stats: [string, string][] = [
    [card.card.key, view.status.replace("-", " ")],
    ["approval → done", approved ? `${minutes(ms(approved.time))} min` : "–"],
    ["agent runs", String(view.tasks.length)],
    ["model spend", `$${view.costUsd.toFixed(2)}`],
    ["pull requests", `${view.changes.filter((c) => c.merged).length}/${view.changes.length} merged`],
    ["CI", view.changes.length ? (firstPass ? "green first push" : `${view.changes.reduce((n, c) => n + c.iterations, 0)} fix loops`) : "–"],
    ["events", String(card.events.length)],
  ];
  return (
    <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
      {stats.map(([k, v]) => (
        <div key={k} className="rounded-lg border border-black/10 px-3 py-2 dark:border-white/15">
          <dt className="text-[10px] tracking-wide text-black/45 uppercase dark:text-white/45">{k}</dt>
          <dd className="mt-0.5 text-sm font-medium">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function RunBar({
  index,
  selected,
  onSelect,
  children,
}: {
  index: FeedIndex | null;
  selected: string | null;
  onSelect: (k: string) => void;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-black/10 px-3 py-2 dark:border-white/15">
      <div className="flex flex-wrap items-center gap-2">
        {index?.cards.map((c) => (
          <button
            key={c.key}
            onClick={() => onSelect(c.key)}
            title={c.summary}
            className={`flex items-center gap-2 rounded-lg border px-2.5 py-1 text-xs transition ${
              c.key === selected ? "border-black/50 dark:border-white/60" : "border-black/10 hover:border-black/30 dark:border-white/15 dark:hover:border-white/35"
            }`}
          >
            <span className="font-mono">{c.key}</span>
            <StatusPill status={c.status} />
          </button>
        ))}
      </div>
      {children}
    </div>
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
  const btn = "rounded-lg border border-black/15 px-3 py-1 text-xs transition hover:border-black/40 dark:border-white/20 dark:hover:border-white/50";
  const replaying = props.mode === "replay";
  return (
    <div className="flex flex-wrap items-center gap-2">
      <button className={`${btn} ${!replaying ? "bg-black text-white dark:bg-white dark:text-black" : ""}`} onClick={props.onLive}>
        Final state
      </button>
      <button className={btn} onClick={props.onToggle}>
        {replaying && props.playing && props.cursor < props.steps - 1 ? "Pause" : replaying ? "Play" : "▶ Replay the run"}
      </button>
      {replaying && (
        <>
          <select className={`${btn} bg-transparent`} value={props.speed} onChange={(e) => props.onSpeed(Number(e.target.value))} aria-label="Replay speed">
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
            className="w-32"
            aria-label="Replay position"
          />
          <span className="font-mono text-[11px] text-black/50 dark:text-white/50">{props.until ? new Date(props.until).toLocaleTimeString() : ""}</span>
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Overview

const FLOW: { who: Who; label: string }[] = [
  { who: "human", label: "Card assigned" },
  { who: "pipeline", label: "Intake checks" },
  { who: "agent", label: "Planner reads the repos" },
  { who: "pipeline", label: "Plan validated" },
  { who: "human", label: "Sponsor approves" },
  { who: "agent", label: "Tests, then code" },
  { who: "pipeline", label: "CI on every commit" },
  { who: "human", label: "Review + merge" },
  { who: "pipeline", label: "Deploy" },
];

const PRINCIPLES: { title: string; body: (types: number) => string }[] = [
  {
    title: "Everything is an event",
    body: (n) =>
      `Every step is written to one append-only log as one of ${n || "a fixed set of"} catalogued event types. Anything not in the catalog is rejected, which keeps the pipeline's scope explicit. Status, this page and its replay are all computed from the log.`,
  },
  {
    title: "Deterministic handlers decide",
    body: () =>
      "Small idempotent handlers subscribe to events and emit new ones. They own routing, validation and policy, and decide when an agent is worth invoking. Replaying the log changes nothing.",
  },
  {
    title: "Agents propose, policy disposes",
    body: () =>
      "Agents run in throwaway sandboxes with no GitHub credentials. A plan must cover every acceptance criterion with a test and stay inside an allowed set of paths before a person sees it; a guard reverts any write outside it.",
  },
  {
    title: "People hold three controls",
    body: () =>
      "The sponsor assigns the card with guidance, approves one exact plan version (bound to its content hash, so any change needs re-approval) and reviews the pull requests. Nothing merges without a person.",
  },
  {
    title: "The pipeline is the judge",
    body: () =>
      "Agent commits go through the same CI as human ones. Failures come back to the agent as structured feedback, and it iterates until green or escalates to the sponsor.",
  },
  {
    title: "Multi-repo by design",
    body: () =>
      "One card can change several repositories. They are ordered by dependency; the consumer is pinned to the producer's commit and re-pinned automatically when the producer merges.",
  },
];

const FLOW_TONE: Record<Who, string> = {
  human: "border-amber-500/40 bg-amber-500/10 text-amber-900 dark:text-amber-200",
  agent: "border-violet-500/40 bg-violet-500/10 text-violet-900 dark:text-violet-200",
  pipeline: "border-black/15 bg-black/[0.04] text-black/75 dark:border-white/20 dark:bg-white/[0.06] dark:text-white/75",
};

function Overview({ index, card, view, onOpen }: { index: FeedIndex | null; card: CardFile | null; view: View | null; onOpen: (t: TabId) => void }) {
  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <h2 className="text-lg font-medium">One card, end to end</h2>
        <p className="text-sm text-black/60 dark:text-white/60">
          Coloured by who acts: <span className="text-amber-700 dark:text-amber-300">people</span>,{" "}
          <span className="text-violet-700 dark:text-violet-300">agents</span> or the <span className="text-black/80 dark:text-white/80">pipeline</span>.
        </p>
        <ol className="flex flex-wrap items-center gap-2 text-sm">
          {FLOW.map((f, i) => (
            <li key={f.label} className="flex items-center gap-2">
              <span className={`rounded-lg border px-3 py-1.5 ${FLOW_TONE[f.who]}`}>{f.label}</span>
              {i < FLOW.length - 1 && <span className="text-black/30 dark:text-white/30">→</span>}
            </li>
          ))}
        </ol>
      </section>

      <section className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {PRINCIPLES.map((p) => (
          <div key={p.title} className="rounded-xl bg-black/[0.03] p-4 dark:bg-white/[0.04]">
            <h3 className="text-base font-medium">{p.title}</h3>
            <p className="mt-2 text-sm leading-relaxed text-black/70 dark:text-white/70">{p.body(index?.pipeline.catalogTypes ?? 0)}</p>
          </div>
        ))}
      </section>

      {card && view && (
        <section className="rounded-xl border border-black/10 p-5 dark:border-white/15">
          <div className="font-mono text-xs text-black/50 dark:text-white/50">The demo card · {card.card.key}</div>
          <h3 className="mt-1 text-base font-medium">{card.card.summary}</h3>
          <p className="mt-2 text-sm text-black/70 dark:text-white/70">{card.card.description}</p>
          <p className="mt-3 text-sm text-black/70 dark:text-white/70">
            {card.card.acceptanceCriteria.length} acceptance criteria across {card.card.components.length} repositories, built by{" "}
            {view.tasks.length} agent runs for ${view.costUsd.toFixed(2)}.{" "}
            <button className="underline" onClick={() => onOpen("process")}>
              Watch the run
            </button>{" "}
            or{" "}
            <button className="underline" onClick={() => onOpen("events")}>
              read its events
            </button>
            .
          </p>
        </section>
      )}

      <p className="text-xs text-black/50 dark:text-white/50">
        Read-only by construction: runs start from the orchestrator&rsquo;s CLI and plans are approved by the sponsoring developer, never from this page.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Process

function Process({ card, view }: { card: CardFile; view: View }) {
  return (
    <div className="space-y-6">
      <Stages view={view} />
      <div className="grid gap-6 md:grid-cols-2">
        <CardPanel card={card} />
        <AgentsPanel view={view} />
      </div>
      <ChangesPanel view={view} />
      <PlanPanel view={view} />
    </div>
  );
}

const STATUS_TONE: Record<string, string> = {
  "plan-review": "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  planning: "bg-sky-500/15 text-sky-800 dark:text-sky-300",
  implementing: "bg-violet-500/15 text-violet-800 dark:text-violet-300",
  "in-review": "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  merged: "bg-emerald-500/15 text-emerald-800 dark:text-emerald-300",
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

function Stages({ view }: { view: View }) {
  const current = STAGES.findIndex((s) => s.id === view.stage);
  return (
    <ol className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
      {STAGES.map((s, i) => {
        const state = view.done || i < current ? "done" : i === current ? "current" : "todo";
        const waiting = state === "current" && s.id === "approval" && view.waitingOnSponsor;
        return (
          <li
            key={s.id}
            className={`rounded-lg border px-3 py-2 text-xs ${
              state === "current"
                ? "border-black/50 dark:border-white/60"
                : state === "done"
                  ? "border-emerald-500/30 text-black/70 dark:text-white/70"
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
    <Panel title={`The card · ${c.key} · sponsor ${c.sponsor}`}>
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
    <Panel title={`Agents · $${view.costUsd.toFixed(2)}`}>
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
            {t.state === "running" && t.lastText && <p className="mt-1 line-clamp-2 text-black/50 italic dark:text-white/50">{t.lastText}</p>}
          </li>
        ))}
      </ul>
      {view.sessions.length > 0 && (
        <div className="mt-4 space-y-1 border-t border-black/10 pt-3 text-xs dark:border-white/15">
          <div className="font-medium text-black/60 dark:text-white/60">Write scope per repository</div>
          {view.sessions.map((s) => (
            <div key={s.component} className="text-black/60 dark:text-white/60">
              {s.repo} <span className="font-mono text-[10px] text-black/40 dark:text-white/40">{s.pathGlobs.join(", ")}</span>
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}

const CI_TONE: Record<string, string> = {
  running: "animate-pulse bg-sky-500/15 text-sky-800 dark:text-sky-300",
  success: "bg-emerald-500/15 text-emerald-800 dark:text-emerald-300",
  failure: "bg-red-500/15 text-red-800 dark:text-red-300",
};

function ChangesPanel({ view }: { view: View }) {
  if (view.changes.length === 0) return null;
  return (
    <Panel title="Pull requests, in merge order">
      <ul className="space-y-3">
        {view.changes.map((c) => (
          <li key={c.component} className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <span className="font-medium">{c.repo}</span>
            {c.url ? (
              <a className="underline" href={c.url} target="_blank" rel="noopener noreferrer">
                #{c.number}
              </a>
            ) : (
              <span className="text-xs text-black/50 dark:text-white/50">{c.commits ? "pushing…" : "waiting to start"}</span>
            )}
            {c.ci && <span className={`rounded-full px-2 py-0.5 text-[11px] ${CI_TONE[c.ci]}`}>CI {c.ci}</span>}
            {c.iterations > 0 && <span className="text-xs text-black/60 dark:text-white/60">{c.iterations} fix{c.iterations > 1 ? "es" : ""}</span>}
            {c.merged ? (
              <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[11px] text-emerald-800 dark:text-emerald-300">merged</span>
            ) : c.ready ? (
              <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[11px] text-amber-800 dark:text-amber-300">ready for review</span>
            ) : null}
            {c.head && <span className="font-mono text-[11px] text-black/40 dark:text-white/40">{c.head.slice(0, 7)}</span>}
            {c.blocked && <span className="w-full text-xs text-red-700 dark:text-red-300">blocked: {c.blocked}</span>}
          </li>
        ))}
      </ul>
      {view.deployment && (
        <p className="mt-3 text-sm">
          Deploy workflow:{" "}
          <a className="underline" href={view.deployment.url} target="_blank" rel="noopener noreferrer">
            {view.deployment.conclusion}
          </a>
          {view.done && " · work item closed"}
        </p>
      )}
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
          <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] text-emerald-800 dark:text-emerald-300">approved by {view.approved?.approver}</span>
        ) : view.waitingOnSponsor && shown.version === view.plans[view.plans.length - 1].version ? (
          <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] text-amber-800 dark:text-amber-300">awaiting the sponsor&rsquo;s approval</span>
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
        <div className="font-medium">Tests, each mapped to the criteria it proves</div>
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
      <details>
        <summary className="cursor-pointer text-black/60 dark:text-white/60">
          Approach ({plan.approach.length} steps){plan.risks.length > 0 && `, risks (${plan.risks.length})`}, scope envelope
        </summary>
        <ol className="mt-2 list-decimal space-y-1 pl-5 text-black/70 dark:text-white/70">
          {plan.approach.map((a) => (
            <li key={a}>{a}</li>
          ))}
        </ol>
        {plan.risks.length > 0 && (
          <ul className="mt-3 list-disc space-y-1 pl-5 text-black/70 dark:text-white/70">
            {plan.risks.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        )}
        <div className="mt-3">
          {plan.scopeEnvelope.map((e) => (
            <div key={e.component} className="font-mono text-[11px] text-black/60 dark:text-white/60">
              {e.component}: {e.pathGlobs.join("  ")}
            </div>
          ))}
        </div>
      </details>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Events

const WHO_TONE: Record<Who, string> = {
  human: "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  agent: "bg-violet-500/15 text-violet-800 dark:text-violet-300",
  pipeline: "bg-black/[0.06] text-black/60 dark:bg-white/10 dark:text-white/60",
};

const shortType = (t: string) => t.replace(/^ap\./, "").replace(/\.v\d+$/, "");

function EventLog({ events, live }: { events: FeedEvent[]; live: boolean }) {
  const [who, setWho] = useState<Who | "all">("all");
  const [open, setOpen] = useState<string | null>(null);
  const byId = useMemo(() => new Map(events.map((e) => [e.id, e])), [events]);

  const rows = events.map((e) => ({ e, d: describe(e) ?? { who: (e.actor?.kind === "human" ? "human" : "pipeline") as Who, text: shortType(e.type) } }));
  const counts = rows.reduce<Record<string, number>>((m, r) => ((m[r.d.who] = (m[r.d.who] ?? 0) + 1), m), {});
  const shown = rows.filter((r) => who === "all" || r.d.who === who);
  const ordered = live ? [...shown].reverse() : shown;

  const jump = (id: string) => {
    setWho("all");
    setOpen(id);
    requestAnimationFrame(() => document.getElementById(`ev-${id}`)?.scrollIntoView({ block: "center", behavior: "smooth" }));
  };

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_17rem]">
      <Panel title={`${events.length} events${live ? ", newest first" : ""} · click one for its envelope`}>
        <div className="mb-3 flex flex-wrap gap-1.5 text-xs">
          {(["all", "human", "agent", "pipeline"] as const).map((w) => (
            <button
              key={w}
              onClick={() => setWho(w)}
              className={`rounded-full border px-2.5 py-0.5 ${who === w ? "border-black/50 dark:border-white/60" : "border-black/15 dark:border-white/20"}`}
            >
              {w} {w === "all" ? events.length : (counts[w] ?? 0)}
            </button>
          ))}
        </div>
        <ul className="max-h-[36rem] space-y-1 overflow-y-auto pr-1">
          {ordered.map(({ e, d }) => {
            const isOpen = open === e.id;
            const cause = e.causationId ? byId.get(e.causationId) : undefined;
            return (
              <li key={e.id} id={`ev-${e.id}`} className={`rounded-md ${isOpen ? "bg-black/[0.03] dark:bg-white/[0.04]" : ""}`}>
                <button
                  onClick={() => setOpen(isOpen ? null : e.id)}
                  aria-expanded={isOpen}
                  className="grid w-full grid-cols-[4.5rem_4.5rem_1fr] items-start gap-2 px-1 py-1 text-left text-xs"
                >
                  <span className="font-mono text-[10px] text-black/40 dark:text-white/40">{new Date(e.time).toLocaleTimeString()}</span>
                  <span className={`justify-self-start rounded-full px-2 py-0.5 text-[10px] ${WHO_TONE[d.who]}`}>{d.who}</span>
                  <span>
                    <span className="text-black/80 dark:text-white/80">{d.text}</span>
                    <span className="ml-2 font-mono text-[10px] text-black/35 dark:text-white/35">{shortType(e.type)}</span>
                  </span>
                </button>
                {isOpen && (
                  <div className="space-y-2 px-2 pb-2">
                    {cause && (
                      <p className="text-[11px] text-black/55 dark:text-white/55">
                        caused by{" "}
                        <button className="font-mono underline" onClick={() => jump(cause.id)}>
                          {shortType(cause.type)}
                        </button>
                      </p>
                    )}
                    <pre className="max-h-80 overflow-auto rounded-md bg-black/[0.05] p-3 font-mono text-[10.5px] leading-relaxed dark:bg-white/[0.06]">
                      {JSON.stringify(
                        { specversion: "1.0", id: e.id, type: e.type, source: e.source, subject: e.subject, time: e.time, causationid: e.causationId || undefined, actor: e.actor, data: e.data },
                        null,
                        2,
                      )}
                    </pre>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      </Panel>
      <EventModel />
    </div>
  );
}

function EventModel() {
  return (
    <aside className="space-y-4 text-xs leading-relaxed text-black/70 dark:text-white/70">
      <Panel title="The event model">
        <dl className="space-y-3">
          <div>
            <dt className="font-medium text-black dark:text-white">Envelope</dt>
            <dd>CloudEvents 1.0, extended with tenant, team, repo, correlation, causation, W3C trace context, origin and actor.</dd>
          </div>
          <div>
            <dt className="font-medium text-black dark:text-white">Names</dt>
            <dd>
              <code className="font-mono">ap.&lt;domain&gt;.&lt;entity&gt;.&lt;verb&gt;.v1</code>, versioned, and only catalogued types can be published.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-black dark:text-white">Layers</dt>
            <dd>L0 raw webhooks → L1 normalized source facts → L2 intents and commands → L3 platform facts.</dd>
          </div>
          <div>
            <dt className="font-medium text-black dark:text-white">Causality</dt>
            <dd>The card is the correlation root; every event names the event that caused it, so any step traces back to a person.</dd>
          </div>
          <div>
            <dt className="font-medium text-black dark:text-white">Idempotency</dt>
            <dd>IDs are UUIDv5 over (cause, type, key): re-publishing the same fact is a no-op, so delivery can be at-least-once.</dd>
          </div>
        </dl>
      </Panel>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// Stack

const STACK: { layer: string; what: string; items: string[] }[] = [
  {
    layer: "Orchestrator",
    what: "One Go binary, ap: CLI and server.",
    items: [
      "Go 1.26, three dependencies",
      "Handlers subscribe by event type; each keeps its own offset",
      "Two dispatcher lanes: fast (routing, state) and agents (slow work)",
      "Catalog gate rejects uncatalogued event types",
    ],
  },
  {
    layer: "Event store",
    what: "Append-only log; state is a fold over it.",
    items: [
      "SQLite (pure-Go modernc driver) for the demo; the design targets a partitioned log",
      "Event-sourced projections instead of a workflow engine",
      "The same fold drives the CLI, this page and replay",
    ],
  },
  {
    layer: "Agents",
    what: "Claude Agent SDK (TypeScript) on Claude Opus 5.5.",
    items: [
      "Roles: Planner, Test Author, Coding, Fix",
      "Structured output validated against a JSON Schema",
      "Tool allowlists per role, e.g. Bash(npm:*); nothing else runs",
      "Per-role turn and dollar budgets",
    ],
  },
  {
    layer: "Sandbox",
    what: "A disposable Docker container per agent task.",
    items: [
      "node:24-bookworm-slim, non-root",
      "Only the session's checkout is mounted; no GitHub credentials",
      "Scope Guard reverts writes outside the approved paths, deleted tests or weakened assertions",
    ],
  },
  {
    layer: "Source control + CI",
    what: "GitHub, driven from the host, never from the sandbox.",
    items: [
      "Applier commits with Ap-* trailers, pushes ap/<card>/<repo> branches, opens draft PRs",
      "GitHub Actions runs the repo's own CI on every agent commit",
      "A reconciler turns checks, reviews, merges and deploys into keyed events",
      "Failures are classified by rule, then fed back to the coding session",
    ],
  },
  {
    layer: "Multi-repo",
    what: "Changesets across repositories.",
    items: [
      "Producer first: PGDataLayer is an npm package installed from GitHub at a commit SHA",
      "Consumer re-pinned automatically when the producer merges; CI re-runs",
      "Merge order posted on every PR",
    ],
  },
  {
    layer: "Deploy",
    what: "The target app's existing pipeline.",
    items: ["GitHub Actions deploy workflow → Kubernetes (Linode LKE)", "Let's Encrypt TLS", "The deploy result closes the work item"],
  },
  {
    layer: "This page",
    what: "Next.js 16 and React; no backend.",
    items: [
      "The orchestrator publishes JSON snapshots to an orphan feed branch",
      "Fetched from GitHub's CDN in the browser",
      "Read-only by construction: nothing here can start a run or approve a plan",
    ],
  },
];

function Stack({ index }: { index: FeedIndex | null }) {
  const p = index?.pipeline;
  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2">
        {STACK.map((s) => (
          <section key={s.layer} className="rounded-xl border border-black/10 p-4 dark:border-white/15">
            <h3 className="text-base font-medium">{s.layer}</h3>
            <p className="mt-0.5 text-sm text-black/60 dark:text-white/60">{s.what}</p>
            <ul className="mt-3 list-disc space-y-1 pl-5 text-sm text-black/75 dark:text-white/75">
              {s.items.map((i) => (
                <li key={i}>{i}</li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      {p && p.handlers.length > 0 && (
        <Panel title={`Handlers in this deployment · ${p.catalogTypes} catalogued event types`}>
          <ul className="flex flex-wrap gap-2">
            {p.handlers.map((h) => (
              <li key={h.id + h.name} className="rounded-full border border-black/15 px-3 py-1 text-xs dark:border-white/20">
                <span className="font-mono text-black/50 dark:text-white/50">{h.id}</span> {h.name.replace(/-/g, " ")}
              </li>
            ))}
          </ul>
        </Panel>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Links

const PROJECT_LINKS = [
  {
    title: "agentic-pipeline",
    href: "https://github.com/notbobutah/agentic-pipeline",
    kind: "Pipeline",
    text: "The orchestrator: event log, handlers, agent sandboxes and the CLI that runs cards.",
  },
  {
    title: "Design docs",
    href: "https://github.com/notbobutah/agentic-pipeline/tree/main/docs",
    kind: "Docs",
    text: "Requirements, architecture, event model and catalog, handlers, work plane, security, ADRs.",
  },
  {
    title: "PGDataLayer",
    href: "https://github.com/notbobutah/PGDataLayer",
    kind: "Target repo",
    text: "Sample data and JSON Schemas published as an npm package. Agents wrote its data, validators and tests.",
  },
  {
    title: "GoReactChat",
    href: "https://github.com/notbobutah/GoReactChat",
    kind: "Target repo",
    text: "This site: a Go + Next.js app. Agents add pages to its web client; its CI verifies every agent commit.",
  },
  {
    title: "/catalog",
    href: "/catalog",
    kind: "Built by agents",
    text: "The page the demo card produced, deployed by the pipeline.",
  },
  {
    title: "Pipeline feed",
    href: "https://github.com/notbobutah/agentic-pipeline/tree/feed",
    kind: "Data",
    text: "The JSON snapshots of the event log this page reads.",
  },
] as const;

function ProjectLinks() {
  return (
    <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {PROJECT_LINKS.map((l) => {
        const body = (
          <>
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium">{l.title}</span>
              <span className="rounded-full bg-black/[0.06] px-2 py-0.5 text-[10px] text-black/60 dark:bg-white/10 dark:text-white/60">{l.kind}</span>
            </div>
            <p className="mt-1 text-xs leading-relaxed text-black/60 dark:text-white/60">{l.text}</p>
          </>
        );
        const cls = "block h-full rounded-xl border border-black/10 px-4 py-3 transition hover:border-black/35 dark:border-white/15 dark:hover:border-white/40";
        return (
          <li key={l.title}>
            {l.href.startsWith("http") ? (
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
  );
}
