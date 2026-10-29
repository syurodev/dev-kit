import type { OrbState } from "thinking-orbs";

import type { Subagent, ToolCall, TranscriptItem } from "./types";

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
  /**
   * What each subagent this turn spawned did, by subagent id — its narration,
   * its steps and its report, nested subagents' included. Those rows arrive
   * interleaved with the session's own (Claude streams parallel subagents into
   * the same pipe), so they are kept here rather than in `work`.
   */
  subagents: Readonly<Record<string, TranscriptItem[]>>;
};

const NO_SUBAGENTS: Readonly<Record<string, TranscriptItem[]>> = Object.freeze({});

const WORK_KINDS = new Set([
  "agent_thought_chunk",
  "tool_call",
  "tool_call_update",
  "subagent_spawned",
  "subagent_state_update",
  "async_task_spawned",
  "async_task_progress",
  "async_task_state_update",
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
  subagents: Readonly<Record<string, TranscriptItem[]>>;
};

function sameItems(left: TranscriptItem[], right: TranscriptItem[]): boolean {
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return false;
  }
  return true;
}

function sameSubagents(
  left: Readonly<Record<string, TranscriptItem[]>>,
  right: Readonly<Record<string, TranscriptItem[]>>,
): boolean {
  const keys = Object.keys(left);
  if (keys.length !== Object.keys(right).length) return false;
  return keys.every((key) => right[key] === left[key]);
}

const CHUNK_KINDS = new Set(["agent_message_chunk", "agent_thought_chunk", "user_message_chunk"]);

/**
 * One message that other rows landed in the middle of, as the pieces it
 * arrived in. The joined item is kept per first piece so a message whose
 * pieces did not change keeps its identity across regroupings.
 */
const joined = new WeakMap<TranscriptItem, { parts: TranscriptItem[]; item: TranscriptItem }>();

function joinMessage(parts: TranscriptItem[]): TranscriptItem {
  if (parts.length === 1) return parts[0];
  const cached = joined.get(parts[0]);
  if (cached && sameItems(cached.parts, parts)) return cached.item;
  const last = parts[parts.length - 1];
  const item: TranscriptItem = {
    ...parts[0],
    text: parts.map((part) => part.text ?? "").join(""),
    updatedAt: last.updatedAt ?? last.createdAt ?? parts[0].updatedAt,
  };
  joined.set(parts[0], { parts, item });
  return item;
}

/** Claude's subagent-spawning tool, under its current and its older name. */
const SUBAGENT_TOOLS = new Set(["Agent", "Task"]);

/**
 * A row that says nothing. claude-agent-acp answers its Agent tool (what
 * spawned a subagent) with a bare tool_call_update and never the tool_call it
 * updates; the subagent's own row already tells that story, and this one drew
 * as "Used a tool" beside it.
 */
function isEmptyToolRow(item: TranscriptItem): boolean {
  const call = item.toolCall;
  if (!call || !SUBAGENT_TOOLS.has(call.name ?? "")) return false;
  return (
    !call.title &&
    !call.kind &&
    !call.status &&
    !call.content?.length &&
    !call.locations?.length &&
    call.rawInput === undefined &&
    call.rawOutput === undefined
  );
}

/**
 * The session's own rows, in order, with a message split by other rows put
 * back together; and every subagent's rows, by subagent id.
 *
 * Claude runs subagents in the session's own pipe: their narration, tool calls
 * and reports arrive interleaved with the lead agent's, tagged only by origin.
 * Drawn in arrival order, a parallel pair of subagents put sixty steps and four
 * reports into the lead agent's account, and cut its own sentences mid-word
 * wherever one of their rows arrived — "tôi sửa một ghi nhớ đ" | a Bash call |
 * "ã sai." — which the recorder keeps as separate rows, so a reload showed the
 * same cuts. A message is one message: its pieces share a messageId.
 *
 * A row whose origin names no subagent in view (its spawn is on an older page
 * not loaded yet) stays in the flow rather than disappearing.
 */
