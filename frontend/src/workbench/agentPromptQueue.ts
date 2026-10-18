import {
  agentHubGetStatus,
  agentHubInvoke,
  type AgentImageAttachment,
  type AgentSession,
  type InvokeResult,
  type InvokeSessionInput,
} from "../bridge/agenthub";
import { isAgentAuthRequiredError, isAgentNotInstalledError, isRuntimeRequirementError } from "../bridge/agentRuntime";
import { toAppError } from "../bridge/errors";
import { useAgentPinsStore } from "../stores/agentPins";
import { useAgentSessionsStore, type QueuedAgentPrompt } from "../stores/agentSessions";
import { failureDismissed } from "../modules/agenthub/sidebar/dismissed";
import { moduleTabs, useWorkbenchTabsStore, type TabStatus } from "../stores/workbenchTabs";
import { encodeEntityRef } from "./entityRef";
import { AGENTHUB_MODULE, tabSessionId } from "../modules/agenthub/documents";
import type { EntityRef } from "./intents";

export type TurnBridge = {
  invoke: (input: InvokeSessionInput) => Promise<InvokeResult>;
  getStatus: (sessionId: string) => Promise<AgentSession>;
};

const agentBridge: TurnBridge = { invoke: agentHubInvoke, getStatus: agentHubGetStatus };

export function queuedPrompt(
  text: string,
  attachments: string[],
  images: AgentImageAttachment[],
  refs: EntityRef[] = [],
): QueuedAgentPrompt {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `queued-${Date.now()}-${Math.random()}`,
    text,
    attachments,
    images,
    refs,
  };
}

/**
 * Another turn holds the session. Go reports ErrSessionBusy as a validation
 * failure with this exact sentence (internal/bridge/errors.go); the code alone
 * is shared with every other refusal.
 */
function isBusy(error: unknown): boolean {
  const app = toAppError(error);
  return app.code === "validation_failed" && app.message === "Agent session is busy.";
}

/**
 * Refused before the agent was given the prompt: Invoke's own checks, starting
 * the runtime, validating what was attached. Anything else failed inside a
 * turn the prompt already started, and sending it again would say it twice.
 */
function refusedBeforeTurn(error: unknown): boolean {
  return (
    toAppError(error).code === "validation_failed" ||
    isAgentNotInstalledError(error) ||
    isRuntimeRequirementError(error) ||
    isAgentAuthRequiredError(error)
  );
}

function setStatus(sessionId: string, status: AgentSession["status"]) {
  const { sessionsById, upsertSession } = useAgentSessionsStore.getState();
  const session = sessionsById[sessionId];
  // A session deleted while its turn ran stays deleted.
  if (session) upsertSession({ ...session, status });
}

async function refreshStatus(sessionId: string, bridge: TurnBridge, fallback: AgentSession["status"]) {
  try {
    const fresh = await bridge.getStatus(sessionId);
    const state = useAgentSessionsStore.getState();
    // A turn that started meanwhile owns the status now.
    if (state.sessionsById[sessionId] && !state.turnInFlightBySession[sessionId]) state.upsertSession(fresh);
  } catch {
    // Never leave a Stop button up for a turn that is over.
    if (!useAgentSessionsStore.getState().turnInFlightBySession[sessionId]) setStatus(sessionId, fallback);
  }
}

/**
 * The one way a turn is sent: the hub's composer, the companion, Retry and the
 * queue all come through here, so every turn marks its session running before
 * invoke and settles it the same way afterwards.
 *
 * Resolves null when the session was busy and the prompt went into the queue
 * instead — its text is kept, not cleared with the composer's.
 */
export async function sendAgentTurn(
  sessionId: string,
  prompt: QueuedAgentPrompt,
  bridge: TurnBridge = agentBridge,
): Promise<InvokeResult | null> {
  if (!useAgentSessionsStore.getState().beginTurn(sessionId)) {
    useAgentSessionsStore.getState().enqueuePrompt(sessionId, prompt);
    return null;
  }
  return runTurn(sessionId, prompt, false, bridge);
}

/** Sends the next queued prompt, when the session can take one. */
export async function drainQueuedPrompt(sessionId: string, bridge: TurnBridge = agentBridge): Promise<boolean> {
  const prompt = useAgentSessionsStore.getState().claimQueuedPrompt(sessionId);
  if (!prompt) return false;
  try {
    return (await runTurn(sessionId, prompt, true, bridge))?.status === "completed";
  } catch {
    return false;
  }
}

/** The person's go-ahead after a failed or cancelled turn paused the queue. */
export function resumeQueuedPrompts(sessionId: string, bridge: TurnBridge = agentBridge): void {
  useAgentSessionsStore.getState().resumeQueue(sessionId);
  void drainQueuedPrompt(sessionId, bridge);
}

