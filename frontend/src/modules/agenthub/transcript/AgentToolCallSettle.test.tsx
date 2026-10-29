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
