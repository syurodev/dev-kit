import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { AgentSession } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { SubagentPanel } from "./subagents";
import type { TranscriptItem } from "./types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// A subagent's call left in_progress in the stored transcript. In the pane, as
// in the transcript, it spins only while something can still finish it: the
// subagent tracked as running, or the session working. Live rows carry no
// sessionId, so the pane's own session is what the lookup uses.
const items: TranscriptItem[] = [
  { id: "spawn", kind: "subagent_spawned", subagent: { id: "a1", name: "Review", task: "look" } },
  { id: "t1", kind: "tool_call", origin: "a1", toolCall: { id: "c1", kind: "read", title: "Read a.go", status: "in_progress" } },
] as TranscriptItem[];
const session = { id: "sp", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "sp", acpSessionId: "" } as AgentSession;

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  useAgentSessionsStore.setState({ transcriptsById: {}, sessionsById: {}, subagents: {}, turnInFlightBySession: {} } as never);
});

async function draw(opts: { tracked?: string; inFlight?: boolean }) {
  useAgentSessionsStore.setState({
    transcriptsById: { sp: { items } } as never,
    sessionsById: { sp: session },
    turnInFlightBySession: opts.inFlight ? { sp: true } : {},
    subagents: { sp: opts.tracked ? [{ id: "a1", name: "Review", state: opts.tracked }] : [] } as never,
  });
  await act(async () => root.render(<SubagentPanel sessionId="sp" subagentId="a1" onClose={() => {}} />));
  return container.querySelector("[data-testid=agent-tool-call]")?.getAttribute("data-state");
}

test("a subagent's unfinished call says it did not finish once its subagent is not tracked as running and the session is quiet", async () => {
  expect(await draw({})).toBe("unfinished");
});

test("it stays live while the subagent is tracked as running", async () => {
  expect(await draw({ tracked: "running" })).toBe("running");
});

test("it stays live while a turn of the session is in flight", async () => {
  expect(await draw({ inFlight: true })).toBe("running");
});
