import { domWindow } from "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { AgentSession } from "@/bridge/agenthub";

const cancelled: string[] = [];
const realBridge = await import("@/bridge/agenthub");
mock.module("@/bridge/agenthub", () => ({
  ...realBridge,
  agentHubCancel: async (id: string) => void cancelled.push(id),
  agentHubGetStatus: async (id: string) => ({ id, status: "idle" }) as AgentSession,
}));

import { TooltipProvider } from "@/components/ui/tooltip";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { SubagentPanel } from "./subagents";
import { useSubagentHeader } from "./subagentHeader";
import type { Subagent, TranscriptItem } from "./types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// Radix's dialogs dispatch their own events; happy-dom accepts only its own classes, not Bun's.
for (const key of ["Event", "CustomEvent"] as const) Object.defineProperty(globalThis, key, { value: domWindow[key], configurable: true, writable: true });

/** What the shell does with a pane's header, row for row (components/ui/pane-slots.tsx). */
function ShellRow({ sessionId, subagentId }: { sessionId: string; subagentId: string }) {
  const header = useSubagentHeader(sessionId, subagentId);
  return (
    <TooltipProvider>
      <div data-testid="row">
        <span data-testid="icon">{header.icon}</span>
        <span data-testid="title" title={header.titleHint}>
          {header.title}
        </span>
        <span data-testid="status">{header.status}</span>
        <span data-testid="actions">{header.actions}</span>
      </div>
    </TooltipProvider>
  );
}

const session = (status: AgentSession["status"], agentTurn = false) =>
  ({ id: "s1", status, ...(agentTurn ? { activity: { agentTurn: true } } : {}) }) as AgentSession;

const seed = (subagent: Subagent | null, sessionState: AgentSession | null, items: TranscriptItem[] = []) =>
  act(async () =>
    useAgentSessionsStore.setState({
    sessionsById: sessionState ? { s1: sessionState } : {},
    subagents: subagent ? { s1: [subagent] } : {},
    transcriptsById: { s1: { items } } as never,
    }),
  );

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  cancelled.length = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  document.body.querySelectorAll("[role=alertdialog]").forEach((el) => el.remove());
  useAgentSessionsStore.setState({ sessionsById: {}, subagents: {}, transcriptsById: {} as never });
});

const draw = () => act(async () => root.render(<ShellRow sessionId="s1" subagentId="a1" />));
const q = (selector: string) => container.querySelector<HTMLElement>(selector);
const stop = () => q("[data-testid=actions] button");
const dialog = () => document.body.querySelector<HTMLElement>("[role=alertdialog]");
const press = (el: HTMLElement | null | undefined) => act(async () => el!.click());
const buttonNamed = (scope: ParentNode, name: string) => [...scope.querySelectorAll<HTMLElement>("button")].find((b) => b.textContent === name);

describe("the subagent pane's header", () => {
  test("is named for the subagent, with its task as the tooltip, a Bot icon and its state", async () => {
    await seed({ id: "a1", name: "Review whitelist", task: "Check the list", state: "completed" }, session("idle"));
    await draw();
    expect(q("[data-testid=title]")?.textContent).toBe("Review whitelist");
    expect(q("[data-testid=title]")?.getAttribute("title")).toBe("Check the list");
    expect(q("[data-testid=icon] svg")).not.toBeNull();
    expect(q("[data-testid=status] [data-slot=agent-state]")?.getAttribute("data-status")).toBe("closed");
  });

  test("calls an unnamed subagent Subagent, and reads its name from the transcript when it is not tracked", async () => {
    await seed({ id: "a1" }, session("idle"));
    await draw();
    expect(q("[data-testid=title]")?.textContent).toBe("Subagent");
    expect(q("[data-testid=title]")?.getAttribute("title")).toBeNull();
    await seed(null, session("idle"), [{ id: "sp", kind: "subagent_spawned", subagent: { id: "a1", name: "Java fixes", task: "Fix it" } }]);
    await draw();
    expect(q("[data-testid=title]")?.textContent).toBe("Java fixes");
    expect(q("[data-testid=title]")?.getAttribute("title")).toBe("Fix it");
  });

  test("follows the subagent's state as the store changes, without the pane being opened again", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    expect(q("[data-slot=agent-state]")?.getAttribute("data-status")).toBe("running");
    expect(stop()).not.toBeNull();
    await act(async () => useAgentSessionsStore.setState({ subagents: { s1: [{ id: "a1", name: "R", state: "failed" }] } }));
    expect(q("[data-slot=agent-state]")?.getAttribute("data-status")).toBe("error");
    expect(stop()).toBeNull();
  });
});

/** Items that count how often they are walked: `findSubagent` iterates them. */
function counted(items: TranscriptItem[]) {
  let walks = 0;
  const proxy = new Proxy(items, {
    get(target, key, receiver) {
      if (key === Symbol.iterator) walks++;
      return Reflect.get(target, key, receiver);
    },
  });
  return { items: proxy as TranscriptItem[], walks: () => walks };
}
const setItems = (items: TranscriptItem[]) => act(async () => useAgentSessionsStore.setState({ transcriptsById: { s1: { items } } as never }));

