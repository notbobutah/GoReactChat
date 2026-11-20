/**
 * Read model for the /pipeline page.
 *
 * The page never talks to the pipeline itself. The orchestrator (agentic-pipeline,
 * running on a developer machine) publishes JSON snapshots of its event log to a
 * public branch, and this module fetches and interprets them. That keeps the site
 * read-only by construction: there is no endpoint here that could start a run,
 * approve a plan or spend money — those stay with the sponsoring developer.
 *
 * Everything shown is derived from events. "Status", "which agent is working",
 * "what the plan says" are all folds over the same list, which is also what
 * makes replay possible: render the fold over the first N events instead of all.
 */

export const DEFAULT_FEED_URL =
  "https://raw.githubusercontent.com/notbobutah/agentic-pipeline/feed/index.json";

export type Actor = { kind: string; id: string; login?: string };

export type FeedEvent = {
  seq: number;
  id: string;
  type: string;
  time: string;
  subject: string;
  causationId: string;
  source: string;
  actor?: Actor;
  data: Record<string, unknown>;
};

export type Telemetry = {
  seq: number;
  task: string;
  time: string;
  body: { kind: string; name?: string; text?: string; status?: string; role?: string; costUsd?: number; turns?: number };
};

export type FileChange = { path: string; action: string; purpose: string };
export type Plan = {
  summary: string;
  approach: string[];
  repos: { component: string; repo: string; changes: FileChange[] }[];
  tests: { name: string; component: string; level: string; covers: string[] }[];
  outOfScope: string[];
  risks: string[];
  openQuestions: string[];
  scopeEnvelope: { component: string; pathGlobs: string[] }[];
  estimate: { iterations: string; costUsd: number; wallclockMinutes: number };
};

export type Card = {
  key: string;
  issueType: string;
  summary: string;
  sponsor: string;
  components: string[];
  description: string;
  acceptanceCriteria: string[];
  guidance?: string[];
};

export type CardSummary = {
  key: string;
  summary: string;
  status: string;
  sponsor: string;
  components: string[];
  costUsd: number;
  started: string;
  updated: string;
  events: number;
  file: string;
};

export type FeedIndex = {
  version: number;
  generatedAt: string;
  pipeline: {
    name: string;
    repo: string;
    catalogTypes: number;
    handlers: { id: string; name: string }[];
    components: { name: string; repo: string; root: string }[];
  };
  cards: CardSummary[];
};

export type CardFile = {
  key: string;
  card: Card;
  status: string;
  events: FeedEvent[];
  telemetry: Telemetry[];
};

/**
 * Fetches a feed document. The query string asks the CDN for a fresh copy; raw
 * GitHub content is still cached for up to five minutes, which is why the page
 * labels the live view with the snapshot time rather than pretending to be live.
 */
