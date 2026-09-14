import { create } from "zustand";
import type { AgentImageAttachment, AgentSession, PendingPermission } from "../bridge/agenthub";
import type {
  Subagent,
  TranscriptModel,
  TranscriptPaging,
} from "../modules/agenthub/types";
import { useShallow } from "zustand/react/shallow";

export const EMPTY_TRANSCRIPT_MODEL: TranscriptModel = {
  items: [],
  usage: null,
  commands: [],
  mode: null,
  configOptions: [],
  toolCallIndex: new Map(),
};

export type AgentHubCommand = "create" | "cancel" | "focus";

export type QueuedAgentPrompt = {
  id: string;
  text: string;
  attachments: string[];
  images: AgentImageAttachment[];
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
  /** Where each session's page walk stands — read by the transcript's top
 * edge to say whether history is still above. */
  transcriptPagingById: Record<string, TranscriptPaging>;
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
  pendingAgentHubCommand: AgentHubCommand | null;
  unreadCompletedBySession: Record<string, boolean>;
  queuedPromptsBySession: Record<string, QueuedAgentPrompt[]>;
  queuedPromptInFlightBySession: Record<string, string | undefined>;
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string) => void;
  setPrompt: (sessionId: string, prompt: string) => void;
  setTranscript: (sessionId: string, model: TranscriptModel) => void;
  setTranscriptLoading: (sessionId: string, loading: boolean) => void;
  setTranscriptError: (sessionId: string, error: Error | null) => void;
  setTranscriptPaging: (sessionId: string, paging: TranscriptPaging) => void;
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
  requestAgentHubCommand: (command: AgentHubCommand) => void;
  consumeAgentHubCommand: (command: AgentHubCommand) => void;
  markCompletedUnread: (sessionId: string) => void;
  markCompletedRead: (sessionId: string) => void;
  enqueuePrompt: (sessionId: string, prompt: QueuedAgentPrompt) => void;
  removeQueuedPrompt: (sessionId: string, promptId: string) => void;
  updateQueuedPrompt: (sessionId: string, prompt: QueuedAgentPrompt) => void;
  claimQueuedPrompt: (sessionId: string) => QueuedAgentPrompt | null;
  releaseQueuedPrompt: (sessionId: string, promptId: string) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set) => ({
  sessionsById: {},
  sessionIds: [],
  activeSessionId: "",
  transcriptsById: {},
  transcriptLoadingById: {},
  transcriptErrorsById: {},
  transcriptPagingById: {},
  promptBySession: {},
  subagents: {},
  pending: null,
  pendingQueue: [],
  chatOpen: false,
  pendingAgentHubCommand: null,
  unreadCompletedBySession: {},
  queuedPromptsBySession: {},
  queuedPromptInFlightBySession: {},
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
      const { [id]: _paging, ...transcriptPagingById } = state.transcriptPagingById;
      const { [id]: _prompt, ...promptBySession } = state.promptBySession;
      const { [id]: _unread, ...unreadCompletedBySession } = state.unreadCompletedBySession;
      const { [id]: _queued, ...queuedPromptsBySession } = state.queuedPromptsBySession;
      const { [id]: _inFlight, ...queuedPromptInFlightBySession } = state.queuedPromptInFlightBySession;
      return {
        sessionIds: state.sessionIds.filter((sessionId) => sessionId !== id),
        sessionsById,
        transcriptsById,
        transcriptLoadingById,
        transcriptErrorsById,
        transcriptPagingById,
        promptBySession,
        unreadCompletedBySession,
        queuedPromptsBySession,
        queuedPromptInFlightBySession,
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
  setTranscriptPaging: (sessionId, paging) =>
    set((state) => ({
      transcriptPagingById: { ...state.transcriptPagingById, [sessionId]: paging },
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
  requestAgentHubCommand: (command) => set({ pendingAgentHubCommand: command }),
  consumeAgentHubCommand: (command) =>
    set((state) =>
      state.pendingAgentHubCommand === command ? { pendingAgentHubCommand: null } : state,
    ),
  markCompletedUnread: (sessionId) =>
    set((state) =>
      state.unreadCompletedBySession[sessionId]
        ? state
        : { unreadCompletedBySession: { ...state.unreadCompletedBySession, [sessionId]: true } },
    ),
  markCompletedRead: (sessionId) =>
    set((state) => {
      if (!state.unreadCompletedBySession[sessionId]) return state;
      const { [sessionId]: _read, ...unreadCompletedBySession } = state.unreadCompletedBySession;
      return { unreadCompletedBySession };
    }),
  enqueuePrompt: (sessionId, prompt) =>
    set((state) => ({
      queuedPromptsBySession: {
        ...state.queuedPromptsBySession,
        [sessionId]: [...(state.queuedPromptsBySession[sessionId] ?? []), prompt],
      },
    })),
  removeQueuedPrompt: (sessionId, promptId) =>
    set((state) => {
      const inFlight = state.queuedPromptInFlightBySession[sessionId];
      return {
        queuedPromptsBySession: {
          ...state.queuedPromptsBySession,
          [sessionId]: (state.queuedPromptsBySession[sessionId] ?? []).filter((prompt) => prompt.id !== promptId),
        },
        queuedPromptInFlightBySession:
          inFlight === promptId
            ? { ...state.queuedPromptInFlightBySession, [sessionId]: undefined }
            : state.queuedPromptInFlightBySession,
      };
    }),
  updateQueuedPrompt: (sessionId, prompt) =>
    set((state) => ({
      queuedPromptsBySession: {
        ...state.queuedPromptsBySession,
        [sessionId]: (state.queuedPromptsBySession[sessionId] ?? []).map((current) =>
          current.id === prompt.id ? prompt : current,
        ),
      },
    })),
  claimQueuedPrompt: (sessionId) => {
    let claimed: QueuedAgentPrompt | null = null;
    set((state) => {
      if (state.queuedPromptInFlightBySession[sessionId]) return state;
      claimed = state.queuedPromptsBySession[sessionId]?.[0] ?? null;
      if (!claimed) return state;
      return {
        queuedPromptInFlightBySession: {
          ...state.queuedPromptInFlightBySession,
          [sessionId]: claimed.id,
        },
      };
    });
    return claimed;
  },
  releaseQueuedPrompt: (sessionId, promptId) =>
    set((state) =>
      state.queuedPromptInFlightBySession[sessionId] === promptId
        ? {
            queuedPromptInFlightBySession: {
              ...state.queuedPromptInFlightBySession,
              [sessionId]: undefined,
            },
          }
        : state,
    ),
}));

export function useAgentSessionList(): AgentSession[] {
  return useAgentSessionsStore(
    useShallow((state) => state.sessionIds.map((id) => state.sessionsById[id])),
  );
}
