import * as React from "react";

import type { ToolCall, TranscriptItem } from "../types";

/**
 * A tool call that is still "pending" or "in_progress" in the stored transcript
 * is only live while something can still finish it. A call an interrupted turn
 * left behind stays that way for ever, and drawing it with a spinner says the
 * agent is working when it is not.
 *
 * The rule is per turn, not per session: a call of an EARLIER turn (a later
 * user message exists) can no longer finish, whatever the session is doing now,
 * even a newer turn. A call of the LATEST turn keeps drawing as its status
 * says. The stored session status cannot settle it: a turn the vendor starts
 * on its own, or a background subagent, produces updates with no turn event
 * and leaves the status idle, so judging by status would draw a call that is
 * really in flight as "didn't finish". The one case this leaves spinning is the
 * person cancelling the latest turn while a call stays in_progress (behaviour
 * as it was); the roadmap's U1 (session Activity) will say what the session is
 * really doing and let the latest turn settle safely.
 */

/** The states a tool call draws (AgentToolCall); `unfinished` is one that never got to end. */
export type SettledToolState = "pending" | "running" | "awaiting" | "done" | "failed" | "unfinished";

export function settleToolState<S extends SettledToolState>(state: S, facts: { earlierTurn: boolean }): S | "unfinished" {
  if (!facts.earlierTurn) return state;
  return state === "pending" || state === "running" || state === "awaiting" ? "unfinished" : state;
}

/** Whether the call being drawn belongs to a turn that is not the newest. Outside a turn (a panel, a fixture): no. */
const EarlierTurnContext = React.createContext(false);

/** What AgentTurn gives everything it draws: the turn it is, relative to the newest. */
export function EarlierTurnProvider({ earlier, children }: { earlier: boolean; children: React.ReactNode }) {
  return <EarlierTurnContext.Provider value={earlier}>{children}</EarlierTurnContext.Provider>;
}

export function useInEarlierTurn(): boolean {
  return React.useContext(EarlierTurnContext);
}

/**
 * The ids of the items that precede the newest user message: the earlier turns'.
 * For a view that draws items outside a turn (the follow panel) and has to say
 * which of them are old.
 */
export function earlierTurnItemIds(items: readonly TranscriptItem[]): ReadonlySet<string> {
  let last = -1;
  for (let i = items.length - 1; i >= 0; i -= 1) {
    if (String(items[i].kind) === "user_message_chunk") {
      last = i;
      break;
    }
  }
  return new Set(items.slice(0, Math.max(last, 0)).map((item) => item.id));
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
