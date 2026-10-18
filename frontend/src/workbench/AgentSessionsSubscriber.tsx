import { useEffect, useRef, useState } from "react";
import { Window } from "@wailsio/runtime";
import {
  agentHubGetStatus,
  agentHubListAgents,
  agentHubPendingElicitations,
  agentHubPendingPermissions,
  agentHubSetPromptQueue,
  onAgentHubElicitation,
  onAgentHubElicitationClosed,
  onAgentHubPermission,
  onAgentHubSessionCreated,
  onAgentHubSessionUpdate,
  onAgentHubNotificationAction,
  onAgentHubSessionRewound,
  onAgentHubTurnFinished,
  type AgentSession,
  type AgentTurnFinishedEvent,
  type SessionUpdateEvent,
} from "../bridge/agenthub";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { reloadTranscript, startAgentTranscriptStream } from "../modules/agenthub/transcript";
import { agentSessionOnScreen, showAgentSession, syncAgentSessionTabTitles } from "../modules/agenthub/documents";
import { groupTurns, workedFor } from "../modules/agenthub/turns";
import type { TranscriptItem } from "../modules/agenthub/types";
import { UpdateKind } from "../../bindings/devkit/internal/agenthub/models";
import {
  drainQueuedPrompt,
  noteTurnFinishedDuringOurs,
  setAgentHubTabsStatus,
  type TurnBridge,
} from "./agentPromptQueue";

/** Someone is looking at this session: its tab in Agent Hub, or the Agent panel. */
function sessionVisible(sessionId: string): boolean {
  return agentSessionOnScreen(sessionId);
}

/** The live region's line for a status a turn passes through; its end is turnFinishedAnnouncement's. */
export function statusAnnouncement(status: string | undefined): string {
  if (status === "running") return "Running…";
  if (status === "awaiting_permission") return "Waiting for permission.";
  return "";
}

type ObservedStatus = { sessionId: string; status: string | undefined };

/**
 * statusAnnouncement for a change seen on one session. Not for the first status
 * seen — mounting, the session list arriving — nor for switching to a session:
 * those said "Running…" for a turn nobody had just started.
 */
export function statusTransitionAnnouncement(previous: ObservedStatus | null, next: ObservedStatus): string {
  if (!previous || previous.sessionId !== next.sessionId || previous.status === undefined) return "";
  return previous.status === next.status ? "" : statusAnnouncement(next.status);
}

/**
 * How a turn ended, from Go's outcome rather than the status: Go resets a
 * failed or cancelled turn to idle, and a turn that asked for permission ends
 * awaiting_permission → idle, so the transition said "Answer ready." for turns
 * that failed and nothing for ones that answered. Never token by token.
 */
export function turnFinishedAnnouncement(
  outcome: AgentTurnFinishedEvent["outcome"],
  items: TranscriptItem[],
): string {
  if (outcome === "cancelled") return "Stopped.";
  if (outcome === "error") return "The agent stopped with an error.";
  if (outcome !== "completed") return "";
  // Only the last turn is grouped: from the last prompt to the end.
  let start = items.length - 1;
  while (start > 0 && String(items[start].kind) !== "user_message_chunk") start -= 1;
  const turns = groupTurns(items.slice(Math.max(start, 0)));
  const worked = turns.length > 0 ? workedFor(turns[turns.length - 1]) : "";
  // "Worked" alone means the rows carry no times; there is nothing to add.
  return worked.startsWith("Worked for") ? `Answer ready. ${worked}` : "Answer ready.";
}

type QueueFacts = Pick<
  ReturnType<typeof useAgentSessionsStore.getState>,
  "queuedPromptsBySession" | "queuePausedBySession"
>;

/**
 * The sessions whose own prompt queue (how many wait, whether the person
 * paused it) differs between two states: what Go is told so that a message
 * from another session waits behind the person's prompts and never starts work
 * on a session they paused. Pure so the rule can be tested without a bridge.
 */
export function promptQueueChanges(
  next: QueueFacts,
  previous: QueueFacts,
): { sessionId: string; waiting: number; paused: boolean }[] {
  const ids = new Set([
    ...Object.keys(next.queuedPromptsBySession),
    ...Object.keys(next.queuePausedBySession),
    ...Object.keys(previous.queuedPromptsBySession),
    ...Object.keys(previous.queuePausedBySession),
  ]);
  const out: { sessionId: string; waiting: number; paused: boolean }[] = [];
  for (const sessionId of ids) {
    const now = { waiting: next.queuedPromptsBySession[sessionId]?.length ?? 0, paused: Boolean(next.queuePausedBySession[sessionId]) };
    const before = { waiting: previous.queuedPromptsBySession[sessionId]?.length ?? 0, paused: Boolean(previous.queuePausedBySession[sessionId]) };
    if (now.waiting !== before.waiting || now.paused !== before.paused) out.push({ sessionId, ...now });
  }
  return out;
}

