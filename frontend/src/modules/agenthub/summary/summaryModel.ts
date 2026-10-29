import { toolNameOf } from "@/workbench/toolPayload";
import { createToolRegistry, DEVKIT_WIRE_PREFIX } from "@/workbench/toolResultViews";
import type { CheckpointIndex } from "../checkpoints";
import { shortCount } from "../surfaces/AgentContextMeter";
import type {
  AsyncTask,
  ConfigOption,
  Connector,
  LimitWindow,
  PlanEntry,
  SessionMode,
  TokenBreakdown,
  TranscriptItem,
} from "../types";
import { CheckpointStatus } from "../../../../bindings/devkit/internal/agenthub/models";

/** The agent's most recent plan in the session, or none. A subagent's plan is its own, not the session's. */
export function latestPlan(items: readonly TranscriptItem[]): PlanEntry[] {
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const it = items[i];
    if (it.kind === "plan" && it.plan && !it.origin) return it.plan;
  }
  return [];
}

export type SessionChange = { path: string; added: number; removed: number; binary: boolean; checkpointId: string };

/**
 * Every file the session touched, its lines summed over turns; the diff opens
 * from the newest checkpoint that touched it. Newest-touched first, a turn's
 * own order kept inside it. Only settled checkpoints count, as in a turn's
 * header (`changeSummary`): a pending or failed one says nothing yet.
 */
export function sessionChanges(index: CheckpointIndex): SessionChange[] {
  const records = [...index.values()]
    .filter((r) => r.status === CheckpointStatus.CheckpointDone && r.changed?.length)
    .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt));
  const byPath = new Map<string, SessionChange & { turn: number; pos: number }>();
  records.forEach((record, turn) => {
    (record.changed ?? []).forEach((file, pos) => {
      const prev = byPath.get(file.path);
      byPath.set(file.path, {
        path: file.path,
        added: (prev?.added ?? 0) + file.added,
        removed: (prev?.removed ?? 0) + file.removed,
        binary: Boolean(prev?.binary || file.binary),
        checkpointId: record.id,
        turn,
        pos,
      });
    });
  });
  return [...byPath.values()]
    .sort((a, b) => b.turn - a.turn || a.pos - b.pos)
    .map(({ path, added, removed, binary, checkpointId }) => ({ path, added, removed, binary, checkpointId }));
}

const DATA_MODULES: Record<string, string> = { db: "Database", kafka: "Kafka", ssh: "SSH" };

/**
 * Only tools that touch a target's data make a module a source. Listing the
 * saved profiles, the active one, a connection test or history says nothing
 * was read from or run on the target, so none of them are here.
 */
const DATA_TOOLS = createToolRegistry<string>();
for (const name of [
  "db.query",
  "db.explain",
  "db.schema.get",
  "db.listForeignKeys",
  "kafka.peekMessages",
  "kafka.describeTopic",
  "kafka.listTopics",
  "kafka.listConsumerGroups",
  "kafka.publishMessage",
  "ssh.runCommand",
]) {
  DATA_TOOLS.register(name, name);
}

/**
 * The DevKit data tool a call names, as its Go name `module.tool`, or null
 * for anything else. Looked up forward, the way result views are
 * (createToolRegistry): the Go name or Claude's wire form
 * (`mcp__devkit__db_schema_get`), never by turning "_" back into "." — a Go
 * name may hold an underscore, and a vendor's own `db_query` is not ours.
 * Codex's MCP calls carry no name: the title is `server.tool`
 * (codex_translate.go), so a DevKit one reads `devkit.db.query`; its tail
 * may also come with underscores (`devkit.db_schema_get`), read as the wire form.
 */
function devkitTool(call: NonNullable<TranscriptItem["toolCall"]>): string | null {
  // The name, else the tail of Codex's title (toolNameOf); "" for neither.
  const name = toolNameOf(call);
  if (!name) return null;
  return DATA_TOOLS.lookup(name) ?? (call.name ? undefined : DATA_TOOLS.lookup(DEVKIT_WIRE_PREFIX + name)) ?? null;
}

