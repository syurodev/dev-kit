import type { TranscriptItem } from "./types";
import { groupTurns, type Turn } from "./turns";

export type TranscriptExportOptions = {
  /** The session's title, for the document heading. */
  title?: string | null;
  /** The working directory, under the heading. */
  projectPath?: string;
  /** The agent, under the heading. */
  vendor?: string;
};

/**
 * A transcript as a Markdown document: one section per turn, the prompt as a
 * quote, the work as a list, the answer as the prose it already is.
 *
 * Deliberately lossy. A tool call's full input and output are on disk and
 * reachable from the app; a document that inlined forty of them would be a
 * log, and a log is not what anyone pastes into a ticket, a PR or a note. What
 * is kept is what a reader needs to follow the thread: what was asked, what
 * the agent did (by title), and what it said.
 *
 * Reads the render model, not the wire rows, so it agrees with the screen —
 * the same grouping into turns, the same coalesced messages, the same one row
 * per tool call and per background task.
 */
export function transcriptToMarkdown(items: TranscriptItem[], options: TranscriptExportOptions = {}): string {
  const out: string[] = [];
  const title = options.title?.trim() || "Agent session";
  out.push(`# ${title}`);
  const meta = [options.vendor, options.projectPath].filter(Boolean);
  if (meta.length > 0) out.push("", meta.map((part) => `\`${part}\``).join(" · "));

  const turns = groupTurns(items);
  turns.forEach((turn, index) => {
    out.push("", "---", "");
    out.push(...turnToMarkdown(turn, index + 1));
  });
  return out.join("\n").trimEnd() + "\n";
}

function turnToMarkdown(turn: Turn, number: number): string[] {
  const out: string[] = [];
  const prompt = turn.prompt?.text?.trim();
  out.push(`## Turn ${number}`);
  if (prompt) {
    out.push("", "**You:**", "", ...quote(prompt));
  }

  const steps = turn.work.map(stepLine).filter((line): line is string => Boolean(line));
  const said = turn.work
    .filter((item) => String(item.kind) === "agent_message_chunk" && item.text?.trim())
    .map((item) => item.text!.trim());
  if (steps.length > 0 || said.length > 0) {
    out.push("", "**Work:**", "");
    for (const line of steps) out.push(`- ${line}`);
    for (const text of said) out.push("", text);
  }

  // What each subagent came back with, under its own name: its rows are kept
  // out of the turn's flow (turns.ts), so without this a report would be lost
  // from the export rather than attributed to the lead agent as it once was.
  for (const item of turn.work) {
    const id = item.subagent?.id;
    const report = id ? subagentReport(turn.subagents[id] ?? []) : "";
    if (report) out.push("", `**${item.subagent?.name || "Subagent"} reported:**`, "", ...quote(report));
  }

  const answer = turn.answer
    .map((item) => item.text?.trim() ?? "")
    .filter(Boolean)
    .join("\n\n");
  if (answer) {
    out.push("", "**Agent:**", "", answer);
  }
  return out;
}

/** One line for a step of work; nothing for a step that is only speech. */
export function stepLine(item: TranscriptItem): string | undefined {
  switch (String(item.kind)) {
    case "agent_message_chunk":
      return undefined; // rendered as prose, below the list
    case "agent_thought_chunk":
      return "Thought";
    case "plan": {
      const entries = item.plan ?? [];
      if (entries.length === 0) return "Made a plan";
      return `Plan: ${entries.map((entry) => `${entry.status === "completed" ? "[x]" : "[ ]"} ${entry.content}`).join("; ")}`;
    }
    case "subagent_spawned":
    case "subagent_state_update": {
      const agent = item.subagent;
      const name = agent?.name || "Subagent";
      const task = agent?.task ? ` — ${agent.task}` : "";
      return `${name}${task} (${agent?.state || "running"})`;
    }
    case "async_task_spawned":
    case "async_task_progress":
    case "async_task_state_update": {
      const task = item.asyncTask;
      const name = task?.name || task?.description || "Background task";
      const tail = task?.summary ? ` — ${task.summary}` : "";
      return `${name}${tail} (${task?.state || "running"})`;
    }
    default: {
      const call = item.toolCall;
      const title = call?.title?.trim() || "Tool call";
      const status = call?.status && call.status !== "completed" ? ` (${call.status})` : "";
      return `${inlineCode(title)}${status}`;
    }
  }
}

/** What a subagent said after its last step — the report it ended on. */
export function subagentReport(activity: TranscriptItem[]): string {
  const said: string[] = [];
  for (let index = activity.length - 1; index >= 0; index -= 1) {
    const row = activity[index];
    if (String(row.kind) !== "agent_message_chunk") break;
    if (row.text?.trim()) said.unshift(row.text.trim());
  }
  return said.join("\n\n");
}

function quote(text: string): string[] {
  return text.split("\n").map((line) => `> ${line}`);
}

/** Backtick-wrapped, with a longer fence when the text has backticks itself. */
function inlineCode(text: string): string {
  const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((run) => run.length));
  const fence = "`".repeat(longest + 1);
  const pad = text.startsWith("`") || text.endsWith("`") ? " " : "";
  return `${fence}${pad}${text}${pad}${fence}`;
}
