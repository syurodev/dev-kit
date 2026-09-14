import { create } from "zustand";
import type { AgentSession, PendingPermission } from "../bridge/agenthub";
import type { Subagent, TranscriptModel } from "../modules/agenthub/types";
import { useShallow } from "zustand/react/shallow";

export const EMPTY_TRANSCRIPT_MODEL: TranscriptModel = {
  items: [],
  usage: null,
  commands: [],
  mode: null,
  toolCallIndex: new Map(),
};

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
  sessionsById: Record<string, AgentSession>;
  sessionIds: string[];
  activeSessionId: string;
  /**
   * The conversations themselves. They live here, not in AgentHubView state,
   * because leaving the module would unmount the subtree — throwing away the
   * transcript and, worse, dropping the event subscription, so updates arriving
   * while you were elsewhere were never collected at all.
   */
  transcriptsById: Record<string, TranscriptModel>;
  transcriptLoadingById: Record<string, boolean>;
  transcriptErrorsById: Record<string, Error | null>;
  promptBySession: Record<string, string>;
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
  setTranscript: (sessionId: string, model: TranscriptModel) => void;
  setTranscriptLoading: (sessionId: string, loading: boolean) => void;
  setTranscriptError: (sessionId: string, error: Error | null) => void;
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
  sessionsById: {},
  sessionIds: [],
  activeSessionId: "",
  transcriptsById: {},
  transcriptLoadingById: {},
  transcriptErrorsById: {},
  promptBySession: {},
  subagents: {},
  pending: null,
  pendingQueue: [],
  chatOpen: false,
  setSessions: (sessions) =>
    set({
      sessionsById: Object.fromEntries(sessions.map((session) => [session.id, session])),
      sessionIds: sessions.map((session) => session.id),
    }),
  upsertSession: (session) =>
    set((state) => {
      if (!state.sessionsById[session.id]) {
        return {
          sessionsById: { ...state.sessionsById, [session.id]: session },
          sessionIds: [...state.sessionIds, session.id],
        };
      }
      return { sessionsById: { ...state.sessionsById, [session.id]: session } };
    }),
  removeSession: (id) =>
    set((state) => {
      // The session's subagents go with it. Nothing reads them once the session
      // is gone, so leaving them behind is a map that only ever grows.
      const { [id]: _gone, ...subagents } = state.subagents;
      const { [id]: _session, ...sessionsById } = state.sessionsById;
      const { [id]: _transcript, ...transcriptsById } = state.transcriptsById;
      const { [id]: _loading, ...transcriptLoadingById } = state.transcriptLoadingById;
      const { [id]: _transcriptError, ...transcriptErrorsById } = state.transcriptErrorsById;
      const { [id]: _prompt, ...promptBySession } = state.promptBySession;
      return {
        sessionIds: state.sessionIds.filter((sessionId) => sessionId !== id),
        sessionsById,
        transcriptsById,
        transcriptLoadingById,
        transcriptErrorsById,
        promptBySession,
        subagents,
      };
    }),
  setActiveSessionId: (id) => set({ activeSessionId: id }),
  setPrompt: (sessionId, prompt) =>
    set((state) => ({ promptBySession: { ...state.promptBySession, [sessionId]: prompt } })),
  setTranscript: (sessionId, model) =>
    set((state) => ({ transcriptsById: { ...state.transcriptsById, [sessionId]: model } })),
  setTranscriptLoading: (sessionId, loading) =>
    set((state) => ({
      transcriptLoadingById: { ...state.transcriptLoadingById, [sessionId]: loading },
    })),
  setTranscriptError: (sessionId, error) =>
    set((state) => ({
      transcriptErrorsById: { ...state.transcriptErrorsById, [sessionId]: error },
    })),
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

export function useAgentSessionList(): AgentSession[] {
  return useAgentSessionsStore(
    useShallow((state) => state.sessionIds.map((id) => state.sessionsById[id])),
  );
}
