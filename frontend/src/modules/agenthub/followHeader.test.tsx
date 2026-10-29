import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { AgentSession } from "@/bridge/agenthub";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { AgentActivityPanel, useActivityHeader } from "./follow";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let stopped = 0;

/** What the shell does with a pane's header, row for row (components/ui/pane-slots.tsx). */
function ShellRow({ title }: { title: string }) {
  const header = useActivityHeader("s1", title, () => void stopped++);
  return (
    <TooltipProvider>
      <div>
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
const seed = (s: AgentSession | null) => act(async () => useAgentSessionsStore.setState({ sessionsById: s ? { s1: s } : {}, transcriptsById: {} as never }));

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  stopped = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  useAgentSessionsStore.setState({ sessionsById: {}, transcriptsById: {} as never });
});

const draw = (title = "Fix login") => act(async () => root.render(<ShellRow title={title} />));
const q = (selector: string) => container.querySelector<HTMLElement>(selector);
const statusOf = () => q("[data-testid=status] [data-slot=agent-state]")?.getAttribute("data-status");

describe("the follow feed's header", () => {
  test("is the session's title, with the session's state and the stop-following control", async () => {
    await seed(session("idle"));
    await draw();
    expect(q("[data-testid=title]")?.textContent).toBe("Fix login");
    expect(q("[data-testid=icon] svg")).not.toBeNull();
    expect(statusOf()).toBe("idle");
    const action = q("[data-testid=actions] button");
    expect(action?.getAttribute("aria-label")).toBe("Stop following");
    await act(async () => action!.click());
    expect(stopped).toBe(1);
  });

  test("reads the state as the sidebar card does: an idle session in a turn of its own is running", async () => {
    await seed(session("idle", true));
    await draw();
    expect(statusOf()).toBe("running");
    await seed(session("awaiting_permission"));
    expect(statusOf()).toBe("awaiting");
    await seed(session("error"));
    expect(statusOf()).toBe("error");
  });

  test("draws no state for a session the store does not know, and names an untitled one", async () => {
    await seed(null);
    await draw("");
    expect(statusOf()).toBeUndefined();
    expect(q("[data-testid=title]")?.textContent).toBe("Session");
  });
});

describe("AgentActivityPanel", () => {
  test("draws no header of its own: no title row, no second stop-following or close", async () => {
    await seed(session("idle"));
    await act(async () => root.render(<AgentActivityPanel sessionId="s1" />));
    expect(q("[data-testid=agent-activity-panel]")).not.toBeNull();
    expect(container.querySelector("header")).toBeNull();
    expect(container.querySelector("button")).toBeNull();
    expect(container.textContent).not.toContain("Following");
    expect(container.textContent).toContain("Nothing changed yet");
  });
});
