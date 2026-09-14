import type { OrbState } from "thinking-orbs";

import type { ToolCall, TranscriptItem } from "./types";

/**
 * One exchange: what you asked, what the agent did about it, and what it said.
 *
 * The transcript arrives as a flat stream of ACP rows — chunks, tool calls,
 * thoughts, subagents — and drawn flat, a single question that took forty tool
 * calls buried its own answer under forty cards. Grouped, the work folds into
 * one line ("Worked for 4m 7s") and the answer sits directly under the
 * question it answers.
 */
export type Turn = {
  /** Stable across re-renders: the prompt's id, or the first row's. */
  id: string;
  /** The user's message. Absent for whatever the agent said before anyone spoke. */
  prompt?: TranscriptItem;
  /** Thinking, tool calls, subagents, plans, and what the agent said between them. */
  work: TranscriptItem[];
  /** What the agent said after its last piece of work. */
  answer: TranscriptItem[];
  /** Rows this build does not model, retained for forward-compatible history. */
  notes: TranscriptItem[];
};

const WORK_KINDS = new Set([
  "agent_thought_chunk",
  "tool_call",
  "tool_call_update",
  "subagent_spawned",
  "subagent_state_update",
  "plan",
]);

/**
 * Splits a transcript into turns.
 *
 * The answer is provisional until the turn ends. Agent text lands in `answer`
 * as it streams, and the moment another piece of work arrives it moves into
 * `work` — it was narration ("I'll start by reading the config"), not the reply.
 * Whatever text is still standing after the last tool call is the answer. That
 * is the only rule that holds while a turn is live, because nothing on the wire
 * marks a message as final.
 */
type TurnDraft = {
  id: string;
  prompt?: TranscriptItem;
  work: TranscriptItem[];
  answer: TranscriptItem[];
  notes: TranscriptItem[];
};

function sameItems(left: TranscriptItem[], right: TranscriptItem[]): boolean {
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return false;
  }
  return true;
}

/**
 * Groups a transcript while retaining the Turn objects that did not change.
 *
 * The rows are still scanned because a later work row can reclassify the
 * answer immediately before it. That only allocates drafts; React sees the
 * old Turn identity for every unaffected exchange.
 */
export function groupTurns(items: TranscriptItem[], previous: Turn[] = []): Turn[] {
  const drafts: TurnDraft[] = [];
  let current: TurnDraft | null = null;
  const open = (id: string, prompt?: TranscriptItem): TurnDraft => {
    const turn: TurnDraft = { id, prompt, work: [], answer: [], notes: [] };
    drafts.push(turn);
    return turn;
  };

  for (const item of items) {
    const kind = String(item.kind);
    if (kind === "user_message_chunk") {
      current = open(item.id, item);
      continue;
    }
    current ??= open(item.id);
    if (kind === "agent_message_chunk") {
      current.answer.push(item);
    } else if (WORK_KINDS.has(kind)) {
      for (const answer of current.answer) current.work.push(answer);
      current.work.push(item);
      current.answer = [];
    } else {
      current.notes.push(item);
    }
  }
  return drafts.map((draft, index) => {
    const prior = previous[index];
    if (
      prior &&
      prior.id === draft.id &&
      prior.prompt === draft.prompt &&
      sameItems(prior.work, draft.work) &&
      sameItems(prior.answer, draft.answer) &&
      sameItems(prior.notes, draft.notes)
    ) {
      return prior;
    }
    return draft;
  });
}

/** The first and last moment anything in the turn happened, as epoch ms. */
const spanCache = new WeakMap<Turn, [number, number] | null>();
function span(turn: Turn): [number, number] | undefined {
  const cached = spanCache.get(turn);
  if (cached !== undefined) return cached ?? undefined;

  let first: number | undefined;
  let last: number | undefined;
  const visit = (row: TranscriptItem | undefined) => {
    if (!row) return;
    const start = Date.parse(row.createdAt ?? "");
    if (Number.isFinite(start) && (first === undefined || start < first)) first = start;
    const end = Date.parse(row.updatedAt ?? row.createdAt ?? "");
    if (Number.isFinite(end) && (last === undefined || end > last)) last = end;
  };
  visit(turn.prompt);
  for (const row of turn.work) visit(row);
  for (const row of turn.answer) visit(row);
  const range: [number, number] | null = first === undefined || last === undefined ? null : [first, last];
  spanCache.set(turn, range);
  return range ?? undefined;
}

