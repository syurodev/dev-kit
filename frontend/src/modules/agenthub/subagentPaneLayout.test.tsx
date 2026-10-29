import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useAgentSessionsStore } from "@/stores/agentSessions";
import { SubagentPanel } from "./subagents";
import { SubagentActivity } from "./surfaces/AgentTurn";
import type { TranscriptItem } from "./types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The pane is 420px wide. The nesting rule and indent that belong to a fold
// inside the transcript (ml-1.5 border-l pl-4) plus a 12px well took about a
// quarter of it; the pane's content starts at its own 8px padding instead.
const NESTING = ["ml-1.5", "border-l", "pl-4"];

const step = (id: string, origin?: string): TranscriptItem => ({
  id,
  kind: "tool_call",
  ...(origin ? { origin } : {}),
  toolCall: { id, kind: "execute", title: "go test ./...", status: "completed" },
});

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
  useAgentSessionsStore.setState({ transcriptsById: {} as never });
});

const classes = (testId: string) => [...(container.querySelector(`[data-testid=${testId}]`)?.classList ?? [])];

test("SubagentActivity keeps its nesting rule in the transcript and drops it when flat", async () => {
  const sub = { id: "a1", name: "Review" };
  await act(async () => root.render(<SubagentActivity subagent={sub} activity={[step("r1", "a1")]} brief={false} />));
  for (const cls of NESTING) expect(classes("subagent-activity")).toContain(cls);

  await act(async () => root.render(<SubagentActivity subagent={sub} activity={[step("r1", "a1")]} brief={false} flat />));
  for (const cls of NESTING) expect(classes("subagent-activity")).not.toContain(cls);
  expect(classes("subagent-activity")).toContain("min-w-0");
});

test("the pane puts a subagent's work in an 8px well, flat", async () => {
  const items: TranscriptItem[] = [
    { id: "spawn", kind: "subagent_spawned", subagent: { id: "a1", name: "Review", task: "look" } },
    step("r1", "a1"),
  ];
  useAgentSessionsStore.setState({ transcriptsById: { sp: { items } } as never });
  await act(async () => root.render(<SubagentPanel sessionId="sp" subagentId="a1" onClose={() => {}} />));
  expect(classes("subagent-panel-activity")).toContain("p-2");
  expect(classes("subagent-panel-activity")).not.toContain("p-3");
  for (const cls of NESTING) expect(classes("subagent-activity")).not.toContain(cls);
});