/** A user message the person did not type: another session's, or the terminal's. */
export function isPeerMessage(event: Pick<SessionUpdateEvent, "parsed">): boolean {
  const parsed = event.parsed;
  return parsed?.kind === UpdateKind.UpdateUserMessageChunk && Boolean(parsed.source);
}

/** The latest preview read per session; an older read that answers late is ignored. */
const previewReads = new Map<string, number>();

/**
 * What a finished turn changes outside the turn itself: the session row, the
 * tab dot, the unread badge and the queue. Go emits the event from Invoke's
 * defer, so for a turn this window sent it can arrive before or after the
 * RPC's reply — sendAgentTurn settles that turn's status and drains the queue
 * itself, and this leaves both alone while it is in flight.
 */
/**
 * What an update from a turn says about the session's tabs: it is running.
 * Whatever the update — a tool call that failed is routine, the agent reads
 * the failure and goes on; whether the turn failed is for its end to say
 * (handleAgentTurnFinished).
 */
export function handleSessionUpdateTabs(event: Pick<SessionUpdateEvent, "sessionId">): void {
  setAgentHubTabsStatus(event.sessionId, "running");
}

export function handleAgentTurnFinished(
  { sessionId, status, outcome }: AgentTurnFinishedEvent,
  bridge?: TurnBridge,
) {
  const state = useAgentSessionsStore.getState();
  const session = state.sessionsById[sessionId];
  const ours = state.turnInFlightBySession[sessionId];
  const nextStatus = status === "error" ? "error" : status === "closed" ? "closed" : "idle";
  if (session && !ours && session.status !== nextStatus) {
    state.upsertSession({ ...session, status: nextStatus });
  }
  if (session && !ours) {
    // The preview is written with the status; a turn from elsewhere (MCP,
    // another agent) only told us the status, so read the row once. Only the
    // preview and its time are taken from it: the next queued turn may already be running
    // here by the time the read answers, and its status must not go back.
    const getStatus = bridge?.getStatus ?? agentHubGetStatus;
    const read = (previewReads.get(sessionId) ?? 0) + 1;
    previewReads.set(sessionId, read);
    void getStatus(sessionId)
      .then((fresh) => {
        if (previewReads.get(sessionId) !== read) return;
        const current = useAgentSessionsStore.getState().sessionsById[sessionId];
        // A title only when the row has none: Go names a session after its
        // first turn (agenthub/session_title.go), and a rename made here since
        // is the person's.
        const title = current && !current.title?.trim() && fresh?.title ? fresh.title : current?.title;
        if (fresh && current && (current.preview !== fresh.preview || current.updatedAt !== fresh.updatedAt || title !== current.title)) {
          useAgentSessionsStore
            .getState()
            .upsertSession({ ...current, title, preview: fresh.preview, updatedAt: fresh.updatedAt });
        }
      })
      .catch(() => {});
  }

  // The turn is over: a completed one marks its tabs finished, which the
  // strip shows until the tab is looked at (workbench/tabs/seenSuccess.ts) —
  // the tab's own mark, beside the rail's unread badge below, which counts the
  // answer as read only once the session itself is on screen. Cancelled or
  // closed is just idle. Not while a turn of ours is in flight: a late event
  // would put the next queued turn's mark out, and sendAgentTurn settles the
  // mark of its own turn.
  if (!ours) {
    setAgentHubTabsStatus(sessionId, nextStatus === "error" ? "error" : outcome === "completed" ? "success" : "idle");
  }

  if (sessionVisible(sessionId)) state.markCompletedRead(sessionId);
  else state.markCompletedUnread(sessionId);

  if (ours) {
    noteTurnFinishedDuringOurs(sessionId);
    return;
  }
  // A turn this window did not send — another agent's invoke — ends the same way.
  if (outcome === "completed") void drainQueuedPrompt(sessionId, bridge);
  else if (outcome === "error" || outcome === "cancelled") state.pauseQueue(sessionId);
}

/**
 * A session another caller created (an agent's child) is not in the list this
 * window loaded at start. Read its row and add it, unless it is already there —
 * the rail then draws it under its parent, and a tab that was waiting on it
 * stops saying "Session not available".
 */
