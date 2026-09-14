import { beforeEach, describe, expect, it } from "bun:test";

import { useAgentSessionsStore } from "./agentSessions";
import type { AgentSession } from "../bridge/agenthub";

const session = (id: string): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/p",
  status: "running",
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: `acp-${id}`,
});

describe("subagents in the session store", () => {
  beforeEach(() => {
    useAgentSessionsStore.setState({ sessions: [], subagents: {} });
  });

  it("merges the state update onto the spawn instead of replacing it", () => {
    // The spawn names the subagent and the state update only says how it ended.
    // Replacing one with the other leaves a row with no name.
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "sub-1", name: "Explore", task: "map the repo" });
    trackSubagent("s1", { id: "sub-1", state: "completed" });

    const [subagent] = useAgentSessionsStore.getState().subagents.s1;
    expect(subagent.name).toBe("Explore");
    expect(subagent.task).toBe("map the repo");
    expect(subagent.state).toBe("completed");
  });

  it("drops a subagent that was killed rather than greying it out", () => {
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "sub-1", name: "Explore" });
    trackSubagent("s1", { id: "sub-2", name: "Verify" });

    trackSubagent("s1", { id: "sub-1", state: "cancelled" });
    expect(useAgentSessionsStore.getState().subagents.s1.map((s) => s.id)).toEqual(["sub-2"]);

    // The session going away takes the rest of them with it.
    trackSubagent("s1", { id: "sub-2", state: "disconnected" });
    expect(useAgentSessionsStore.getState().subagents.s1).toEqual([]);
  });

  it("keeps the ones that ran to an end, so the dot can say how", () => {
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "ok", name: "Explore" });
    trackSubagent("s1", { id: "bad", name: "Verify" });
    trackSubagent("s1", { id: "ok", state: "completed" });
    trackSubagent("s1", { id: "bad", state: "failed" });

    expect(useAgentSessionsStore.getState().subagents.s1.map((s) => s.state)).toEqual([
      "completed",
      "failed",
    ]);
  });

  it("forgets a session's subagents when the session is removed", () => {
    const { upsertSession, trackSubagent, removeSession } = useAgentSessionsStore.getState();
    upsertSession(session("s1"));
    trackSubagent("s1", { id: "sub-1", name: "Explore" });

    removeSession("s1");

    expect(useAgentSessionsStore.getState().subagents.s1).toBeUndefined();
  });
});
