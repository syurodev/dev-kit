import { describe, expect, it } from "bun:test";

import { drainQueuedPrompt } from "./agentPromptQueue";
import { useAgentSessionsStore, type QueuedAgentPrompt } from "../stores/agentSessions";

const queued = (id: string): QueuedAgentPrompt => ({ id, text: id, attachments: [], images: [] });

function resetQueue() {
  useAgentSessionsStore.setState({ queuedPromptsBySession: {}, queuedPromptInFlightBySession: {} });
}

describe("Agent prompt queue", () => {
  it("sends and removes the first prompt after a successful turn", async () => {
    resetQueue();
    const store = useAgentSessionsStore.getState();
    store.enqueuePrompt("s1", queued("first"));
    store.enqueuePrompt("s1", queued("second"));
    const sent: string[] = [];

    expect(await drainQueuedPrompt("s1", async (input) => {
      sent.push(input.prompt);
      return { status: "completed", summary: "", filesChanged: [], turnCount: 1, stopReason: "end_turn" };
    })).toBe(true);
    expect(sent).toEqual(["first"]);
    expect(useAgentSessionsStore.getState().queuedPromptsBySession.s1.map((item) => item.id)).toEqual(["second"]);
  });

  it("keeps a prompt when the turn errors or is cancelled", async () => {
    resetQueue();
    useAgentSessionsStore.getState().enqueuePrompt("s1", queued("retry-me"));

    expect(await drainQueuedPrompt("s1", async () => ({
      status: "error", summary: "", filesChanged: [], turnCount: 0, stopReason: "",
    }))).toBe(false);
    expect(useAgentSessionsStore.getState().queuedPromptsBySession.s1[0].id).toBe("retry-me");
    expect(useAgentSessionsStore.getState().queuedPromptInFlightBySession.s1).toBeUndefined();
  });
});
