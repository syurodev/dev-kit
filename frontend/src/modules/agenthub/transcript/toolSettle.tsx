import * as React from "react";

import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { Subagent, ToolCall, TranscriptItem } from "../types";

/**
 * A tool call that is still "pending" or "in_progress" in the stored transcript
 * is only live while something can still finish it. A call an interrupted turn
 * left behind stays that way for ever, and drawing it with a spinner says the
 * agent is working when it is not.
 *
 * Two rules settle it to "didn't finish":
 *
 * - A call of an EARLIER turn (a later user message exists) can no longer finish,
 *   whatever the session is doing now, even a newer turn.
 * - A call of the LATEST turn can no longer finish when the session is quiet
 *   (isSessionQuiet): nothing works on it (neither a turn of ours nor one the agent
 *   started itself, session.activity), no turn of ours is in flight and no permission
 *   waits. That is the cancelled-turn case. The stored status alone could not say it:
 *   a turn the vendor starts on its own leaves the status idle and was
 *   indistinguishable from a cancelled one, which is what session activity now tells
 *   apart.
 *
 * A call with a subagent `origin` may still finish in a quiet session, but only while
 * that subagent is tracked as running (the store's `subagents`, fed by every spawn and
 * state update, ended ones dropped): a background subagent can keep working after the
 * lead's turn ended, and the session has no signal for it. A subagent that is not
 * tracked as running, in a quiet session, left its call unfinished for good.
 */

/** The states a tool call draws (AgentToolCall); `unfinished` is one that never got to end. */
export type SettledToolState = "pending" | "running" | "awaiting" | "done" | "failed" | "unfinished";

/** What decides whether a call can still finish. */
export type SettleFacts = {
  /** A later user message exists. */
  earlierTurn: boolean;
  /** The session is quiet (isSessionQuiet). Absent outside a session: not quiet. */
  sessionQuiet?: boolean;
  /** The call belongs to a subagent (its row has an `origin`). */
  subagent?: boolean;
  /** That subagent is tracked as running (subagentTrackedRunning). Absent: not tracked. */
  subagentRunning?: boolean;
};

/** Whether a call that has not ended can never end. */
export function cannotFinish(facts: SettleFacts): boolean {
  return facts.earlierTurn || (Boolean(facts.sessionQuiet) && !(facts.subagent && facts.subagentRunning));
}

export function settleToolState<S extends SettledToolState>(state: S, facts: SettleFacts): S | "unfinished" {
  if (!cannotFinish(facts)) return state;
  return state === "pending" || state === "running" || state === "awaiting" ? "unfinished" : state;
}

type SettleScope = { earlier: boolean; quiet: boolean; sessionId?: string };

/** Outside a turn (a panel, a fixture): neither an earlier turn nor a quiet session. */
const SettleContext = React.createContext<SettleScope>({ earlier: false, quiet: false });

/**
 * What AgentTurn gives everything it draws: the turn it is, relative to the newest, and
 * whether the session is quiet (only the newest turn can then settle by it).
 */
export function EarlierTurnProvider({
  earlier,
  quiet = false,
  sessionId,
  children,
}: {
  earlier: boolean;
  quiet?: boolean;
  /** The session drawn: where a subagent's call looks up whether its subagent still runs. */
  sessionId?: string;
  children: React.ReactNode;
}) {
  const value = React.useMemo(() => ({ earlier, quiet, sessionId }), [earlier, quiet, sessionId]);
  return <SettleContext.Provider value={value}>{children}</SettleContext.Provider>;
}

/** The session the scope is for; a row's own `sessionId` (loaded pages) wins over it (live rows carry none). */
export function useSettleSessionId(): string | undefined {
  return React.useContext(SettleContext).sessionId;
}

export function useInEarlierTurn(): boolean {
  return React.useContext(SettleContext).earlier;
}

/**
 * Whether the subagent an `origin` names is tracked as running in a session's
 * list. Same rule as subagents.tsx isSubagentRunning (no state yet is running),
 * kept here because that module imports the transcript surfaces.
 */
export function subagentTrackedRunning(tracked: readonly Subagent[] | undefined, origin?: string): boolean {
  if (!origin) return false;
  const found = tracked?.find((subagent) => subagent.id === origin);
  return Boolean(found) && (!found!.state || found!.state === "running");
}

/**
 * The facts for one call: the scope it is drawn in, whether it is a subagent's
 * (`origin`) and, with the session it lives in, whether that subagent is still
 * tracked as running.
 */
export function useSettleFacts(origin?: string, sessionId?: string): SettleFacts {
  const { earlier, quiet, sessionId: scopeSession } = React.useContext(SettleContext);
  const session = sessionId ?? scopeSession;
  const subagentRunning = useAgentSessionsStore((s) =>
    origin && session ? subagentTrackedRunning(s.subagents[session], origin) : false,
  );
  return { earlierTurn: earlier, sessionQuiet: quiet, subagent: Boolean(origin), subagentRunning };
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