/** "12s", "4m 7s", "1h 2m" — the precision a person uses, not a stopwatch's. */
export function formatElapsed(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/**
 * "Worked for 4m 7s". Past tense on purpose: this is the label of a finished
 * turn, and the same sentence in the present while the orb spins would say the
 * clock had stopped. Just "Worked" when the rows carry no timestamps.
 */
export function workedFor(turn: Turn): string {
  const range = span(turn);
  return range ? `Worked for ${formatElapsed(range[1] - range[0])}` : "Worked";
}

/**
 * What the agent is doing right now, as a sentence and as one of the orb's
 * nine verbs.
 *
 * The sentence is the tool's own title where there is one — "Read
 * package.json", the command it is running — because a specific line is
 * reassurance and "Working…" is a spinner with words on it.
 */
export function activityOf(turn: Turn): { label: string; orb: OrbState } {
  if (turn.answer.length > 0) return { label: "Writing", orb: "composing" };
  const last = turn.work[turn.work.length - 1];
  if (!last) return { label: "Starting", orb: "breathing" };

  switch (String(last.kind)) {
    case "agent_thought_chunk":
      return { label: "Thinking", orb: "solving" };
    case "plan":
      return { label: "Planning", orb: "weaving" };
    case "subagent_spawned":
    case "subagent_state_update": {
      const name = last.subagent?.name;
      return { label: name ? `Running ${name}` : "Running a subagent", orb: "connecting" };
    }
    default: {
      const call = last.toolCall;
      const kind = call?.kind?.toLowerCase() ?? "";
      const orb: OrbState =
        kind === "read" || kind === "search" || kind === "fetch"
          ? "searching"
          : kind === "edit" || kind === "delete" || kind === "move"
            ? "shaping"
            : "working";
      return { label: call?.title || "Using a tool", orb };
    }
  }
}

/**
 * The inside of a turn's work, in the order it happened: what the agent said,
 * a plan, and runs of steps between them.
 *
 * Steps run together until the agent speaks again. That is the grouping a
 * person reads — "it looked at six files, then said what it found" — and it is
 * what keeps a turn of forty calls to a handful of groups under a handful of
 * sentences, instead of forty lines.
 */
export type WorkSegment =
  | { kind: "text"; item: TranscriptItem }
  | { kind: "plan"; item: TranscriptItem }
  | { kind: "steps"; id: string; items: TranscriptItem[] };

export function workSegments(work: TranscriptItem[]): WorkSegment[] {
  const out: WorkSegment[] = [];
  for (const item of work) {
    const kind = String(item.kind);
    if (kind === "agent_message_chunk") {
      // An empty message is not speech, so it does not split the steps around it.
      if (item.text?.trim()) out.push({ kind: "text", item });
      continue;
    }
    if (kind === "plan") {
      const existing = out.find((segment) => segment.kind === "plan");
      if (existing?.kind === "plan") existing.item = item;
      else out.push({ kind: "plan", item });
      continue;
    }
    const last = out[out.length - 1];
    if (last?.kind === "steps") last.items.push(item);
    else out.push({ kind: "steps", id: item.id, items: [item] });
  }
  return out;
}

const STEP_PHRASES: Record<string, string> = {
  read: "read files",
  edit: "edited files",
  delete: "edited files",
  move: "edited files",
  search: "searched files",
  execute: "ran commands",
  fetch: "searched the web",
  think: "ran subagents",
};

/**
 * "Read files, ran commands, searched the web" — what a run of steps did, one
 * phrase per kind of step, in the order each kind first appeared.
 */
export function stepSummary(items: TranscriptItem[]): string {
  const phrases: string[] = [];
  for (const item of items) {
    const kind = String(item.kind);
    const phrase =
      kind === "agent_thought_chunk"
        ? "thought"
        : kind.startsWith("subagent_")
          ? "ran subagents"
          : (STEP_PHRASES[item.toolCall?.kind?.toLowerCase() ?? ""] ?? "used tools");
    if (!phrases.includes(phrase)) phrases.push(phrase);
  }
  const text = phrases.join(", ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * One step as a verb and what it acted on: "Read" + "hook.go".
 *
 * Built on the titles claude-agent-acp 0.74 writes (tools.js): "Read <path>",
 * with " (1 - 50)" when it read a range; "Write <path>"; "Edit <path>";
 * "Find …" and "grep …" for Glob and Grep; "Fetch <url>"; `Search "<query>"`;
 * and a Bash call titled with the command itself. A file is named by its last
 * segment — the path relative to the project is noise at a glance, and the row
 * opens onto the full location. A title in no known shape is shown whole.
 */
export function stepLabel(call: ToolCall): { verb: string; object: string } {
  const title = call.title?.trim() ?? "";
  const [, head = "", rest = ""] = /^(\S+)\s+([\s\S]+)$/.exec(title) ?? [];
  const file = (path: string) =>
    path.replace(/\s\((?:from line \d+|\d+ - \d+)\)$/, "").split("/").pop() || path;

  switch (head) {
    case "Read":
      return { verb: "Read", object: file(rest) };
    case "Write":
      return { verb: "Wrote", object: file(rest) };
    case "Edit":
      return { verb: "Edited", object: file(rest) };
    case "Fetch":
      return { verb: "Fetched", object: rest };
    case "Search":
      return { verb: "Searched the web for", object: rest.replace(/^"([\s\S]*)"$/, "$1") };
    case "Find":
    case "grep":
      return { verb: "Searched for", object: rest };
  }
  if (call.kind?.toLowerCase() === "execute" && title) return { verb: "Ran", object: title };
  return { verb: title || "Used a tool", object: "" };
}