export type ToolSource = { module: string; label: string; count: number; detail: string };

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** What the agent did with the module, in a few words, from the tools it called (`count` per tool name). */
function detailOf(prefix: string, tools: ReadonlyMap<string, number>): string {
  const n = (name: string) => tools.get(`${prefix}.${name}`) ?? 0;
  switch (prefix) {
    case "db":
      return n("query") > 0 ? plural(n("query"), "query", "queries") : "read the schema";
    case "kafka":
      if (n("publishMessage") > 0) return `published ${n("publishMessage")}`;
      if (n("peekMessages") > 0) return `peeked ${plural(n("peekMessages"), "topic", "topics")}`;
      return "read topics";
    default:
      return `ran ${plural(n("runCommand"), "command", "commands")}`;
  }
}

/** DevKit data tools the agent called, per module. `module` is the module manifest id. */
export function toolSources(items: readonly TranscriptItem[]): ToolSource[] {
  const byModule = new Map<string, Map<string, number>>();
  for (const it of items) {
    const tool = it.toolCall ? devkitTool(it.toolCall) : null;
    if (!tool) continue;
    const prefix = tool.slice(0, tool.indexOf("."));
    const tools = byModule.get(prefix) ?? new Map<string, number>();
    tools.set(tool, (tools.get(tool) ?? 0) + 1);
    byModule.set(prefix, tools);
  }
  return [...byModule].map(([prefix, tools]) => ({
    module: prefix === "db" ? "database" : prefix,
    label: DATA_MODULES[prefix],
    count: [...tools.values()].reduce((a, b) => a + b, 0),
    detail: detailOf(prefix, tools),
  }));
}

const URL_RE = /https?:\/\/[^\s<>()"'`]+/g;
const TRAILING_PUNCTUATION = /[.,;:!?]+$/;

export type LinkHost = { host: string; url: string };

function linkOf(raw: string): LinkHost | null {
  const trimmed = raw.replace(TRAILING_PUNCTUATION, "");
  try {
    const u = new URL(trimmed);
    if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.host) return null;
    return { host: u.host, url: trimmed };
  } catch {
    return null;
  }
}

/** One item's links. Items are not changed once echoed, so each is read once. */
const linksByItem = new WeakMap<TranscriptItem, LinkHost[]>();

function itemLinks(it: TranscriptItem): LinkHost[] {
  const cached = linksByItem.get(it);
  if (cached) return cached;
  const out: LinkHost[] = [];
  const push = (raw: string) => {
    const link = linkOf(raw);
    if (link) out.push(link);
  };
  // The person's own words: a subagent's echoed brief is the agent talking.
  if (it.kind === "user_message_chunk" && !it.origin && typeof it.text === "string") {
    for (const m of it.text.matchAll(URL_RE)) push(m[0]);
  }
  const input = it.toolCall?.rawInput as { url?: unknown } | undefined;
  if (it.toolCall?.kind === "fetch" && typeof input?.url === "string") push(input.url);
  linksByItem.set(it, out);
  return out;
}

/** One link per host, from what the person wrote and what the agent fetched; first seen first. */
export function linkHosts(items: readonly TranscriptItem[]): LinkHost[] {
  const seen = new Map<string, string>();
  for (const it of items) {
    for (const link of itemLinks(it)) if (!seen.has(link.host)) seen.set(link.host, link.url);
  }
  return [...seen].map(([host, url]) => ({ host, url }));
}

/**
 * A file name in two parts, so a narrow row can cut the stem and keep the
 * extension: `booking-note.spec.ts` → `booking-note` + `.spec.ts`. The tail is
 * the last extension, plus the one before it when that is short (`.spec`,
 * `.entity`, `.d`), and nothing for a dotfile or a name with no extension.
 */
