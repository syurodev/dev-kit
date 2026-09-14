import { create } from "zustand";
import { extractUpdateText, type AgentSession, type PendingPermission } from "../bridge/agenthub";
import type { Subagent } from "../modules/agenthub/types";

export type TranscriptLine = {
  id: string;
  role: "user" | "agent" | "system";
  text: string;
};

export type TranscriptState = {
  lines: TranscriptLine[];
  prompt: string;
};

export const EMPTY_TRANSCRIPT: TranscriptState = { lines: [], prompt: "" };

/** Append streaming chunk text for a session update payload. */
export function appendUpdateLine(
  lines: TranscriptLine[],
  update: unknown,
): TranscriptLine[] {
  const text = extractUpdateText(update).trim();
  if (!text) {
    return lines;
  }
  const last = lines[lines.length - 1];
  if (last?.role === "agent") {
    return [
      ...lines.slice(0, -1),
      { ...last, text: last.text === text ? last.text : `${last.text}${text}` },
    ];
  }
  return [
    ...lines,
    {
      id: `agent-${Date.now()}-${lines.length}`,
      role: "agent",
      text,
    },
  ];
}

/**
 * The subagent end states that mean "stopped", not "finished".
 *
 * A subagent that was killed leaves the tree: the rail is what is going on
 * right now, and a row for an agent nobody can reach is a row that only invites
 * a click that does nothing. The ones that ran to an end stay, with the dot
 * saying how they ended — that is a result, and results are worth a glance.
 *
 * Whatever it said before it was killed is still in the session's transcript,
 * which is where a record belongs.
 */
const KILLED_STATES = new Set(["cancelled", "disconnected"]);

type AgentSessionsState = {
  sessions: AgentSession[];
  activeSessionId: string;
  /**
   * The conversations themselves. They live here, not in AgentHubView state,
   * because leaving the module would unmount the subtree — throwing away the
   * transcript and, worse, dropping the event subscription, so updates arriving
   * while you were elsewhere were never collected at all.
   */
  transcripts: Record<string, TranscriptState>;
  /**
   * The agents Claude started inside its own process, by the session that owns
   * them. Kept here rather than read back out of the transcript because the
   * rail shows every session's subagents and a transcript is only loaded for
   * the one that is open.
   *
   * Live updates only: a subagent that ran before this app started is in that
   * session's transcript, not here. They are turn-scoped things, and a finished
   * one from last week is not what the rail is for.
   */
  subagents: Record<string, Subagent[]>;
  pending: PendingPermission | null;
  pendingQueue: PendingPermission[];
  /** Whether the shell is showing the conversation in the companion slot. */
  chatOpen: boolean;
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string) => void;
  setPrompt: (sessionId: string, prompt: string) => void;
  updateLines: (
    sessionId: string,
    updater: (prev: TranscriptLine[]) => TranscriptLine[],
  ) => void;
  /**
   * Records a subagent_spawned or subagent_state_update. Neither message is
   * complete — the spawn names it, the state update only says how it ended —
   * so they are merged by id rather than replacing one another.
   *
   * A subagent that was stopped rather than finished is removed instead of
   * merged (see KILLED_STATES).
   */
  trackSubagent: (sessionId: string, subagent: Subagent) => void;
  enqueuePending: (pending: PendingPermission) => void;
  dequeuePending: (id: string) => void;
  setChatOpen: (open: boolean) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set) => ({
  sessions: [],
  activeSessionId: "",
  transcripts: {},
  subagents: {},
  pending: null,
  pendingQueue: [],
  chatOpen: false,
  setSessions: (sessions) => set({ sessions }),
  upsertSession: (session) =>
    set((state) => {
      const index = state.sessions.findIndex((s) => s.id === session.id);
      if (index === -1) {
        return { sessions: [...state.sessions, session] };
      }
      const next = state.sessions.slice();
      next[index] = session;
      return { sessions: next };
    }),
  removeSession: (id) =>
    set((state) => {
      // The session's subagents go with it. Nothing reads them once the session
      // is gone, so leaving them behind is a map that only ever grows.
      const { [id]: _gone, ...subagents } = state.subagents;
      return { sessions: state.sessions.filter((s) => s.id !== id), subagents };
    }),
  setActiveSessionId: (id) => set({ activeSessionId: id }),
  setPrompt: (sessionId, prompt) =>
    set((state) => ({
      transcripts: {
        ...state.transcripts,
        [sessionId]: { ...(state.transcripts[sessionId] ?? EMPTY_TRANSCRIPT), prompt },
      },
    })),
  updateLines: (sessionId, updater) =>
    set((state) => {
      const current = state.transcripts[sessionId] ?? EMPTY_TRANSCRIPT;
      return {
        transcripts: {
          ...state.transcripts,
          [sessionId]: { ...current, lines: updater(current.lines) },
        },
      };
    }),
  trackSubagent: (sessionId, subagent) =>
    set((state) => {
      const current = state.subagents[sessionId] ?? [];
      let next: Subagent[];
      if (KILLED_STATES.has(subagent.state ?? "")) {
        next = current.filter((s) => s.id !== subagent.id);
      } else {
        const index = current.findIndex((s) => s.id === subagent.id);
        next = current.slice();
        if (index === -1) next.push(subagent);
        else next[index] = { ...current[index], ...subagent };
      }
      return { subagents: { ...state.subagents, [sessionId]: next } };
    }),
  enqueuePending: (pending) =>
    set((state) => {
      if (state.pendingQueue.some((p) => p.id === pending.id)) {
        return {};
      }
      const pendingQueue = [...state.pendingQueue, pending];
      return { pendingQueue, pending: state.pending ?? pending };
    }),
  dequeuePending: (id) =>
    set((state) => {
      const pendingQueue = state.pendingQueue.filter((p) => p.id !== id);
      return { pendingQueue, pending: pendingQueue[0] ?? null };
    }),
  setChatOpen: (open) => set({ chatOpen: open }),
}));