describe("what the header costs on a streaming session", () => {
  test("with no subagent open, no transcript is walked, however often the store changes", async () => {
    const log = counted([{ id: "t1", kind: "agent_message_chunk", text: "a" } as TranscriptItem]);
    await seed(null, session("running"));
    await setItems(log.items);
    await act(async () => root.render(<ShellRow sessionId="s1" subagentId="" />));
    await setItems(counted([]).items);
    await act(async () => useAgentSessionsStore.setState({ sessionsById: { s1: session("idle") } }));
    await setItems(log.items);
    await act(async () => useAgentSessionsStore.setState({ sessionsById: { s1: session("running") } }));
    expect(log.walks()).toBe(0);
    expect(q("[data-testid=title]")?.textContent).toBe("Subagent");
    expect(stop()).toBeNull();
  });

  test("the three pieces share one walk per change of the transcript, and a store change that leaves it alone walks nothing", async () => {
    const base = [{ id: "sp", kind: "subagent_spawned", subagent: { id: "a1", name: "R" } } as TranscriptItem];
    const first = counted(base);
    await seed(null, session("running"));
    await setItems(first.items);
    await draw();
    expect(first.walks()).toBe(1);
    await act(async () => useAgentSessionsStore.setState({ sessionsById: { s1: session("running", true) } }));
    expect(first.walks()).toBe(1);
    const next = counted([...base, { id: "t2", kind: "agent_message_chunk", text: "x" } as TranscriptItem]);
    await setItems(next.items);
    expect(next.walks()).toBe(1);
  });
});

describe("Stop turn on a subagent", () => {
  test("is offered while the subagent runs and its session is working, and says what it does", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    expect(stop()?.textContent).toBe("Stop turn");
    expect(stop()?.getAttribute("aria-label")).toBe("Stop turn");
  });

  test("tells, in its tooltip, that it stops more than this subagent", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    await act(async () => {
      stop()!.focus();
      await new Promise((resolve) => setTimeout(resolve, 200));
    });
    expect(document.body.textContent).toContain("Stops the whole session's turn, including this subagent and background tasks");
  });

  test("is offered through a turn the agent started itself", async () => {
    await seed({ id: "a1", name: "R", state: "running" }, session("idle", true));
    await draw();
    expect(stop()).not.toBeNull();
  });

  test("is not offered for a finished subagent, even while its session works", async () => {
    for (const state of ["completed", "failed"]) {
      await seed({ id: "a1", name: "R", state }, session("running"));
      await draw();
      expect(stop()).toBeNull();
    }
  });

  test("is not offered while its session is not working: a quiet, failed or closed one has no turn to stop", async () => {
    for (const status of ["idle", "closed", "error"] as const) {
      await seed({ id: "a1", name: "R" }, session(status));
      await draw();
      expect(stop()).toBeNull();
    }
    await seed({ id: "a1", name: "R" }, null);
    await draw();
    expect(stop()).toBeNull();
  });

  test("asks first and cancels nothing until the person confirms", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    await press(stop());
    expect(dialog()).not.toBeNull();
    expect(dialog()!.textContent).toContain("whole");
    expect(cancelled).toEqual([]);
  });

  test("dismissing the question cancels nothing", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    await press(stop());
    await press(buttonNamed(dialog()!, "Cancel"));
    expect(cancelled).toEqual([]);
    expect(dialog()).toBeNull();
  });

  test("confirming cancels the session's turn once, through the one cancel function", async () => {
    await seed({ id: "a1", name: "R" }, session("running"));
    await draw();
    await press(stop());
    await press(buttonNamed(dialog()!, "Stop turn"));
    expect(cancelled).toEqual(["s1"]);
    // The row is read again, so the pane follows the store like everywhere else.
    expect(useAgentSessionsStore.getState().sessionsById.s1.status).toBe("idle");
    expect(stop()).toBeNull();
  });
});

describe("SubagentPanel", () => {
  test("draws no header of its own: the shell's row carries the name, the state and the close", async () => {
    await seed({ id: "a1", name: "Review whitelist", task: "t" }, session("running"), [
      { id: "sp", kind: "subagent_spawned", subagent: { id: "a1", name: "Review whitelist" } },
    ]);
    await act(async () => root.render(<SubagentPanel sessionId="s1" subagentId="a1" />));
    expect(q("[data-testid=subagent-panel]")).not.toBeNull();
    expect(container.querySelector("header")).toBeNull();
    expect(container.querySelector("[aria-label=Close]")).toBeNull();
    expect(container.querySelector("[data-slot=agent-state]")).toBeNull();
    expect(container.textContent).not.toContain("Review whitelist");
  });
});