function splitFlow(items: readonly TranscriptItem[]): {
  main: TranscriptItem[];
  activity: Map<string, TranscriptItem[]>;
} {
  const known = new Set<string>();
  for (const item of items) {
    if (item.subagent?.id) known.add(item.subagent.id);
  }
  // Each list holds runs of pieces while it is built, joined once at the end:
  // joining as each piece arrived asked the cache for [a, b] and then
  // [a, b, c], so the next grouping's [a, b] missed and minted a new item —
  // and every turn holding a message in three pieces lost its identity on
  // every update, which re-rendered the whole transcript per streamed token.
  const mainRuns: TranscriptItem[][] = [];
  const activityRuns = new Map<string, TranscriptItem[][]>();
  const push = (runs: TranscriptItem[][], item: TranscriptItem) => {
    const run = runs[runs.length - 1];
    const last = run?.[run.length - 1];
    if (
      last &&
      CHUNK_KINDS.has(String(item.kind)) &&
      item.kind === last.kind &&
      item.messageId &&
      item.messageId === last.messageId
    ) {
      run.push(item);
      return;
    }
    runs.push([item]);
  };

  for (const item of items) {
    if (isEmptyToolRow(item)) continue;
    const origin = item.origin;
    if (origin && known.has(origin)) {
      let runs = activityRuns.get(origin);
      if (!runs) {
        runs = [];
        activityRuns.set(origin, runs);
      }
      push(runs, item);
      continue;
    }
    push(mainRuns, item);
  }
  const activity = new Map<string, TranscriptItem[]>();
  for (const [origin, runs] of activityRuns) activity.set(origin, runs.map(joinMessage));
  return { main: mainRuns.map(joinMessage), activity };
}

export type LiveSubagent = { subagent: Subagent; activity: TranscriptItem[] };

/**
 * The subagents still running, in the order they were spawned (nested ones
 * after their parent), each with what it has done so far — and every
 * subagent's activity by id, for drawing a nested one inside it.
 *
 * A subagent with no state yet is running: its spawn carries none, and only
 * the state update that ends it says otherwise. Callers ask only while the
 * session is live, since a subagent cut off with its session never gets one.
 */
export function runningSubagents(items: readonly TranscriptItem[]): {
  running: LiveSubagent[];
  activity: Readonly<Record<string, TranscriptItem[]>>;
} {
  const { main, activity } = splitFlow(items);
  const running: LiveSubagent[] = [];
  const seen = new Set<string>();
  const visit = (rows: TranscriptItem[]) => {
    for (const row of rows) {
      const subagent = row.subagent;
      if (!subagent?.id || seen.has(subagent.id)) continue;
      seen.add(subagent.id);
      const rowsOf = activity.get(subagent.id) ?? [];
      if ((subagent.state ?? "running") === "running") running.push({ subagent, activity: rowsOf });
      visit(rowsOf);
    }
  };
  visit(main);
  return { running, activity: Object.fromEntries(activity) };
}

/**
 * What a subagent is doing now, as a line: its last step's own label, or that
 * it is writing when its last row is words — the report, most often.
 */
export function currentStep(activity: TranscriptItem[]): string {
  const last = activity[activity.length - 1];
  if (!last) return "Starting";
  const kind = String(last.kind);
  if (kind === "agent_message_chunk") return "Writing";
  if (kind === "agent_thought_chunk") return "Thinking";
  if (last.subagent) return `Running ${last.subagent.name || "a subagent"}`;
  if (last.toolCall) {
    if (isToolSearch(last.toolCall)) return toolSearchLine(last.toolCall);
    const { verb, object } = stepLabel(last.toolCall);
    return object ? `${verb} ${object}` : verb;
  }
  return "Working";
}

/** How many steps — everything but its words — a subagent has taken. */
export function stepCount(activity: TranscriptItem[]): number {
  return activity.filter((row) => String(row.kind) !== "agent_message_chunk").length;
}

