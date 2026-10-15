import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { Board, Task } from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { activeDocument, BOARD_VIEW, DETAIL_VIEW, MODULE, openBoard, openTaskDetail } from "./documents";
import { tasksModule } from "./manifest";
import { useTasksStore } from "./tasksStore";
import { TasksSidebar } from "./TasksSidebar";

/**
 * Server-rendered, so no effect runs and nothing reaches the bridge: each
 * test puts the store where it wants it and reads what the rail draws from
 * that — the same smoke GitSidebar.test.tsx does for its rail.
 */

const board: Board = {
  id: "b1",
  name: "Payments board",
  keyPrefix: "PAY",
  color: "blue",
  workspaceId: "w1",
  workflow: {
    statuses: [
      { id: "todo", name: "To do", category: "open" },
      { id: "done", name: "Done", category: "terminal" },
    ],
  },
};

function task(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    boardId: "b1",
    key: "PAY-abc123",
    title: "Refund flow",
    status: "todo",
    priority: "",
    labels: null,
    assignee: "",
    parentId: "",
    blockedBy: null,
    planNodeId: "",
    sessionId: "",
    branch: "",
    archivedAt: "",
    history: null,
    createdAt: "",
    updatedAt: "",
    ...overrides,
  };
}

/** App.tsx mounts the tooltip provider once; a rail rendered alone needs its own. */
function render(): string {
  return renderToString(
    <TooltipProvider>
      <TasksSidebar />
    </TooltipProvider>,
  );
}

/**
 * zustand hands a server render `getInitialState()` rather than the live state
 * (the useSyncExternalStore server snapshot), so a plain setState would never
 * reach the markup. `seed` writes both — the live state, and the initial-state
 * object that snapshot reads — and afterAll puts the initial states back, so
 * production code keeps no SSR branch for a test's sake.
 */
type SeedableStore<T> = { setState: (partial: Partial<T>) => void; getInitialState: () => T };
const restores: (() => void)[] = [];
function seed<T extends object>(store: SeedableStore<T>, partial: Partial<T>): void {
  const initial = store.getInitialState();
  const saved = { ...initial };
  restores.push(() => Object.assign(initial, saved));
  Object.assign(initial, partial);
  store.setState(partial);
}
afterAll(() => {
  while (restores.length) restores.pop()!();
});

const acme = { id: "w1", name: "Acme workspace", root: "/tmp/acme" } as Workspace;

beforeEach(() => {
  seed(useTasksStore, {
    boards: null,
    workspaces: null,
    tasksByBoard: {},
    loadingBoards: {},
    error: "",
    expandedWorkspaces: {},
    expandedBoards: {},
    expandedStatuses: {},
    expandedTasks: {},
  });
  seed(useWorkspaceSwitcherStore, { currentId: null });
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "", recentlyClosed: [] });
});

describe("TasksSidebar", () => {
  test("draws no empty state before the first read answers", () => {
    const html = render();
    expect(html).toContain("Search tasks");
    expect(html).not.toContain("No boards yet");
    expect(html).not.toContain('role="treeitem"');
  });

  test("with nothing at all it explains, and New board says a workspace comes first", () => {
    seed(useTasksStore, { boards: [], workspaces: [] });
    const html = render();
    expect(html).toContain("No boards yet");
    expect(html).toContain("title bar");
  });

  test("groups boards under their workspace, and the rest under Not linked", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w1" });
    seed(useTasksStore, {
      boards: [board, { ...board, id: "b2", name: "Stray board", workspaceId: "" }],
      workspaces: [acme],
      expandedWorkspaces: { unlinked: true },
    });
    const html = render();
    expect(html).toContain("Acme workspace");
    expect(html).toContain("Payments board");
    expect(html).toContain("Not linked to a workspace");
    expect(html).toContain("Stray board");
  });

  test("a task row carries its key, who holds it, its priority and its subtasks' progress", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w1" });
    seed(useTasksStore, {
      boards: [board],
      workspaces: [acme],
      expandedBoards: { b1: true },
      tasksByBoard: {
        b1: [
          task({ sessionId: "s1", priority: "urgent" }),
          task({ id: "t2", key: "PAY-def456", title: "Sub one", parentId: "t1", status: "done" }),
          task({ id: "t3", key: "PAY-0f0f0f", title: "Sub two", parentId: "t1" }),
        ],
      },
    });
    const html = render();
    expect(html).toContain("PAY-abc123");
    expect(html).toContain("Refund flow");
    expect(html).toContain("Held by an agent");
    expect(html).toContain("urgent");
    expect(html).toMatch(/1<!-- -->\/<!-- -->2/);
  });

  test("is the module's rail, so it is there with no tab open", () => {
    expect(tasksModule.sidebar).toBeDefined();
  });
});

describe("board documents", () => {
  test("one tab per board, and opening it again focuses it", () => {
    openBoard("b1", "Payments");
    openBoard("b2", "Ops", { preview: false });
    openBoard("b1", "Payments", { preview: false });
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE);
    expect(tabs.openTabs.filter((tab) => tab.viewId === BOARD_VIEW)).toHaveLength(2);
    expect(activeDocument(tabs)).toEqual({ boardId: "b1", taskId: "" });
  });

  test("a task tab in front names the task, not a board", () => {
    openTaskDetail("t9", "PAY-t9", { preview: false });
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, MODULE);
    expect(tabs.openTabs[0].viewId).toBe(DETAIL_VIEW);
    expect(activeDocument(tabs)).toEqual({ boardId: "", taskId: "t9" });
  });
});
