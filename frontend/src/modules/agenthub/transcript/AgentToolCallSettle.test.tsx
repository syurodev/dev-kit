import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";

import type { ToolCall, TranscriptItem } from "../types";
import { AgentToolCall } from "./AgentToolCall";
import { AgentToolActivity } from "./AgentToolActivity";
import { AgentRunningCommand } from "./AgentRunningCommand";
import { StepGroup } from "../surfaces/AgentTurn";
import { TranscriptStream } from "../surfaces/TranscriptStream";
import { EarlierTurnProvider } from "./toolSettle";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { AgentSession } from "@/bridge/agenthub";
import "@/modules/git/manifest";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function mount() {
  const container = document.createElement("div");
  document.body.appendChild(container);
  return { container, root: createRoot(container) };
}

describe("a tool call of a turn that is over", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    ({ container, root } = mount());
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  async function draw(node: React.ReactNode, earlier: boolean) {
    await act(async () => root.render(<EarlierTurnProvider earlier={earlier}>{node}</EarlierTurnProvider>));
  }
  const call = (status: string, extra: Partial<ToolCall> = {}): ToolCall => ({ id: "c1", kind: "read", title: "Read a.go", status, ...extra });
  const row = (c: ToolCall) => <AgentToolCall variant="row" call={c} />;
  const state = () => container.querySelector("[data-testid=agent-tool-call]")?.getAttribute("data-state");

  test.each(["pending", "in_progress", "running", "awaiting_permission"])("a %s call of an earlier turn says it did not finish", async (status) => {
    await draw(row(call(status)), true);
    expect(state()).toBe("unfinished");
    expect(container.textContent).toContain("didn't finish");
    expect(container.textContent).not.toContain("running…");
    // No sweep, no spinner: nothing about it moves.
    expect(container.querySelector("[data-active]")).toBeNull();
  });

  test.each([
    ["pending", "pending"],
    ["in_progress", "running"],
    ["awaiting_permission", "awaiting"],
  ])("a %s call of the latest turn is drawn as its status says, whatever the session is doing", async (status, expected) => {
    await draw(row(call(status)), false);
    expect(state()).toBe(expected);
  });

  test("the latest turn keeps its spinner", async () => {
    await draw(row(call("in_progress")), false);
    expect(container.textContent).toContain("running…");
    expect(container.querySelector("[data-active]")).not.toBeNull();
  });

  test("a call outside any turn (a panel, a fixture) is drawn as its status says", async () => {
    await act(async () => root.render(row(call("in_progress"))));
    expect(state()).toBe("running");
  });

  test("a call that ended stays what it was", async () => {
    await draw(row(call("completed")), true);
    expect(state()).toBe("done");
    await draw(row(call("failed")), true);
    expect(state()).toBe("failed");
  });

  test("the card variant says it too", async () => {
    await draw(<AgentToolCall call={call("pending")} />, true);
    expect(state()).toBe("unfinished");
    expect(container.textContent).toContain("didn't finish");
    expect(container.textContent).not.toContain("queued");
  });

  test("an activity card stops spinning and says so in neutral words", async () => {
    const commit: ToolCall = { id: "g1", name: "git.commit", kind: "other", title: "git.commit", status: "in_progress", rawInput: { message: "Fix login" } };
    await draw(<AgentToolActivity call={commit} />, false);
    const live = container.querySelector("[data-testid=tool-activity]")!;
    expect(live.getAttribute("data-state")).toBe("running");
    expect(live.textContent).toContain("Committing");

    await draw(<AgentToolActivity call={commit} />, true);
    const card = container.querySelector("[data-testid=tool-activity]")!;
    expect(card.getAttribute("data-state")).toBe("unfinished");
    expect(card.textContent).toContain("Didn't finish");
    expect(card.textContent).toContain("Fix login");
    // Not a present participle claiming it is under way.
    expect(card.textContent).not.toContain("Committing");
    expect(card.textContent).not.toContain("…");
    expect(card.querySelector("[data-active]")).toBeNull();
  });

  test("a running command draws no clock when its turn is over", async () => {
    const item = {
      id: "i1",
      kind: "tool_call",
      createdAt: new Date(Date.now() - 60_000).toISOString(),
      toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" },
    } as TranscriptItem;
    await draw(<AgentRunningCommand item={item} />, false);
    expect(container.textContent).toContain("bun test");
    await draw(<AgentRunningCommand item={item} />, true);
    expect(container.textContent).toBe("");
  });
});