export async function fetchFeed<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${url}?t=${Math.floor(Date.now() / 15_000)}`, { signal, cache: "no-store" });
  if (!res.ok) throw new Error(`feed ${res.status} ${res.statusText}`);
  return (await res.json()) as T;
}

export function cardUrl(indexUrl: string, file: string): string {
  return new URL(file, indexUrl).toString();
}

// ---------------------------------------------------------------------------
// Stages: the coarse journey a visitor can follow without reading event names.

export const STAGES = [
  { id: "intake", label: "Card picked up" },
  { id: "planning", label: "Agent plans" },
  { id: "approval", label: "Developer approves" },
  { id: "coding", label: "Agents write code + tests" },
  { id: "verify", label: "Pipeline builds + tests" },
  { id: "review", label: "Human review" },
  { id: "done", label: "Merged + deployed" },
] as const;

export type StageId = (typeof STAGES)[number]["id"];

export function stageOf(type: string): StageId | null {
  const t = type.replace(/^ap\./, "").replace(/\.v\d+$/, "");
  if (/^work\.(item\.assigned|request|repos)/.test(t)) return "intake";
  if (/^work\.plan\.(requested)$/.test(t) || t.startsWith("agent.")) return null; // follows its parent
  if (/^work\.plan\.(proposed|revision|approved|rejected)/.test(t) || t.startsWith("command.")) return "approval";
  if (t.startsWith("devsession.ready_for_review") || t.startsWith("change.review")) return "review";
  if (t.startsWith("devsession.") || t.startsWith("changeset.created")) return "coding";
  if (t.startsWith("pipeline.") || t.startsWith("diagnosis.") || t.startsWith("change.")) return "verify";
  if (t === "work.completed" || t.startsWith("deployment.") || t === "changeset.merged") return "done";
  return null;
}

// ---------------------------------------------------------------------------
// Projection over a prefix of the log (live view = whole log, replay = prefix).

export type AgentTask = {
  taskId: string;
  agent: string;
  model: string;
  state: "running" | "done" | "failed";
  costUsd: number;
  turns: number;
  tools: Record<string, number>;
  lastText?: string;
  started: string;
};

export type View = {
  status: string;
  stage: StageId;
  plans: { version: number; digest: string; plan: Plan; costUsd: number }[];
  approved?: { version: number; approver: string; digest: string };
  waitingOnSponsor: boolean;
  tasks: AgentTask[];
  costUsd: number;
  sessions: { component: string; repo: string; pathGlobs: string[] }[];
};

const STATUS_BY_TYPE: Record<string, string> = {
  "ap.work.item.assigned.v1": "received",
  "ap.work.request.rejected.v1": "rejected",
  "ap.work.plan.requested.v1": "planning",
  "ap.work.plan.proposed.v1": "plan-review",
  "ap.work.plan.revision.requested.v1": "planning",
  "ap.work.plan.approved.v1": "implementing",
  "ap.work.blocked.v1": "blocked",
  "ap.work.cancelled.v1": "cancelled",
  "ap.work.completed.v1": "done",
};

/** Parses feed timestamps (UTC or offset) to epoch ms, so ordering is by instant, not by string. */
export const ms = (t: string) => Date.parse(t);

export function project(events: FeedEvent[], telemetry: Telemetry[], until: number | null): View {
  const v: View = { status: "received", stage: "intake", plans: [], waitingOnSponsor: false, tasks: [], costUsd: 0, sessions: [] };
  const tasks = new Map<string, AgentTask>();
  for (const e of events) {
    if (until !== null && ms(e.time) > until) break;
    v.status = STATUS_BY_TYPE[e.type] ?? v.status;
    const s = stageOf(e.type);
    if (s) v.stage = s;
    const d = e.data;
    switch (e.type) {
      case "ap.work.plan.requested.v1":
        v.stage = "planning";
        v.waitingOnSponsor = false;
        break;
      case "ap.work.plan.proposed.v1":
        v.plans.push({ version: d.version as number, digest: d.digest as string, plan: d.plan as Plan, costUsd: d.costUsd as number });
        v.waitingOnSponsor = true;
        break;
      case "ap.work.plan.approved.v1":
        v.approved = { version: d.version as number, approver: d.approver as string, digest: d.digest as string };
        v.waitingOnSponsor = false;
        break;
      case "ap.devsession.requested.v1":
        v.sessions.push({ component: d.component as string, repo: d.repo as string, pathGlobs: (d.pathGlobs as string[]) ?? [] });
        break;
      case "ap.agent.task.assigned.v1":
        tasks.set(d.taskId as string, {
          taskId: d.taskId as string, agent: d.agent as string, model: d.model as string,
          state: "running", costUsd: 0, turns: 0, tools: {}, started: e.time,
        });
        break;
      case "ap.agent.task.completed.v1": {
        const t = tasks.get(d.taskId as string);
        if (t) {
          t.state = d.conclusion === "success" ? "done" : "failed";
          t.costUsd = (d.costUsd as number) ?? 0;
          t.turns = (d.turns as number) ?? 0;
        }
        v.costUsd += (d.costUsd as number) ?? 0;
        break;
      }
    }
  }
  for (const t of telemetry) {
    if (until !== null && ms(t.time) > until) continue;
    const task = tasks.get(t.task);
    if (!task) continue;
    if (t.body.kind === "tool" && t.body.name) task.tools[t.body.name] = (task.tools[t.body.name] ?? 0) + 1;
    if (t.body.kind === "text" && t.body.text) task.lastText = t.body.text;
    if (t.body.kind === "status" && t.body.status === "done" && task.state === "running") {
      task.turns = t.body.turns ?? task.turns;
    }
  }
  v.tasks = [...tasks.values()];
  return v;
}

// ---------------------------------------------------------------------------
// Plain-English narration of each event, for visitors.

export type Who = "human" | "agent" | "pipeline";

export function describe(e: FeedEvent): { who: Who; text: string } | null {
  const d = e.data;
  const str = (k: string) => String(d[k] ?? "");
  switch (e.type) {
    case "ap.work.item.assigned.v1":
      return { who: "human", text: `${str("sponsor")} assigned ${str("key")} to the agent` };
    case "ap.work.item.commented.v1":
      return { who: "human", text: `${str("author")} commented: “${truncate(str("body"), 140)}”` };
    case "ap.work.request.received.v1":
      return { who: "pipeline", text: "Intake router turned the assignment into a work request" };
    case "ap.work.request.accepted.v1":
      return { who: "pipeline", text: "Eligibility checks passed: issue type, sponsor identity, acceptance criteria" };
    case "ap.work.request.rejected.v1":
      return { who: "pipeline", text: `Rejected at intake: ${str("reason")} (${str("detail")})` };
    case "ap.work.guidance.provided.v1":
      return { who: "pipeline", text: `Recorded ${str("author")}'s comment as trusted guidance (${str("authorRole")})` };
    case "ap.work.repos.resolved.v1":
      return { who: "pipeline", text: `Resolved repositories: ${((d.repos as { repo: string }[]) ?? []).map((r) => r.repo).join(", ")}` };
    case "ap.work.plan.requested.v1":
      return { who: "pipeline", text: `Requested implementation plan v${str("version")}${d.revisionFeedback ? ` with feedback: “${truncate(str("revisionFeedback"), 100)}”` : ""}` };
    case "ap.agent.task.assigned.v1":
      return { who: "agent", text: `${label(str("agent"))} started in a sandbox (${str("model")}, budget $${str("maxBudgetUsd")})` };
    case "ap.agent.task.completed.v1":
      return { who: "agent", text: `Agent task ${str("conclusion")} after ${str("turns")} turns, $${num(d.costUsd)}` };
    case "ap.agent.proposal.created.v1":
      {
      const kind = str("kind").replace(/-/g, " ");
      return { who: "agent", text: `Agent proposed ${/^[aeiou]/.test(kind) ? "an" : "a"} ${kind}. Proposals are never applied directly` };
    }
    case "ap.policy.decision.recorded.v1": {
      const reasons = (d.reasons as string[] | undefined) ?? [];
      return { who: "pipeline", text: `Policy ${str("query")}: ${str("result")}${reasons.length ? ` (${truncate(reasons.join("; "), 140)})` : ""}` };
    }
    case "ap.work.plan.proposed.v1":
      return { who: "pipeline", text: `Plan v${str("version")} validated and sent to the sponsor for approval` };
    case "ap.work.plan.revision.requested.v1":
      return { who: "human", text: `${str("requestedBy")} asked for changes to plan v${str("version")}` };
    case "ap.command.received.v1":
      return { who: "human", text: `${str("issuer")} sent /ap ${str("command")}${d.args ? " " + str("args") : ""}` };
    case "ap.command.rejected.v1":
      return { who: "pipeline", text: `/ap ${str("command")} rejected: ${str("reason")}` };
    case "ap.command.accepted.v1":
      return null;
    case "ap.work.plan.approved.v1":
      return { who: "human", text: `${str("approver")} approved plan v${str("version")}. Approval is bound to its content hash` };
    case "ap.devsession.requested.v1":
      return { who: "pipeline", text: `Coding session requested for ${str("repo")}, limited to ${((d.pathGlobs as string[]) ?? []).length} path patterns` };
    case "ap.changeset.created.v1":
      return { who: "pipeline", text: `Grouped the changes into one changeset; merge order ${((d.mergeOrder as string[]) ?? []).join(" → ")}` };
    case "ap.work.blocked.v1":
      return { who: "pipeline", text: `Blocked: ${str("reason")}` };
    case "ap.agent.escalated.v1":
      return { who: "agent", text: `Escalated to the sponsor: ${str("reason")}` };
    case "ap.work.cancelled.v1":
      return { who: "pipeline", text: `Cancelled: ${str("reason")}` };
    default:
      return { who: "pipeline", text: e.type.replace(/^ap\./, "").replace(/\.v\d+$/, "") };
  }
}

export function label(agent: string): string {
  return agent.replace(/-/g, " ").replace(/^\w/, (c) => c.toUpperCase());
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function num(v: unknown): string {
  return typeof v === "number" ? v.toFixed(2) : "0.00";
}
