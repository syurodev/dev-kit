import { agentHubInvoke, type InvokeResult } from "../bridge/agenthub";
import { useAgentSessionsStore } from "../stores/agentSessions";

type Invoke = (input: {
  sessionId: string;
  prompt: string;
  attachments: string[];
  images: import("../bridge/agenthub").AgentImageAttachment[];
}) => Promise<InvokeResult>;

/** Sends one queued prompt. A failed/cancelled turn stays queued for retry. */
export async function drainQueuedPrompt(sessionId: string, invoke: Invoke = agentHubInvoke): Promise<boolean> {
  const state = useAgentSessionsStore.getState();
  const prompt = state.claimQueuedPrompt(sessionId);
  if (!prompt) return false;
  try {
    const result = await invoke({
      sessionId,
      prompt: prompt.text,
      attachments: prompt.attachments,
      images: prompt.images,
    });
    if (result.status !== "completed") {
      state.releaseQueuedPrompt(sessionId, prompt.id);
      return false;
    }
    state.removeQueuedPrompt(sessionId, prompt.id);
    return true;
  } catch {
    state.releaseQueuedPrompt(sessionId, prompt.id);
    return false;
  }
}