export async function adoptCreatedSession(
  sessionId: string,
  getStatus: (id: string) => Promise<AgentSession> = agentHubGetStatus,
): Promise<void> {
  if (useAgentSessionsStore.getState().sessionsById[sessionId]) return;
  try {
    const session = await getStatus(sessionId);
    // A turn event may have added it while the read was in flight.
    if (session && !useAgentSessionsStore.getState().sessionsById[sessionId]) {
      useAgentSessionsStore.getState().upsertSession(session);
    }
  } catch {
    // Deleted again, or the hub is unavailable: the list loads it next time.
  }
}

/**
 * Keeps the shared Agent Hub session store in sync regardless of which
 * module is mounted. Rendered once in Workbench.tsx, as a sibling of the
 * sidebar and content card — never remounted on module switch, unlike
 * AgentHubView which only exists while that module is active.
 */
export function AgentSessionsSubscriber() {
  const setSessions = useAgentSessionsStore((state) => state.setSessions);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const enqueuePending = useAgentSessionsStore((state) => state.enqueuePending);
  const enqueueElicitation = useAgentSessionsStore((state) => state.enqueueElicitation);
  const dequeueElicitation = useAgentSessionsStore((state) => state.dequeueElicitation);
  const trackSubagent = useAgentSessionsStore((state) => state.trackSubagent);

  // The one live region for Agent Hub. The hub and the companion each mount a
  // transcript, and a region in each read every status twice.
  const [announcement, setAnnouncement] = useState("");
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const activeStatus = useAgentSessionsStore((state) => state.sessionsById[state.activeSessionId]?.status);
  const observedStatus = useRef<ObservedStatus | null>(null);
  useEffect(() => {
    const next = { sessionId: activeSessionId, status: activeStatus };
    const line = statusTransitionAnnouncement(observedStatus.current, next);
    observedStatus.current = next;
    if (line && sessionVisible(activeSessionId)) setAnnouncement(line);
  }, [activeSessionId, activeStatus]);

  // A session's tabs carry its title: a rename, or the title the agent gives
  // it after the first turn, retitles them wherever they are.
  useEffect(
    () =>
      useAgentSessionsStore.subscribe((state, previous) => {
        if (
          state.queuedPromptsBySession === previous.queuedPromptsBySession &&
          state.queuePausedBySession === previous.queuePausedBySession
        ) {
          return;
        }
        for (const { sessionId, waiting, paused } of promptQueueChanges(state, previous)) {
          void agentHubSetPromptQueue(sessionId, waiting, paused).catch(() => {});
        }
      }),
    [],
  );

  useEffect(() => {
    syncAgentSessionTabTitles(useAgentSessionsStore.getState().sessionsById);
    return useAgentSessionsStore.subscribe((state, previous) => {
      if (state.sessionsById !== previous.sessionsById) syncAgentSessionTabTitles(state.sessionsById);
    });
  }, []);

  useEffect(() => {
    let mounted = true;
    agentHubListAgents()
      .then((sessions) => {
        if (mounted) {
          setSessions(sessions);
        }
      })
      .catch(() => {
        // No sessions yet, or the hub is unavailable — sidebar just shows
        // no Agent Hub badge, same as an empty session list. Loaded all the
        // same: a tab waiting on the list would otherwise wait for ever.
        if (mounted) useAgentSessionsStore.setState({ sessionsLoaded: true });
      });
    agentHubPendingPermissions()
      .then((list) => {
        if (mounted) {
          list.forEach((p) => enqueuePending(p));
        }
      })
      .catch(() => {
        // Hub unavailable — no prompts to restore.
      });
    agentHubPendingElicitations()
      .then((list) => {
        if (mounted) list.forEach((e) => enqueueElicitation(e));
      })
      .catch(() => {
        // Hub unavailable — no questions to restore.
      });
    return () => {
      mounted = false;
    };
  }, [setSessions, enqueuePending, enqueueElicitation]);

  /**
   * Session metadata and tab status synchronization.
   * The shell-owned transcript stream is started alongside this listener;
   * `agenthub:sessionUpdate` marks a tab running; the separate turn-finished
   * event below marks it finished (or idle, or failed) and records an unread result.
   *
   * Subscribed globally so turns continue streaming when working in another module.
   * Synchronizes tab status with ACP update stream ('running' during turns,
   * with terminal status finalized by `agenthub:turnFinished`).
   * Tasks will be the second consumer of setTabStatus.
   */
  useEffect(() => {
    startAgentTranscriptStream();
    return onAgentHubSessionUpdate((event) => {
      // The agent names the session itself, after the first exchange, from
      // what was actually said. Until this landed every row in the swarm tree
      // read "Session 1d5b9d50" — an id is not a name. The backend has already
      // persisted it by the time this update arrives; this is what renames the
      // row without waiting for a reload.
      if (event.parsed?.kind === UpdateKind.UpdateSessionInfo && event.parsed.title) {
        const { sessionsById, upsertSession } = useAgentSessionsStore.getState();
        const session = sessionsById[event.sessionId];
        if (session && !session.title) {
          upsertSession({ ...session, title: event.parsed.title });
        }
      }

      // The swarm rail shows every session's subagents, not just the open
      // one's, so they are collected here rather than from the transcript the
      // module builds for whichever session you are looking at.
      if (
        (event.parsed?.kind === UpdateKind.UpdateSubagentSpawned ||
          event.parsed?.kind === UpdateKind.UpdateSubagentState) &&
        event.parsed.subagent
      ) {
        trackSubagent(event.sessionId, event.parsed.subagent);
      }

      // Another session wrote to this one. The row says so until the person
      // looks; a session already in front of them has nothing to flag.
      if (isPeerMessage(event) && !sessionVisible(event.sessionId)) {
        useAgentSessionsStore.getState().notePeerMessage(event.sessionId);
      }

      handleSessionUpdateTabs(event);
    });
  }, [trackSubagent]);

  useEffect(() => onAgentHubSessionCreated((sessionId) => void adoptCreatedSession(sessionId)), []);

  useEffect(
    () =>
      onAgentHubTurnFinished((event) => {
        handleAgentTurnFinished(event);
        if (!sessionVisible(event.sessionId)) return;
        const items = useAgentSessionsStore.getState().transcriptsById[event.sessionId]?.items ?? [];
        const line = turnFinishedAnnouncement(event.outcome, items);
        if (line) setAnnouncement(line);
      }),
    [],
  );

  useEffect(() => {
    return onAgentHubNotificationAction((sessionId) => {
      void (async () => {
        const state = useAgentSessionsStore.getState();
        if (!state.sessionsById[sessionId]) {
          try {
            state.upsertSession(await agentHubGetStatus(sessionId));
          } catch {
            // The notification may outlive a deleted session; still raise the
            // window so the user can see the remaining Agent Hub state.
          }
        }
        showAgentSession(sessionId);
        await Window.Show().catch(() => {});
        await Window.Focus().catch(() => {});
      })();
    });
  }, []);

  useEffect(() => {
    return onAgentHubPermission(async (pending) => {
      enqueuePending(pending);
      const session = useAgentSessionsStore.getState().sessionsById[pending.sessionId];
      if (session) {
        upsertSession({ ...session, status: "awaiting_permission" });
        return;
      }
      // Session isn't in the store yet — the initial list load is likely
      // still in flight. Fetch it directly so the permission event isn't
      // silently dropped.
      try {
        const fetched = await agentHubGetStatus(pending.sessionId);
        upsertSession({ ...fetched, status: "awaiting_permission" });
      } catch {
        // Hub unavailable or session gone — nothing to show.
      }
    });
  }, [upsertSession, enqueuePending]);

  // A rewound session's transcript on screen holds messages the session no
  // longer has. Reloading rather than patching, because the cut can reach
  // further back than the message the user named — a turn the agent never
  // replied to goes with it — and a patch would have to guess how far.
  //
  // The cards go too: a permission or a question belongs to a turn that has
  // been cut, and answering it would resolve something in a conversation that
  // no longer exists.
  useEffect(() => {
    return onAgentHubSessionRewound((sessionId) => {
      const state = useAgentSessionsStore.getState();
      state.dequeuePendingForSession(sessionId);
      state.dequeueElicitationForSession(sessionId);
      reloadTranscript(sessionId);
    });
  }, []);

  // Questions do not touch the session's status: the turn is still running
  // (awaiting_permission would say the agent is blocked on an authorization,
  // and would stop the stream's running dot). The queue alone drives the cards.
  useEffect(() => onAgentHubElicitation(enqueueElicitation), [enqueueElicitation]);
  useEffect(() => onAgentHubElicitationClosed((event) => dequeueElicitation(event.id)), [dequeueElicitation]);

  return (
    <div aria-live="polite" aria-atomic="true" className="sr-only" data-testid="agent-live-region">
      {announcement}
    </div>
  );
}
