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
  availableModes: [],
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

/** The statuses Go's Invoke refuses a prompt in. */
const CANNOT_TAKE_PROMPT = new Set(["running", "awaiting_permission", "closed"]);

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
  /** Prompts waiting for their turn. One whose turn has started is not here. */
  queuedPromptsBySession: Record<string, QueuedAgentPrompt[]>;
  /**
   * A turn this window sent is between invoke and its answer. sendAgentTurn
   * owns the session's status until it returns, whichever order Go's
   * turnFinished event and the RPC's reply arrive in.
   */
  turnInFlightBySession: Record<string, boolean>;
  /** A turn failed or was cancelled with prompts still waiting; they wait for Resume. */
  queuePausedBySession: Record<string, boolean>;
  /** Why the queue paused, when the refusal said: a refused queued prompt is refused again on each Resume until fixed. */
  queuePauseReasonBySession: Record<string, string>;
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
  /** Marks a turn in flight; false when one already is. */
  beginTurn: (sessionId: string) => boolean;
  /**
   * Takes the next waiting prompt out of the queue and marks its turn in
   * flight — only when the session exists, can take a prompt, has no
   * permission request waiting, and its queue is not paused.
   */
  claimQueuedPrompt: (sessionId: string) => QueuedAgentPrompt | null;
  /** Puts back, at the head, a prompt the backend refused before its turn began. */
  releaseQueuedPrompt: (sessionId: string, prompt: QueuedAgentPrompt) => void;
  finishTurn: (sessionId: string) => void;
  /** Pauses the session's queue if anything is waiting in it. */
  pauseQueue: (sessionId: string, reason?: string) => void;
  resumeQueue: (sessionId: string) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set, get) => ({
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
  turnInFlightBySession: {},
  queuePausedBySession: {},
  queuePauseReasonBySession: {},
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
      const { [id]: _inFlight, ...turnInFlightBySession } = state.turnInFlightBySession;
      const { [id]: _paused, ...queuePausedBySession } = state.queuePausedBySession;
      const { [id]: _reason, ...queuePauseReasonBySession } = state.queuePauseReasonBySession;
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
        turnInFlightBySession,
        queuePausedBySession,
        queuePauseReasonBySession,
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
      const queue = (state.queuedPromptsBySession[sessionId] ?? []).filter((prompt) => prompt.id !== promptId);
      return {
        queuedPromptsBySession: { ...state.queuedPromptsBySession, [sessionId]: queue },
        // Nothing left to resume.
        queuePausedBySession:
          queue.length === 0
            ? { ...state.queuePausedBySession, [sessionId]: false }
            : state.queuePausedBySession,
        queuePauseReasonBySession:
          queue.length === 0
            ? { ...state.queuePauseReasonBySession, [sessionId]: "" }
            : state.queuePauseReasonBySession,
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
  beginTurn: (sessionId) => {
    if (get().turnInFlightBySession[sessionId]) return false;
    set((state) => ({ turnInFlightBySession: { ...state.turnInFlightBySession, [sessionId]: true } }));
    return true;
  },
  claimQueuedPrompt: (sessionId) => {
    const state = get();
    const [next, ...rest] = state.queuedPromptsBySession[sessionId] ?? [];
    const session = state.sessionsById[sessionId];
    if (
      !next ||
      !session ||
      CANNOT_TAKE_PROMPT.has(session.status) ||
      state.turnInFlightBySession[sessionId] ||
      state.queuePausedBySession[sessionId] ||
      // The agent is waiting on a person; a queued prompt must not jump that.
      state.pendingQueue.some((pending) => pending.sessionId === sessionId)
    ) {
      return null;
    }
    set({
      queuedPromptsBySession: { ...state.queuedPromptsBySession, [sessionId]: rest },
      turnInFlightBySession: { ...state.turnInFlightBySession, [sessionId]: true },
    });
    return next;
  },
  releaseQueuedPrompt: (sessionId, prompt) =>
    set((state) =>
      state.sessionsById[sessionId]
        ? {
            queuedPromptsBySession: {
              ...state.queuedPromptsBySession,
              [sessionId]: [prompt, ...(state.queuedPromptsBySession[sessionId] ?? [])],
            },
          }
        : state,
    ),
  finishTurn: (sessionId) =>
    set((state) => ({ turnInFlightBySession: { ...state.turnInFlightBySession, [sessionId]: false } })),
  pauseQueue: (sessionId, reason = "") =>
    set((state) =>
      state.queuedPromptsBySession[sessionId]?.length
        ? {
            queuePausedBySession: { ...state.queuePausedBySession, [sessionId]: true },
            queuePauseReasonBySession: { ...state.queuePauseReasonBySession, [sessionId]: reason },
          }
        : state,
    ),
  resumeQueue: (sessionId) =>
    set((state) => ({
      queuePausedBySession: { ...state.queuePausedBySession, [sessionId]: false },
      queuePauseReasonBySession: { ...state.queuePauseReasonBySession, [sessionId]: "" },
    })),
}));

export function useAgentSessionList(): AgentSession[] {
  return useAgentSessionsStore(
    useShallow((state) => state.sessionIds.map((id) => state.sessionsById[id])),
  );
}
