import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { SessionStatus } from "@/bridge/agenthub";
import type { ToolCall, TranscriptItem } from "../types";
import { AgentToolCall } from "./AgentToolCall";
import { AgentToolActivity } from "./AgentToolActivity";
import { AgentRunningCommand } from "./AgentRunningCommand";
import { StepGroup } from "../surfaces/AgentTurn";
import "@/modules/git/manifest";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const SESSION = "s1";

function setSession(status: SessionStatus, turnInFlight = false) {
  useAgentSessionsStore.setState({
    sessionsById: {
      [SESSION]: { id: SESSION, agentType: "claude", projectPath: "/p", status, parentSessionId: null, depth: 0, title: "s", acpSessionId: "" },
    },
    turnInFlightBySession: turnInFlight ? { [SESSION]: true } : {},
  });
}

describe("a tool call that can no longer finish", () => {
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

  async function draw(node: React.ReactNode) {
    await act(async () => root.render(node));
  }
  const row = (call: ToolCall, sessionId: string | null = SESSION) => (
    <AgentToolCall variant="row" call={call} sessionId={sessionId ?? undefined} />
  );
  const call = (status: string, extra: Partial<ToolCall> = {}): ToolCall => ({ id: "c1", kind: "read", title: "Read a.go", status, ...extra });
  const state = () => container.querySelector("[data-testid=agent-tool-call]")?.getAttribute("data-state");

  test.each(["pending", "in_progress", "running"])("a %s call of an idle session says it did not finish", async (status) => {
    setSession("idle");
    await draw(row(call(status)));
    expect(state()).toBe("unfinished");
    expect(container.textContent).toContain("didn't finish");
    expect(container.textContent).not.toContain("running…");
    // No sweep, no spinner: nothing about it moves.
    expect(container.querySelector("[data-active]")).toBeNull();
  });

  test.each(["error", "closed"] as const)("and one of a %s session", async (status) => {
    setSession(status);
    await draw(row(call("in_progress")));
    expect(state()).toBe("unfinished");
  });

  test("a running session keeps its spinner", async () => {
    setSession("running");
    await draw(row(call("in_progress")));
    expect(state()).toBe("running");
    expect(container.textContent).toContain("running…");
    expect(container.querySelector("[data-active]")).not.toBeNull();
  });

  test("so does a call of a turn this window has in flight, before the row says running", async () => {
    setSession("idle", true);
    await draw(row(call("in_progress")));
    expect(state()).toBe("running");
  });

  test("a call outside any known session is drawn as its status says", async () => {
    setSession("idle");
    await draw(row(call("in_progress"), null));
    expect(state()).toBe("running");
    await draw(row(call("in_progress"), "not-loaded"));
    expect(state()).toBe("running");
  });

  test("a call that ended stays what it was", async () => {
    setSession("idle");
    await draw(row(call("completed")));
    expect(state()).toBe("done");
    await draw(row(call("failed")));
    expect(state()).toBe("failed");
  });

  test("the card variant says it too", async () => {
    setSession("idle");
    await draw(<AgentToolCall call={call("pending")} sessionId={SESSION} />);
    expect(state()).toBe("unfinished");
    expect(container.textContent).toContain("didn't finish");
    expect(container.textContent).not.toContain("queued");
  });

  test("an activity card stops spinning, and says so", async () => {
    const commit: ToolCall = { id: "g1", name: "git.commit", kind: "other", title: "git.commit", status: "in_progress", rawInput: { message: "Fix login" } };
    setSession("running");
    await draw(<AgentToolActivity call={commit} sessionId={SESSION} />);
    expect(container.querySelector("[data-testid=tool-activity]")?.getAttribute("data-state")).toBe("running");
    expect(container.querySelector("[role=status], [aria-label]")).not.toBeNull();

    await act(async () => setSession("idle"));
    const card = container.querySelector("[data-testid=tool-activity]")!;
    expect(card.getAttribute("data-state")).toBe("unfinished");
    expect(card.textContent).toContain("didn't finish");
    expect(card.textContent).toContain("Fix login");
    expect(card.textContent).not.toContain("…");
    expect(card.querySelector("[data-active]")).toBeNull();
  });

  test("a running command draws no clock when nothing can finish it", async () => {
    const item: TranscriptItem = {
      id: "i1",
      kind: "tool_call",
      sessionId: SESSION,
      createdAt: new Date(Date.now() - 60_000).toISOString(),
      toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" },
    } as TranscriptItem;
    setSession("running");
    await draw(<AgentRunningCommand item={item} />);
    expect(container.textContent).toContain("bun test");
    await act(async () => setSession("idle"));
    expect(container.textContent).toBe("");
  });
});

describe("a run of steps with an interrupted command", () => {
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

  const items = [
    { id: "i1", kind: "tool_call", sessionId: SESSION, toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" } },
    { id: "i2", kind: "tool_call", sessionId: SESSION, toolCall: { id: "y", kind: "read", title: "Read a.go", status: "completed" } },
  ] as TranscriptItem[];

  test("keeps the slot for a running command only while the session runs", async () => {
    setSession("running");
    await act(async () => root.render(<StepGroup items={items} />));
    expect(container.querySelector("[data-testid=step-activities]")).not.toBeNull();
    await act(async () => setSession("idle"));
    expect(container.querySelector("[data-testid=step-activities]")).toBeNull();
  });
});

describe("a failed call's first line", () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    setSession("idle");
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });
  async function draw(call: ToolCall) {
    await act(async () => root.render(<AgentToolCall variant="row" call={call} sessionId={SESSION} />));
  }
  const said = () => container.querySelector("[data-testid=tool-call-said]")?.textContent;
  const failed = (extra: Partial<ToolCall>): ToolCall => ({ id: "c1", kind: "execute", title: "bun test", status: "failed", ...extra });

  test("shows what a vendor sent as content, in the collapsed row", async () => {
    await draw(failed({ content: [{ type: "content", content: { type: "text", text: "\nExit code 1\nrest of it" } }] }));
    expect(said()).toBe("· Exit code 1");
  });

  test("shows an error an object output carries", async () => {
    await draw(failed({ rawOutput: { error: "permission denied" } }));
    expect(said()).toBe("· permission denied");
  });

  test("is not drawn twice when the row already shows the reason", async () => {
    await draw(failed({ rawOutput: "Exit code 1\nrest of it" }));
    expect(said()).toBeUndefined();
    expect(container.querySelector("[data-testid=tool-call-reason]")?.textContent).toContain("Exit code 1");
  });

  test("is not drawn for a call that did not fail, or one with nothing to say", async () => {
    await draw(failed({ status: "completed", rawOutput: { error: "ignored" } }));
    expect(said()).toBeUndefined();
    await draw(failed({}));
    expect(said()).toBeUndefined();
  });
});