describe("a run of steps with an interrupted command", () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    ({ container, root } = mount());
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  const items = [
    { id: "i1", kind: "tool_call", toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" } },
    { id: "i2", kind: "tool_call", toolCall: { id: "y", kind: "read", title: "Read a.go", status: "completed" } },
  ] as TranscriptItem[];

  test("keeps the slot for a running command only in the latest turn", async () => {
    await act(async () => root.render(<EarlierTurnProvider earlier={false}><StepGroup items={items} /></EarlierTurnProvider>));
    expect(container.querySelector("[data-testid=step-activities]")).not.toBeNull();
    await act(async () => root.render(<EarlierTurnProvider earlier={true}><StepGroup items={items} /></EarlierTurnProvider>));
    expect(container.querySelector("[data-testid=step-activities]")).toBeNull();
  });
});

describe("the transcript", () => {
  // Live-streamed items carry no sessionId (only initTranscript sets it), and the
  // verdict must not need one.
  const item = (id: string, kind: string, extra: Record<string, unknown> = {}) => ({ id, kind, ...extra }) as unknown as TranscriptItem;
  const transcript = [
    item("u1", "user_message_chunk", { text: "first" }),
    item("t1", "tool_call", { toolCall: { id: "c1", kind: "read", title: "Read old.go", status: "in_progress" } }),
    item("u2", "user_message_chunk", { text: "second" }),
    item("t2", "tool_call", { toolCall: { id: "c2", kind: "read", title: "Read new.go", status: "in_progress" } }),
  ];

  test("settles the call of an earlier turn and leaves the latest turn's alone", () => {
    const html = renderToString(<TranscriptStream items={transcript} streaming />);
    const states = [...html.matchAll(/data-testid="agent-tool-call"[^>]*data-state="([a-z]+)"/g)].map((m) => m[1]);
    expect(states).toEqual(["unfinished", "running"]);
  });

  test("a single turn is the latest turn", () => {
    const html = renderToString(<TranscriptStream items={transcript.slice(0, 2)} streaming />);
    expect(html).toContain('data-state="running"');
    expect(html).not.toContain('data-state="unfinished"');
  });

  test("a subagent's call of an earlier turn settles with it", () => {
    const html = renderToString(
      <TranscriptStream
        items={[
          item("u1", "user_message_chunk", { text: "first" }),
          item("t1", "tool_call", { toolCall: { id: "c1", name: "Agent", kind: "think", title: "Agent", status: "in_progress" } }),
          item("u2", "user_message_chunk", { text: "second" }),
        ]}
      />,
    );
    expect(html).toContain('data-state="unfinished"');
  });
});

describe("a call of the latest turn in a quiet session", () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    ({ container, root } = mount());
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  async function draw(node: React.ReactNode, quiet: boolean) {
    await act(async () => root.render(<EarlierTurnProvider earlier={false} quiet={quiet}>{node}</EarlierTurnProvider>));
  }
  const call = (status: string): ToolCall => ({ id: "c1", kind: "read", title: "Read a.go", status });
  const state = () => container.querySelector("[data-testid=agent-tool-call]")?.getAttribute("data-state");

  test.each(["pending", "in_progress", "awaiting_permission"])("a %s call says it did not finish", async (status) => {
    await draw(<AgentToolCall variant="row" call={call(status)} />, true);
    expect(state()).toBe("unfinished");
    expect(container.textContent).toContain("didn't finish");
  });

  test("a call that ended stays what it was", async () => {
    await draw(<AgentToolCall variant="row" call={call("completed")} />, true);
    expect(state()).toBe("done");
  });

  test("a subagent's call stays live: only an earlier turn settles it", async () => {
    await draw(<AgentToolCall variant="row" call={call("in_progress")} origin="sub-1" />, true);
    expect(state()).toBe("running");
  });

  test("an activity card stops spinning too, unless it is a subagent's", async () => {
    const commit: ToolCall = { id: "g1", name: "git.commit", kind: "other", title: "git.commit", status: "in_progress", rawInput: { message: "Fix login" } };
    await draw(<AgentToolActivity call={commit} />, true);
    expect(container.querySelector("[data-testid=tool-activity]")!.getAttribute("data-state")).toBe("unfinished");
    await draw(<AgentToolActivity call={commit} origin="sub-1" />, true);
    expect(container.querySelector("[data-testid=tool-activity]")!.getAttribute("data-state")).toBe("running");
  });

  test("a running command draws no clock, unless it is a subagent's", async () => {
    const item = (origin?: string) =>
      ({
        id: "i1",
        kind: "tool_call",
        origin,
        createdAt: new Date(Date.now() - 60_000).toISOString(),
        toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" },
      }) as TranscriptItem;
    await draw(<AgentRunningCommand item={item()} />, true);
    expect(container.textContent).toBe("");
    await draw(<AgentRunningCommand item={item("sub-1")} />, true);
    expect(container.textContent).toContain("bun test");
  });

  test("a step run passes a row's origin on: its subagent's call stays live, the lead's settles", async () => {
    const read = (id: string, origin?: string) =>
      ({ id, kind: "tool_call", origin, toolCall: { id, kind: "read", title: `Read ${id}`, status: "in_progress" } }) as TranscriptItem;
    await draw(<StepGroup items={[read("lead"), read("sub", "sub-1")]} defaultOpen />, true);
    const states = [...container.querySelectorAll("[data-testid=agent-tool-call]")].map((el) => el.getAttribute("data-state"));
    expect(states).toEqual(["unfinished", "running"]);
  });

  test("a step run keeps the slot for a running command only while the session may still finish it", async () => {
    const items = [
      { id: "i1", kind: "tool_call", toolCall: { id: "x", kind: "execute", title: "bun test", status: "in_progress" } },
    ] as TranscriptItem[];
    await draw(<StepGroup items={items} />, false);
    expect(container.querySelector("[data-testid=step-activities]")).not.toBeNull();
    await draw(<StepGroup items={items} />, true);
    expect(container.querySelector("[data-testid=step-activities]")).toBeNull();
    const sub = [{ ...items[0], origin: "sub-1" }] as TranscriptItem[];
    await draw(<StepGroup items={sub} />, true);
    expect(container.querySelector("[data-testid=step-activities]")).not.toBeNull();
  });
});