/** The activity of every subagent in `work`, and of the subagents those spawned. */
function subagentsOf(
  work: TranscriptItem[],
  activity: Map<string, TranscriptItem[]>,
  prior: Readonly<Record<string, TranscriptItem[]>> | undefined,
): Readonly<Record<string, TranscriptItem[]>> {
  if (activity.size === 0) return NO_SUBAGENTS;
  let out: Record<string, TranscriptItem[]> | null = null;
  const visit = (rows: TranscriptItem[]) => {
    for (const row of rows) {
      const id = row.subagent?.id;
      if (!id || out?.[id]) continue;
      const rowsOf = activity.get(id);
      if (!rowsOf) continue;
      out ??= {};
      // The previous grouping's array when nothing in it changed, so the turn
      // around it can keep its identity too.
      out[id] = prior?.[id] && sameItems(prior[id], rowsOf) ? prior[id] : rowsOf;
      visit(rowsOf);
    }
  };
  visit(work);
  return out ?? NO_SUBAGENTS;
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
    const turn: TurnDraft = { id, prompt, work: [], answer: [], notes: [], subagents: NO_SUBAGENTS };
    drafts.push(turn);
    return turn;
  };

  const { main, activity } = splitFlow(items);
  for (const item of main) {
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
  // Matched by id, not position: an older page lands in front and shifts
  // every held turn along. The shift is found once, from the first turn held
  // before; the common case (nothing prepended) costs one comparison.
  let shift = 0;
  if (previous.length > 0 && drafts[0]?.id !== previous[0].id) {
    shift = drafts.findIndex((draft) => draft.id === previous[0].id);
  }
  return drafts.map((draft, index) => {
    const prior = shift < 0 ? undefined : previous[index - shift];
    const matched = prior?.id === draft.id ? prior : undefined;
    draft.subagents = subagentsOf(draft.work, activity, matched?.subagents);
    if (
      matched &&
      matched.prompt === draft.prompt &&
      sameItems(matched.work, draft.work) &&
      sameItems(matched.answer, draft.answer) &&
      sameItems(matched.notes, draft.notes) &&
      sameSubagents(matched.subagents, draft.subagents)
    ) {
      return matched;
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
  if (!last) {
    const phase = turn.notes[turn.notes.length - 1]?.phase;
    const labels: Record<string, string> = {
      saving_checkpoint: "Saving checkpoint…",
      opening_agent: "Opening agent…",
      waiting_for_model: "Waiting for the model…",
    };
    return { label: (phase && labels[phase]) || "Starting", orb: "breathing" };
  }

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
    case "async_task_spawned":
    case "async_task_progress":
    case "async_task_state_update": {
      const name = last.asyncTask?.name || last.asyncTask?.description;
      return { label: name ? `${name} in the background` : "Working in the background", orb: "connecting" };
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
      if (call && isToolSearch(call)) return { label: toolSearchLine(call), orb };
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

/**
 * The index of the newest turn that holds a plan of the session's own: the
 * one whose plan the summary card shows. A subagent's plan is its own. -1
 * when no turn has one. One backwards pass, run once per grouping.
 */
export function newestPlanTurnIndex(turns: readonly Turn[]): number {
  for (let i = turns.length - 1; i >= 0; i -= 1) {
    if (turns[i].work.some((item) => String(item.kind) === "plan" && !item.origin)) return i;
  }
  return -1;
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
 * A compaction, which claude-agent-acp 0.78 reports as a `think` tool call —
 * the kind its Task/Agent tool uses too. Only the tool name tells them apart.
 */
export function isCompaction(call?: ToolCall | null): boolean {
  return call?.name?.toLowerCase() === "compact";
}

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
          : kind.startsWith("async_task_")
            ? "ran background tasks"
          : isCompaction(item.toolCall)
            ? "compacted the conversation"
            : isToolSearch(item.toolCall)
              ? isFailed(item.toolCall?.status)
                ? "couldn't load tools"
                : "loaded tools"
            : (STEP_PHRASES[item.toolCall?.kind?.toLowerCase() ?? ""] ?? "used tools");
    if (!phrases.includes(phrase)) phrases.push(phrase);
  }
  const text = phrases.join(", ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * One step as a verb and what it acted on: "Read" + "hook.go".
 *
 * Built on the titles claude-agent-acp writes (tools.js; 0.74, unchanged in a
 * 0.78 capture): "Read <path>",
 * with " (1 - 50)" when it read a range; "Write <path>"; "Edit <path>";
 * "Find …" and "grep …" for Glob and Grep; "Fetch <url>"; `Search "<query>"`;
 * and a Bash call titled with the command itself. A file is named by its last
 * segment — the path relative to the project is noise at a glance, and the row
 * opens onto the full location. A title in no known shape is shown whole.
 *
 * A command is named by what it is for when the agent said so: Claude sends a
 * Bash call's own `description` ("Verify docker, pongo and created files"),
 * and a command's first line is usually `cd` into the project — six rows in a
 * captured session read "Ran cd /home/…/kong-plugin" and nothing else. The
 * command itself is in the row's body. Without a description, a leading `cd`
 * is dropped for the same reason.
 *
 * An MCP tool is titled with its wire name, `mcp__devkit__tasks_transition`;
 * it reads as the tool's own words and, quieter, the server it is on.
 */
export function stepLabel(call: ToolCall): { verb: string; object: string } {
  const title = call.title?.trim() ?? "";
  const mcp = /^mcp__(.+?)__(.+)$/.exec(title || (call.name ?? ""));
  if (mcp) {
    const words = mcp[2]
      .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
      .replace(/[_.]+/g, " ")
      .trim()
      .toLowerCase();
    return { verb: words.charAt(0).toUpperCase() + words.slice(1), object: mcp[1] };
  }
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
  const builtin = builtinLabel(call, head || title);
  if (builtin) return builtin;
  if (call.kind?.toLowerCase() === "execute") {
    const description = commandDescription(call);
    if (description) return { verb: description, object: "" };
    if (title) return { verb: "Ran", object: title.replace(LEADING_CD, "") || title };
  }
  return { verb: title || "Used a tool", object: "" };
}

function builtinName(call: ToolCall, titleHead: string): string {
  // A Bash command is titled with the command itself, so a title's first word
  // names a tool only for a call that is not a shell command and has no name.
  return call.name?.trim() || (call.kind?.toLowerCase() === "execute" ? "" : titleHead);
}

function isFailed(status?: string): boolean {
  return status?.toLowerCase() === "failed" || status?.toLowerCase() === "error";
}

/** Whether a call is still going (the states AgentToolCall draws as pending, running, awaiting). */
function isUnderWay(status?: string): boolean {
  const state = status?.toLowerCase();
  return state === "pending" || state === "in_progress" || state === "running" || state === "awaiting_permission" || state === "awaiting";
}

/** Claude's ToolSearch: it loads deferred tools (`select:<Name>[,<Name>]`) or looks them up by keyword. */
export function isToolSearch(call?: ToolCall | null): boolean {
  return Boolean(call) && builtinName(call!, call!.title?.trim().split(/\s+/)[0] ?? "") === "ToolSearch";
}

const TOOL_TERM_MAX = 80;
// Typed by hand: the project's `lib` predates Intl.Segmenter, which every shipped webview has.
type Segmenter = { segment(text: string): Iterable<{ segment: string }> };
const SegmenterCtor = (Intl as unknown as { Segmenter?: new () => Segmenter }).Segmenter;
const graphemes = SegmenterCtor ? new SegmenterCtor() : null;

/** A term cut to `TOOL_TERM_MAX` characters as a person counts them, never inside a surrogate pair or a combined sequence. */
function clipTerm(term: string): string {
  if (term.length <= TOOL_TERM_MAX) return term;
  const parts = graphemes ? Array.from(graphemes.segment(term), (part) => part.segment) : Array.from(term);
  return parts.length > TOOL_TERM_MAX ? `${parts.slice(0, TOOL_TERM_MAX - 1).join("")}…` : term;
}

/**
 * What a ToolSearch asked for: the tool names of a `select:A,B` query, or the
 * words of a keyword one, each clipped. Its input is `{query, max_results}`.
 */
export function toolSearchTerms(call: ToolCall): { select: boolean; terms: string[] } {
  const input = call.rawInput as { query?: unknown } | null | undefined;
  const query = typeof input?.query === "string" ? input.query.trim() : "";
  const select = /^select:/i.test(query);
  const terms = (select ? query.slice("select:".length).split(",") : query.split(/\s+/))
    .map((term) => term.trim())
    .filter(Boolean)
    .map(clipTerm);
  return { select, terms };
}

/** A ToolSearch as one line: "Loading tools" while it runs, else its row's words. */
function toolSearchLine(call: ToolCall): string {
  if (isUnderWay(call.status)) return "Loading tools";
  const { verb, object } = stepLabel(call);
  return object ? `${verb} ${object}` : verb;
}

/** One entry of a run once its ToolSearch calls are folded: a step as it is, or a stretch of searches. */
export type FoldedStep = { kind: "step"; item: TranscriptItem } | { kind: "loaded"; id: string; items: TranscriptItem[] };

/** The React key of an entry: a stretch is `loaded:` its first call, so it never meets a step's id. */
export function foldedStepKey(entry: FoldedStep): string {
  return entry.kind === "step" ? entry.item.id : `loaded:${entry.id}`;
}

/**
 * A stretch of consecutive ToolSearch calls is one "Loaded tools" entry — 274 of
 * 7,048 calls, mostly `select:` loads, said nothing the next step did not.
 * A failed search stays a step of its own, and ends the stretch before it.
 */
export function foldToolSearch(items: readonly TranscriptItem[]): FoldedStep[] {
  const out: FoldedStep[] = [];
  for (const item of items) {
    if (isToolSearch(item.toolCall) && !isFailed(item.toolCall?.status)) {
      const last = out[out.length - 1];
      if (last?.kind === "loaded") last.items.push(item);
      else out.push({ kind: "loaded", id: item.id, items: [item] });
    } else {
      out.push({ kind: "step", item });
    }
  }
  return out;
}

/**
 * Claude's own tools that carry no path or command for a title to be built
 * from, worded by name and what the input says. The keys are Claude Code's
 * tool schemas — Skill `{skill, args}`, Monitor `{command, description}`,
 * SendMessage `{to, message}` — read defensively, since no recorded transcript
 * holds one of each.
 *
 * Past tense only once the call is done. While it is under way the vendor's
 * own title stands when it has one (ExitPlanMode asks "Ready to code?" until
 * answered), else a present form; a failed call says it could not.
 */
function builtinLabel(call: ToolCall, titleHead: string): { verb: string; object: string } | null {
  const tool = builtinName(call, titleHead);
  const input = (call.rawInput && typeof call.rawInput === "object" ? call.rawInput : {}) as Record<string, unknown>;
  const text = (...keys: string[]) => {
    for (const key of keys) {
      const value = input[key];
      if (typeof value === "string" && value.trim()) return value.trim();
    }
    return "";
  };
  const title = call.title?.trim() ?? "";
  // A vendor title is anything but the bare tool name.
  const vendorTitle = title && title.toLowerCase() !== tool.toLowerCase() ? title : "";
  const failed = isFailed(call.status);
  const underWay = isUnderWay(call.status);
  /** The three tenses of one sentence: done, under way, failed. */
  const say = (done: string, now: string, couldnt: string, object = "") => {
    if (underWay && vendorTitle) return { verb: vendorTitle, object: "" };
    return { verb: failed ? couldnt : underWay ? now : done, object };
  };
  switch (tool) {
    case "ToolSearch": {
      const { select, terms } = toolSearchTerms(call);
      const object = terms.join(select ? ", " : " ");
      return failed ? { verb: "Couldn't load", object } : { verb: "Loaded tools", object };
    }
    case "Skill": {
      const skill = text("skill", "name", "command");
      // Claude's title already names it ("Load skill: superpowers:brainstorming").
      if (vendorTitle && (!skill || vendorTitle.includes(skill))) return { verb: vendorTitle, object: "" };
      return skill
        ? say("Used skill", "Using skill", "Couldn't use skill", skill)
        : say("Used a skill", "Using a skill", "Couldn't use a skill");
    }
    case "Monitor": {
      // Not a watch without something to watch: Monitor has been called with
      // ToolSearch's {query, max_results}, and failed.
      const what = text("description");
      if (!what && !text("command")) return { verb: title || "Monitor", object: "" };
      return what
        ? say("Watched", "Monitoring", "Couldn't watch", what)
        : say("Watched a process", "Monitoring a process", "Couldn't watch a process");
    }
    case "TaskStop":
      return say("Stopped a task", "Stopping a task", "Couldn't stop a task");
    case "SendMessage": {
      const to = text("to");
      return to
        ? say("Sent a message to", "Sending a message to", "Couldn't send a message to", to)
        : say("Sent a message", "Sending a message", "Couldn't send a message");
    }
    case "EnterPlanMode":
      return say("Entered plan mode", "Entering plan mode", "Couldn't enter plan mode");
    case "ExitPlanMode":
      return say("Left plan mode", "Exiting plan mode", "Couldn't exit plan mode");
    case "ListAgents":
      return say("Listed agents", "Listing agents", "Couldn't list agents");
    case "SubagentHandback":
      return say("Handed back", "Handing back", "Couldn't hand back");
  }
  return null;
}

/** `cd <dir> &&`, `cd <dir>;` or `cd <dir>` on a line of its own, at the start. */
const LEADING_CD = /^cd\s+(?:"[^"]*"|'[^']*'|\S+)\s*(?:&&|;|\n)\s*/;

/** The command a shell tool call ran, when its input says. */
export function toolCommand(call: ToolCall): string {
  const input = call.rawInput as { command?: unknown } | null | undefined;
  return typeof input?.command === "string" ? input.command : "";
}

/** What the agent said a command was for (Claude's Bash `description`). */
export function commandDescription(call: ToolCall): string {
  const input = call.rawInput as { description?: unknown } | null | undefined;
  return typeof input?.description === "string" ? input.description.trim() : "";
}
