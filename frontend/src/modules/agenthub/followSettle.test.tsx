import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { AgentActivityPanel } from "./follow";
import type { TranscriptItem } from "./types";
import "@/modules/git/manifest";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
useAgentPinsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

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
});

const commit = (id: string): TranscriptItem =>
  ({ id, kind: "tool_call", toolCall: { id, name: "git.commit", kind: "other", status: "in_progress", rawInput: { message: id } } }) as TranscriptItem;
const user = (id: string): TranscriptItem => ({ id, kind: "user_message_chunk", text: id }) as TranscriptItem;

test("the follow panel settles a card of an earlier turn and keeps the latest turn's spinning", async () => {
  const items = [user("u1"), commit("old"), user("u2"), commit("new")];
  useAgentSessionsStore.setState({ transcriptsById: { s1: { items } } as never });
  await act(async () => root.render(<AgentActivityPanel sessionId="s1" />));
  const states = [...container.querySelectorAll("[data-testid=tool-activity]")].map((el) => `${el.textContent?.includes("old") ? "old" : "new"}:${el.getAttribute("data-state")}`);
  expect(states.sort()).toEqual(["new:running", "old:unfinished"]);
});

test("the follow panel keeps a subagent's card spinning only while that subagent is tracked as running", async () => {
  const sub = { ...commit("sub-commit"), origin: "sub-1" } as TranscriptItem;
  const session = { id: "s1", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "t", acpSessionId: "" };
  const state = async (tracked: boolean) => {
    useAgentSessionsStore.setState({
      transcriptsById: { s1: { items: [user("u1"), sub] } } as never,
      sessionsById: { s1: session } as never,
      subagents: { s1: tracked ? [{ id: "sub-1", name: "Look", state: "running" }] : [] } as never,
    });
    await act(async () => root.render(<AgentActivityPanel sessionId="s1" />));
    return container.querySelector("[data-testid=tool-activity]")?.getAttribute("data-state");
  };
  try {
    expect(await state(false)).toBe("unfinished");
    expect(await state(true)).toBe("running");
  } finally {
    useAgentSessionsStore.setState({ sessionsById: {}, subagents: {} } as never);
  }
});

test("the follow panel draws a subagent's running command clock only while that subagent is tracked as running", async () => {
  const cmd = {
    id: "i1",
    kind: "tool_call",
    origin: "sub-1",
    createdAt: new Date(Date.now() - 60_000).toISOString(),
    toolCall: { id: "x", kind: "execute", title: "bun test --isolate", status: "in_progress" },
  } as TranscriptItem;
  const session = { id: "s1", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "t", acpSessionId: "" };
  const text = async (tracked: boolean) => {
    useAgentSessionsStore.setState({
      transcriptsById: { s1: { items: [user("u1"), cmd] } } as never,
      sessionsById: { s1: session } as never,
      subagents: { s1: tracked ? [{ id: "sub-1", name: "Look", state: "running" }] : [] } as never,
    });
    await act(async () => root.render(<AgentActivityPanel sessionId="s1" />));
    return container.textContent ?? "";
  };
  try {
    expect(await text(false)).not.toContain("bun test --isolate");
    expect(await text(true)).toContain("bun test --isolate");
  } finally {
    useAgentSessionsStore.setState({ sessionsById: {}, subagents: {} } as never);
  }
});