export function splitFileName(name: string): { stem: string; tail: string } {
  const last = name.lastIndexOf(".");
  if (last <= 0 || last === name.length - 1) return { stem: name, tail: "" };
  const before = name.lastIndexOf(".", last - 1);
  const start = before > 0 && last - before <= 8 ? before : last;
  const tail = name.slice(start);
  return tail.length > 14 ? { stem: name.slice(0, last), tail: name.slice(last) } : { stem: name.slice(0, start), tail };
}

/**
 * A line of a summary section that is a name and a value, and how it reads:
 * `tone` is how safe it is (the chrome's status colours), never which kind.
 */
export type SummaryRow = {
  key: string;
  label: string;
  value: string;
  tone?: "ok" | "warning" | "danger";
  title?: string;
};

/** What the Environment section is drawn from. */
export type EnvironmentInput = {
  vendorName: string;
  /**
   * The name of the account the session runs as, once it is known. Absent for a
   * session on the built-in account that never moved, and while the list that
   * names the account is still on its way: the row is left out, never guessed.
   */
  accountLabel?: string | null;
  folderPath: string;
  workspaceName?: string | null;
  /** The repositories this work spans, with the branch each is on. */
  repos: readonly { name: string; branch: string; own: boolean }[];
  configOptions: readonly ConfigOption[];
  mode: string | null;
  availableModes: readonly SessionMode[];
};

function lastSegment(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  return trimmed.slice(Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\")) + 1) || trimmed;
}

/** A select option's current value as the person reads it: its name, else the value. */
function currentChoice(option: ConfigOption | undefined): string {
  if (!option || typeof option.currentValue !== "string" || !option.currentValue) return "";
  const match = option.options?.find((o) => o.value === option.currentValue);
  return match?.name || option.currentValue;
}

/**
 * Several repositories: the branch when they are all on one (`main · 15 repos`),
 * else how many branches there are. The tooltip groups the repositories by
 * branch, so "which of the fifteen is not on main" is one hover away.
 */
function branchesRow(repos: readonly { name: string; branch: string }[]): SummaryRow {
  const byBranch = new Map<string, string[]>();
  for (const repo of repos) byBranch.set(repo.branch, [...(byBranch.get(repo.branch) ?? []), repo.name]);
  const groups = [...byBranch.entries()].sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]));
  const count = `${repos.length} repos`;
  return {
    key: "branch",
    label: "Branches",
    value: groups.length === 1 ? `${groups[0][0]} · ${count}` : `${groups.length} branches · ${count}`,
    title: groups.map(([branch, names]) => `${branch} (${names.length}): ${names.join(", ")}`).join("\n"),
  };
}

/**
 * Where and how the session runs. Only what the session actually has: a row is
 * left out rather than drawn empty, so a vendor with no effort setting, or a
 * folder outside a workspace, simply has fewer rows.
 */
export function environmentRows(input: EnvironmentInput): SummaryRow[] {
  const rows: SummaryRow[] = [{ key: "agent", label: "Agent", value: input.vendorName }];
  if (input.accountLabel) rows.push({ key: "account", label: "Account", value: input.accountLabel });
  const model = currentChoice(input.configOptions.find((o) => o.id === "model"));
  if (model) rows.push({ key: "model", label: "Model", value: model });
  const effort = currentChoice(input.configOptions.find((o) => o.id === "reasoning_effort" || o.id === "effort"));
  if (effort) rows.push({ key: "effort", label: "Effort", value: effort });
  const mode = input.availableModes.find((m) => m.id === input.mode)?.name || input.mode;
  if (mode) rows.push({ key: "mode", label: "Mode", value: mode });
  if (input.workspaceName) rows.push({ key: "workspace", label: "Workspace", value: input.workspaceName });
  if (input.folderPath) {
    rows.push({ key: "folder", label: "Folder", value: lastSegment(input.folderPath), title: input.folderPath });
  }
  const repos = input.repos.filter((r) => r.branch);
  if (repos.length === 1) {
    rows.push({ key: "branch", label: "Branch", value: repos[0].branch, title: repos[0].name });
  } else if (repos.length > 1) {
    rows.push(branchesRow(repos));
  }
  return rows;
}