describe("the transcript of a session the window knows", () => {
  // Drawn on a client: the store a server render reads is its initial state, not what the test set.
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    ({ container, root } = mount());
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    await setSession(null);
  });
  const item = (id: string, kind: string, extra: Record<string, unknown> = {}) => ({ id, kind, ...extra }) as unknown as TranscriptItem;
  const latestTurn = [
    item("u1", "user_message_chunk", { text: "go" }),
    item("t1", "tool_call", { toolCall: { id: "c1", kind: "read", title: "Read new.go", status: "in_progress" } }),
  ];
  const ref = { sessionId: "s1", workspaceId: "", projectPath: "/p" };
  const row = (over: Partial<AgentSession> = {}): AgentSession => ({
    id: "s1", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "s1", acpSessionId: "", ...over,
  });
  const states = async (items: TranscriptItem[], props: { streaming?: boolean } = {}) => {
    await act(async () => root.render(<TranscriptStream items={items} refContext={ref} {...props} />));
    return [...container.querySelectorAll("[data-testid=agent-tool-call]")].map((el) => el.getAttribute("data-state"));
  };
  // Through act: a mounted TranscriptStream subscribes to the store, and a store update outside
  // act is an update outside act.
  const setSession = (session: AgentSession | null, inFlight = false) =>
    act(async () => {
      useAgentSessionsStore.setState({
        sessionsById: session ? { s1: session } : {},
        turnInFlightBySession: inFlight ? { s1: true } : {},
      });
    });

  test("an idle session's latest turn no longer spins: a cancelled turn's call is settled", async () => {
    await setSession(row());
    expect(await states(latestTurn)).toEqual(["unfinished"]);
  });

  test("a session with only background tasks is quiet too", async () => {
    await setSession(row({ activity: { agentTurn: false, tasks: 2 } }));
    expect(await states(latestTurn)).toEqual(["unfinished"]);
  });

  test("an agent-started turn keeps its calls live", async () => {
    await setSession(row({ activity: { agentTurn: true, tasks: 0 } }));
    expect(await states(latestTurn)).toEqual(["running"]);
  });

  test("a running turn, or a turn in flight, keeps its calls live", async () => {
    await setSession(row({ status: "running" }));
    expect(await states(latestTurn)).toEqual(["running"]);
    await setSession(row(), true);
    expect(await states(latestTurn)).toEqual(["running"]);
  });

  // Its own test, rendered streaming from the first frame: a live turn starts motion animations, and
  // happy-dom reports one cancelled on unmount as an unhandled AbortError when it began mid-test.
  test("a caller that says a turn streams keeps its calls live, whatever the store says", async () => {
    await setSession(row());
    expect(await states(latestTurn, { streaming: true })).toEqual(["running"]);
  });

  test("a session waiting for permission keeps its calls live", async () => {
    await setSession(row({ status: "awaiting_permission" }));
    expect(await states(latestTurn)).toEqual(["running"]);
  });

  test("a session that is not loaded settles nothing", async () => {
    await setSession(null);
    expect(await states(latestTurn)).toEqual(["running"]);
  });
});

describe("a failed call's first line", () => {
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
  async function draw(call: ToolCall) {
    await act(async () => root.render(<AgentToolCall variant="row" call={call} />));
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