/**
 * Sessions where some turn finished while one of ours was in flight. The
 * handler leaves the drain to that turn; if ours is then refused as busy, the
 * turn that held the session may be the one that finished, and no later event
 * will drain what the refusal queued. Only a finish seen: Go also refuses while
 * another invoke is resuming with its status still idle, and retrying on the
 * status alone would spin until it says running.
 */
const finishedDuringTurn = new Set<string>();

export function noteTurnFinishedDuringOurs(sessionId: string): void {
  finishedDuringTurn.add(sessionId);
}

/**
 * The mark on every Agent Hub tab showing this session. Only those: the
 * new-session tab shows no session (its draft becomes a session tab of its
 * own), and now that every tab shows its status it would carry every
 * session's.
 */
export function setAgentHubTabsStatus(sessionId: string, status: TabStatus): void {
  const { modules, setTabStatus } = useWorkbenchTabsStore.getState();
  // A failure the person dismissed (sidebar/dismissed.ts) settles to idle: the
  // red dot says "look at this", and they said not to.
  const session = useAgentSessionsStore.getState().sessionsById[sessionId];
  const settled =
    status === "error" && session && failureDismissed(session, useAgentPinsStore.getState().dismissedFailures)
      ? "idle"
      : status;
  for (const tab of moduleTabs(modules, AGENTHUB_MODULE).openTabs) {
    if (tabSessionId(tab) === sessionId) setTabStatus(tab.id, settled);
  }
}

/**
 * A turn of ours settled; `completed` when it ran to its end, which marks the
 * tabs finished until they are looked at (workbench/tabs/seenSuccess.ts).
 * turnFinished leaves the mark alone while one is in flight.
 */
function settleTabs(sessionId: string, completed = false) {
  const state = useAgentSessionsStore.getState();
  const status = state.sessionsById[sessionId]?.status;
  // The next queued turn has started, or another agent's turn holds the session.
  if (state.turnInFlightBySession[sessionId] || status === "running" || status === "awaiting_permission") return;
  setAgentHubTabsStatus(sessionId, status === "error" ? "error" : completed ? "success" : "idle");
}

/** Runs a turn already marked in flight (beginTurn / claimQueuedPrompt). */
async function runTurn(
  sessionId: string,
  prompt: QueuedAgentPrompt,
  fromQueue: boolean,
  bridge: TurnBridge,
): Promise<InvokeResult | null> {
  const store = useAgentSessionsStore.getState();
  finishedDuringTurn.delete(sessionId);
  setStatus(sessionId, "running");
  let result: InvokeResult;
  try {
    result = await bridge.invoke({
      sessionId,
      prompt: prompt.text,
      attachments: prompt.attachments,
      images: prompt.images,
      refs: prompt.refs.map(encodeEntityRef),
    });
  } catch (error) {
    const busy = isBusy(error);
    const refused = refusedBeforeTurn(error);
    if (fromQueue && refused) store.releaseQueuedPrompt(sessionId, prompt);
    else if (busy) store.enqueuePrompt(sessionId, prompt);
    store.finishTurn(sessionId);
    // A turn that ran and failed or was cancelled pauses the queue, and so does
    // a queued prompt refused for itself: sent again, it is refused again. Busy,
    // or a manual send refused before its turn, ran no turn.
    const { code, message } = toAppError(error);
    // The reason is shown by Paused: a queued prompt's error is not thrown to anyone.
    if (!busy && (fromQueue || !refused)) store.pauseQueue(sessionId, message);
    await refreshStatus(sessionId, bridge, busy ? "running" : code === "operation_cancelled" ? "idle" : "error");
    // claimQueuedPrompt sends only if the fresh status can take it and the queue is not paused.
    if (busy) {
      if (finishedDuringTurn.delete(sessionId)) void drainQueuedPrompt(sessionId, bridge);
      settleTabs(sessionId);
      return null;
    }
    void drainQueuedPrompt(sessionId, bridge);
    settleTabs(sessionId);
    throw error;
  }
  store.finishTurn(sessionId);
  // A refused tool is the user's own answer, not a broken session: the turn ran
  // to its end and the session is idle. The queue still pauses — what the agent
  // was doing was interrupted on purpose, and the prompts behind it were
  // written for a plan that has just changed.
  if (result.status === "denied") {
    store.pauseQueue(sessionId, "You denied a permission.");
    await refreshStatus(sessionId, bridge, "idle");
    settleTabs(sessionId);
    return result;
  }
  if (result.status !== "completed") {
    store.pauseQueue(sessionId);
    await refreshStatus(sessionId, bridge, "error");
    settleTabs(sessionId);
    return result;
  }
  // Go set the session idle before replying. Not re-read: the next queued turn
  // starts now, and a late read would take its Stop button away.
  setStatus(sessionId, "idle");
  void drainQueuedPrompt(sessionId, bridge);
  settleTabs(sessionId, true);
  return result;
}
