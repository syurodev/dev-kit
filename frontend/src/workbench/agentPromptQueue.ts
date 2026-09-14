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
import { useAgentSessionsStore, type QueuedAgentPrompt } from "../stores/agentSessions";

export type TurnBridge = {
  invoke: (input: InvokeSessionInput) => Promise<InvokeResult>;
  getStatus: (sessionId: string) => Promise<AgentSession>;
};

const agentBridge: TurnBridge = { invoke: agentHubInvoke, getStatus: agentHubGetStatus };

export function queuedPrompt(text: string, attachments: string[], images: AgentImageAttachment[]): QueuedAgentPrompt {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `queued-${Date.now()}-${Math.random()}`,
    text,
    attachments,
    images,
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

/** Runs a turn already marked in flight (beginTurn / claimQueuedPrompt). */
async function runTurn(
  sessionId: string,
  prompt: QueuedAgentPrompt,
  fromQueue: boolean,
  bridge: TurnBridge,
): Promise<InvokeResult | null> {
  const store = useAgentSessionsStore.getState();
  setStatus(sessionId, "running");
  let result: InvokeResult;
  try {
    result = await bridge.invoke({
      sessionId,
      prompt: prompt.text,
      attachments: prompt.attachments,
      images: prompt.images,
    });
  } catch (error) {
    const busy = isBusy(error);
    if (fromQueue && refusedBeforeTurn(error)) store.releaseQueuedPrompt(sessionId, prompt);
    else if (busy) store.enqueuePrompt(sessionId, prompt);
    store.finishTurn(sessionId);
    // Busy is not a failed turn: the queue goes on once the other one ends.
    if (!busy) store.pauseQueue(sessionId);
    const code = toAppError(error).code;
    await refreshStatus(sessionId, bridge, busy ? "running" : code === "operation_cancelled" ? "idle" : "error");
    if (busy) return null;
    throw error;
  }
  store.finishTurn(sessionId);
  if (result.status !== "completed") {
    store.pauseQueue(sessionId);
    await refreshStatus(sessionId, bridge, "error");
    return result;
  }
  // Go set the session idle before replying. Not re-read: the next queued turn
  // starts now, and a late read would take its Stop button away.
  setStatus(sessionId, "idle");
  void drainQueuedPrompt(sessionId, bridge);
  return result;
}
