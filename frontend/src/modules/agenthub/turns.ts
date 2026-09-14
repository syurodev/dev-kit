import type { OrbState } from "thinking-orbs";

import type { TranscriptItem } from "./types";

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
  /** Rows that are neither: rate limits, invoke results, kinds this build does not model. */
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
export function groupTurns(items: TranscriptItem[]): Turn[] {
  const turns: Turn[] = [];
  let current: Turn | null = null;
  const open = (id: string, prompt?: TranscriptItem): Turn => {
    const turn: Turn = { id, prompt, work: [], answer: [], notes: [] };
    turns.push(turn);
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
      current.work.push(...current.answer, item);
      current.answer = [];
    } else {
      current.notes.push(item);
    }
  }
  return turns;
}

/** The first and last moment anything in the turn happened, as epoch ms. */
function span(turn: Turn): [number, number] | undefined {
  const rows = [turn.prompt, ...turn.work, ...turn.answer].filter(Boolean) as TranscriptItem[];
  const starts = rows.map((row) => Date.parse(row.createdAt ?? "")).filter(Number.isFinite);
  const ends = rows
    .map((row) => Date.parse(row.updatedAt ?? row.createdAt ?? ""))
    .filter(Number.isFinite);
  if (!starts.length || !ends.length) return undefined;
  return [Math.min(...starts), Math.max(...ends)];
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
