import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { ToolCall } from "../types";

/**
 * A tool call that is still "pending" or "in_progress" in the stored transcript
 * is only live while something can still finish it. A call an interrupted turn
 * left behind stays that way for ever in a session that is idle, and drawing
 * it with a spinner says the agent is working when it is not.
 */

/** The states a tool call draws (AgentToolCall); `unfinished` is one that never got to end. */
export type SettledToolState = "pending" | "running" | "awaiting" | "done" | "failed" | "unfinished";

export function settleToolState<S extends SettledToolState>(state: S, facts: { sessionRunning: boolean }): S | "unfinished" {
  if (facts.sessionRunning) return state;
  return state === "pending" || state === "running" || state === "awaiting" ? "unfinished" : state;
}

/**
 * Whether a call of this session can still be answered: the session is running
 * or waiting on a permission, or a turn this window sent is in flight (it
 * sets the status itself only after the call). An unknown status gets the
 * benefit of the doubt — a row not loaded yet is not proof the call is over.
 */
export function callCanStillFinish(status: string | undefined, turnInFlight: boolean): boolean {
  if (turnInFlight) return true;
  return !(status === "idle" || status === "error" || status === "closed");
}

/** callCanStillFinish for a session of the store; a call outside any session (a fixture) can. */
export function useToolCallCanFinish(sessionId: string | undefined): boolean {
  return useAgentSessionsStore((state) => {
    if (!sessionId) return true;
    const session = state.sessionsById[sessionId];
    return session ? callCanStillFinish(session.status, Boolean(state.turnInFlightBySession[sessionId])) : true;
  });
}

const ANSI = /\u001b\[[0-9;]*[A-Za-z]/g;

function firstTextLine(text: string, max: number): string {
  for (const raw of text.replace(ANSI, "").split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    return line.length > max ? `${line.slice(0, max - 1)}…` : line;
  }
  return "";
}

/** A JSON document is the tool's payload, not a sentence about what went wrong. */
function isDocument(text: string): boolean {
  const start = text.trimStart();
  return start.startsWith("{") || start.startsWith("[");
}

/**
 * The first non-empty line of what a failed call said, plain text, for the
 * collapsed row: rawOutput as text; an object's `error` or `message` when that
 * is a string; else the text content a vendor sent in its place (Claude's
 * failed Bash). "" when there is nothing sentence-like to show.
 */
export function failureLine(call: Pick<ToolCall, "rawOutput" | "content">, max = 120): string {
  const raw = call.rawOutput;
  if (typeof raw === "string") {
    if (!isDocument(raw)) {
      const line = firstTextLine(raw, max);
      if (line) return line;
    }
  } else if (raw && typeof raw === "object") {
    const record = raw as Record<string, unknown>;
    for (const key of ["error", "message"]) {
      const value = record[key];
      if (typeof value === "string") {
        const line = firstTextLine(value, max);
        if (line) return line;
      }
    }
  }
  for (const item of call.content ?? []) {
    const text = item.type === "content" ? item.content?.text : undefined;
    if (typeof text === "string" && !isDocument(text)) {
      const line = firstTextLine(text, max);
      if (line) return line;
    }
  }
  return "";
}