/** "resets in 42m" / "resets in 3h" / "resets in 2d": minutes until it is far enough out to count hours. */
export function resetsIn(resetsAt: number, now = Date.now()): string {
  const minutes = Math.max(0, Math.round((resetsAt * 1000 - now) / 60_000));
  if (minutes < 90) return `resets in ${minutes}m`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `resets in ${hours}h` : `resets in ${Math.round(hours / 24)}d`;
}

/** The windows still running: one past its reset started over, so what it last said is no longer true. */
export function liveLimits(limits: readonly LimitWindow[] | null | undefined, now = Date.now()): LimitWindow[] {
  return (limits ?? []).filter((limit) => !limit.resetsAt || limit.resetsAt * 1000 > now);
}

/** The plan's windows, one row each, as the vendor labelled them. */
export function limitRows(limits: readonly LimitWindow[] | null | undefined, now = Date.now()): SummaryRow[] {
  return liveLimits(limits, now).map((limit, i) => {
    const parts = [`${Math.round(limit.usedPercent)}%`];
    if (limit.resetsAt) parts.push(resetsIn(limit.resetsAt, now));
    return {
      key: `limit:${i}:${limit.label}`,
      label: limit.label,
      value: parts.join(" · "),
      tone: limit.status === "reached" ? "danger" : limit.status === "warning" ? "warning" : undefined,
    };
  });
}

/** The token split, for a vendor that reports one; nothing for one that does not. */
export function tokenRows(tokens: TokenBreakdown | null | undefined): SummaryRow[] {
  if (!tokens || tokens.session.total <= 0) return [];
  const s = tokens.session;
  const rows: SummaryRow[] = [{ key: "input", label: "Input", value: shortCount(s.input) }];
  if (s.input > 0 && s.cached > 0) {
    rows[0].value += ` · ${Math.round((s.cached / s.input) * 100)}% cached`;
  }
  const output = s.reasoning > 0 ? `${shortCount(s.output)} · ${shortCount(s.reasoning)} reasoning` : shortCount(s.output);
  rows.push({ key: "output", label: "Output", value: output });
  if (tokens.lastRequest.total > 0) {
    rows.push({ key: "last", label: "Last request", value: shortCount(tokens.lastRequest.total) });
  }
  return rows;
}

const CONNECTOR_STATE: Record<string, { word: string; tone?: SummaryRow["tone"] }> = {
  ready: { word: "Ready" },
  starting: { word: "Starting" },
  failed: { word: "Failed", tone: "danger" },
  cancelled: { word: "Cancelled", tone: "warning" },
};

/** The MCP servers the vendor started, one row each, in the order they started. */
export function connectorRows(connectors: readonly Connector[] | null | undefined): SummaryRow[] {
  return (connectors ?? []).map((c) => {
    const state = CONNECTOR_STATE[c.state] ?? { word: c.state || "Unknown", tone: undefined };
    return {
      key: `connector:${c.name}`,
      label: c.name,
      value: c.needsLogin ? "Sign-in needed" : state.word,
      tone: c.needsLogin ? "warning" : state.tone,
      title: c.error || undefined,
    };
  });
}

const RUNNING_TASK_STATES = new Set(["running", "pending", ""]);

/** Whether a background task is still going. */
export function isTaskRunning(task: AsyncTask): boolean {
  return RUNNING_TASK_STATES.has(task.state ?? "");
}

/**
 * The background tasks the vendor reported, one per id, running first. Only a
 * vendor that has the notion sends them (Claude does); for one that does not
 * the list is empty and the section is not drawn. A task a subagent started is
 * listed too: the process belongs to the session (and the session's count of
 * background tasks includes it), whoever started it.
 */
export function backgroundTasks(items: readonly TranscriptItem[]): AsyncTask[] {
  const byId = new Map<string, AsyncTask>();
  for (const item of items) {
    if (item.asyncTask?.id) byId.set(item.asyncTask.id, item.asyncTask);
  }
  const all = [...byId.values()];
  return [...all.filter(isTaskRunning), ...all.filter((t) => !isTaskRunning(t))];
}
