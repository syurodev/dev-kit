import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import type { Board, Task } from "@/bridge/tasks";
import { SplitPanelProvider } from "@/components/ui/split-panel";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { useTasksStore } from "./tasksStore";
import { TasksBoardView } from "./TasksBoardView";

/**
 * Server-rendered, so no effect runs and nothing reaches the bridge — the
 * same approach TasksSidebar.test.tsx takes for the rail this view sits
 * beside. Each test seeds tasksStore/workspaceSwitcher directly and reads
 * what the view draws from that render alone.
 */

function board(overrides: Partial<Board> = {}): Board {
  return {
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
    ...overrides,
  };
}

function task(overrides: Partial<Task> = {}): Task {
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

/** Same seed helper TasksSidebar.test.tsx uses: writes both the live state and
 * the initial-state object the SSR snapshot reads, and restores it after. */
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

function render(params?: Record<string, unknown>): string {
  // The board opens a task in the module's right-hand pane, which the shell
  // provides; the provider is all a server render needs of it.
  return renderToString(
    <SplitPanelProvider>
      <TasksBoardView params={params} />
    </SplitPanelProvider>,
  );
}

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
    boardViews: {},
  });
  seed(useWorkspaceSwitcherStore, { currentId: null });
});

describe("TasksBoardView", () => {
  test("draws nothing before boards have loaded", () => {
    expect(render()).toBe("");
  });

  test("opensHome with no current workspace points at the sidebar", () => {
    seed(useTasksStore, { boards: [board()] });
    const html = render();
    expect(html).toContain("No board here");
    expect(html).toContain("Tasks sidebar");
  });

  test("opensHome with boards but none in the current workspace points at the sidebar", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w2" });
    seed(useTasksStore, { boards: [board({ workspaceId: "w1" })] });
    const html = render();
    expect(html).toContain("This workspace has no board yet");
  });

  test("an unknown boardId reads as gone, not as the workspace's home board", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w1" });
    seed(useTasksStore, { boards: [board()] });
    const html = render({ boardId: "does-not-exist" });
    expect(html).toContain("This board is gone");
  });

  test("opensHome picks the current workspace's first board by name", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w1" });
    seed(useTasksStore, {
      boards: [board({ id: "b2", name: "Zeta board" }), board({ id: "b1", name: "Alpha board" })],
      tasksByBoard: { b1: [] },
    });
    const html = render();
    expect(html).toContain("Alpha board");
  });

  test("an explicit boardId wins over the workspace's home board", () => {
    seed(useWorkspaceSwitcherStore, { currentId: "w1" });
    seed(useTasksStore, {
      boards: [board({ id: "b1", name: "Alpha board" }), board({ id: "b2", name: "Zeta board" })],
      tasksByBoard: { b2: [] },
    });
    const html = render({ boardId: "b2" });
    expect(html).toContain("Zeta board");
    expect(html).not.toContain("Alpha board");
  });

  test("From plan is offered only for a board bound to a workspace", () => {
    seed(useTasksStore, {
      boards: [board({ workspaceId: "" })],
      tasksByBoard: { b1: [] },
    });
    const html = render({ boardId: "b1" });
    expect(html).not.toContain("From plan");
  });

  test("From plan shows once the board is bound", () => {
    seed(useTasksStore, {
      boards: [board({ workspaceId: "w1" })],
      tasksByBoard: { b1: [] },
    });
    const html = render({ boardId: "b1" });
    expect(html).toContain("From plan");
  });

  test("draws the board's columns and tasks once loaded", () => {
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: { b1: [task()] },
    });
    const html = render({ boardId: "b1" });
    expect(html).toContain("Payments board");
    expect(html).toContain("PAY-abc123");
    expect(html).toContain("Refund flow");
    expect(html).toContain("To do");
    expect(html).toContain("Done");
  });

  test("a task whose column is gone is still on the board", () => {
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: { b1: [task({ status: "removed-column" })] },
    });
    const html = render({ boardId: "b1" });
    expect(html).toContain("No column");
    expect(html).toContain("Refund flow");
  });

  test("a card waiting on an unfinished task says so; one waiting on a finished task does not", () => {
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: {
        b1: [
          task({ id: "t1", blockedBy: ["t2"] }),
          task({ id: "t2", key: "PAY-def456", title: "Ledger", status: "todo" }),
        ],
      },
    });
    expect(render({ boardId: "b1" })).toContain("Waiting on unfinished tasks");
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: {
        b1: [
          task({ id: "t1", blockedBy: ["t2"] }),
          task({ id: "t2", key: "PAY-def456", title: "Ledger", status: "done" }),
        ],
      },
    });
    expect(render({ boardId: "b1" })).not.toContain("Waiting on unfinished tasks");
  });

  test("New task opens a dialog rather than creating a placeholder card", () => {
    seed(useTasksStore, { boards: [board()], tasksByBoard: { b1: [] } });
    const html = render({ boardId: "b1" });
    expect(html).toContain("New task");
    expect(html).toContain("Filter tasks");
  });

  test("a card shows its task in the pane rather than opening a tab", () => {
    seed(useTasksStore, { boards: [board()], tasksByBoard: { b1: [task()] } });
    const html = render({ boardId: "b1" });
    expect(html).toContain("PAY-abc123 Refund flow — show details");
    // Nothing is picked until someone picks it.
    expect(html).not.toContain('aria-current="true"');
  });

  test("the header sums the board up, leaving out what is zero", () => {
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: {
        b1: [task({ id: "t1", blockedBy: ["t2"] }), task({ id: "t2", key: "PAY-def456", status: "done" }), task({ id: "t3", key: "PAY-ghi789", blockedBy: ["t1"] })],
      },
    });
    // NumberFlow's declarative shadow DOM has a fallback number in light DOM.
    // Keep that visible text, excluding inert templates and embedded CSS.
    const html = render({ boardId: "b1" })
      .replace(/<template\b[^>]*>[\s\S]*?<\/template\s*>/g, "")
      .replace(/<style\b[^>]*>[\s\S]*?<\/style\s*>/g, "")
      .replace(/<[^>]+>/g, "");
    expect(html).toContain("1/3");
    expect(html).toContain("1 waiting");
    expect(html).not.toContain("in progress");
    expect(html).not.toContain("with agents");
  });

  test("a column with work offers to add a task in place", () => {
    seed(useTasksStore, { boards: [board()], tasksByBoard: { b1: [task()] } });
    const html = render({ boardId: "b1" });
    // To do is open and full width; the empty Done is folded and offers none.
    expect(html.match(/Add task/g)?.length).toBe(1);
  });

  test("an empty finished column is a narrow strip that can be expanded", () => {
    seed(useTasksStore, { boards: [board()], tasksByBoard: { b1: [task()] } });
    const html = render({ boardId: "b1" });
    expect(html).toContain('aria-label="Expand Done"');
    expect(html).not.toContain("Nothing finished yet");
  });

  test("an empty open column keeps its full width", () => {
    seed(useTasksStore, { boards: [board()], tasksByBoard: { b1: [task({ status: "done" })] } });
    const html = render({ boardId: "b1" });
    expect(html).not.toContain('aria-label="Expand To do"');
    expect(html).toContain("No tasks");
  });

  test("a board remembered as a list draws rows, finished groups folded", () => {
    seed(useTasksStore, {
      boards: [board()],
      tasksByBoard: { b1: [task(), task({ id: "t2", key: "PAY-def456", title: "Shipped thing", status: "done" })] },
      boardViews: { b1: "list" },
    });
    const html = render({ boardId: "b1" });
    expect(html).toContain("Refund flow");
    expect(html).not.toContain("Shipped thing");
    expect(html).not.toContain("Add task");
  });
});
