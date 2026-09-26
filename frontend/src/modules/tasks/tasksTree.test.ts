import { describe, expect, it } from "bun:test";
import {
  buildTasksTree,
  createBoardChangeDebouncer,
  matchesBoardName,
  matchesTask,
  matchesWorkspaceName,
  UNLINKED_GROUP_ID,
  type TasksTreeCallbacks,
  type TasksTreeInput,
} from "./tasksTree";
import type { Board, Task } from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";
import type { TreeNodeData } from "@/components/tree-view";

const tick = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

function board(overrides: Partial<Board> = {}): Board {
  return {
    id: "board-1",
    name: "DevKit",
    keyPrefix: "DEV",
    color: "blue",
    workflow: {
      statuses: [
        { id: "todo", name: "To do", category: "open" },
        { id: "doing", name: "Doing", category: "active" },
        { id: "done", name: "Done", category: "terminal" },
      ],
    },
    ...overrides,
  };
}

function workspace(overrides: Partial<Workspace> = {}): Workspace {
  return { id: "ws-1", name: "DevKit workspace", root: "/tmp/devkit", ...overrides };
}

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: "task-1",
    boardId: "board-1",
    key: "DEV-1",
    title: "Untitled",
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

/** A no-op set of callbacks for tests that only inspect the tree's shape. */
function noopCallbacks(): TasksTreeCallbacks {
  return {
    toggleWorkspace: () => {},
    toggleBoard: () => {},
    toggleStatus: () => {},
    toggleTask: () => {},
  };
}

function baseInput(overrides: Partial<TasksTreeInput> = {}): TasksTreeInput {
  return {
    boards: [],
    workspaces: [],
    currentWorkspaceId: null,
    tasksByBoard: {},
    loadingBoards: {},
    expandedWorkspaces: {},
    expandedBoards: {},
    expandedStatuses: {},
    expandedTasks: {},
    callbacks: noopCallbacks(),
    ...overrides,
  };
}

function findNode(nodes: TreeNodeData[], id: string): TreeNodeData | undefined {
  for (const node of nodes) {
    if (node.id === id) return node;
    const found = node.children && findNode(node.children, id);
    if (found) return found;
  }
  return undefined;
}

describe("buildTasksTree — status order", () => {
  it("lists statuses in the board's own workflow order, not alphabetically or by id", () => {
    const b = board();
    const ws = workspace();
    b.workspaceId = ws.id;
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        workspaces: [ws],
        currentWorkspaceId: ws.id,
        tasksByBoard: { [b.id]: [] },
        expandedBoards: { [b.id]: true },
      }),
    );
    const boardNode = findNode(tree, `board:${b.id}`)!;
    expect(boardNode.children?.map((n) => n.id)).toEqual([
      `status:${b.id}:todo`,
      `status:${b.id}:doing`,
      `status:${b.id}:done`,
    ]);
  });
});

describe("buildTasksTree — subtasks", () => {
  it("nests a subtask only under its parent, never as its own status-level row", () => {
    const b = board();
    const parent = task({ id: "p1", key: "DEV-1", status: "todo" });
    const child = task({ id: "c1", key: "DEV-2", status: "todo", parentId: "p1" });
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { [b.id]: [parent, child] },
        expandedBoards: { [b.id]: true },
      }),
    );
    const statusNode = findNode(tree, `status:${b.id}:todo`)!;
    // Only the parent is a direct child of the status — the subtask does not
    // also show up as a sibling column entry.
    expect(statusNode.children?.map((n) => n.id)).toEqual(["task:p1"]);
    const parentNode = statusNode.children![0];
    expect(parentNode.children?.map((n) => n.id)).toEqual(["task:c1"]);
  });

  it("never recurses past one level of subtask", () => {
    const b = board();
    const grandparent = task({ id: "gp", key: "DEV-1", status: "todo" });
    const parent = task({ id: "p", key: "DEV-2", status: "todo", parentId: "gp" });
    const grandchild = task({ id: "gc", key: "DEV-3", status: "todo", parentId: "p" });
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { [b.id]: [grandparent, parent, grandchild] },
        expandedBoards: { [b.id]: true },
      }),
    );
    const gpNode = findNode(tree, "task:gp")!;
    const pNode = gpNode.children!.find((n) => n.id === "task:p")!;
    // A second level exists (parent under grandparent)...
    expect(pNode).toBeDefined();
    // ...but nothing was ever asked to look for grandchildren of it: the
    // grandchild is simply not in the tree at all, one level short of found.
    expect(findNode(tree, "task:gc")).toBeUndefined();
  });
});

describe("buildTasksTree — workspace ordering", () => {
  it("sorts the current workspace first, keeping the rest in the order given", () => {
    const wsA = workspace({ id: "a", name: "Alpha" });
    const wsB = workspace({ id: "b", name: "Bravo" });
    const wsC = workspace({ id: "c", name: "Charlie" });
    const tree = buildTasksTree(
      baseInput({
        workspaces: [wsA, wsB, wsC],
        currentWorkspaceId: "c",
        boards: [board({ id: "bd", workspaceId: "c" })],
        tasksByBoard: { bd: [] },
      }),
    );
    expect(tree.map((n) => n.id)).toEqual(["ws:c", "ws:a", "ws:b"]);
  });
});

describe("buildTasksTree — unlinked boards", () => {
  it("groups a board with no workspace binding under 'Not linked to a workspace'", () => {
    const b = board({ id: "orphan", workspaceId: "" });
    const tree = buildTasksTree(baseInput({ boards: [b], tasksByBoard: { orphan: [] } }));
    expect(tree.map((n) => n.id)).toEqual([UNLINKED_GROUP_ID]);
    expect(findNode(tree, `board:${b.id}`)).toBeDefined();
  });

  it("also unlinks a board whose workspaceId points at a workspace no longer in the list", () => {
    const b = board({ id: "dangling", workspaceId: "deleted-ws" });
    const tree = buildTasksTree(baseInput({ boards: [b], workspaces: [], tasksByBoard: { dangling: [] } }));
    expect(tree.map((n) => n.id)).toEqual([UNLINKED_GROUP_ID]);
  });

  it("omits the group entirely when every board is bound", () => {
    const ws = workspace();
    const b = board({ workspaceId: ws.id });
    const tree = buildTasksTree(
      baseInput({ boards: [b], workspaces: [ws], tasksByBoard: { [b.id]: [] } }),
    );
    expect(tree.some((n) => n.id === UNLINKED_GROUP_ID)).toBe(false);
  });
});

describe("buildTasksTree — status default fold", () => {
  it("collapses a terminal status by default but leaves it openable", () => {
    const b = board();
    const toggled: [string, string, boolean][] = [];
    const callbacks: TasksTreeCallbacks = {
      ...noopCallbacks(),
      toggleStatus: (boardId, statusId, expanded) => {
        toggled.push([boardId, statusId, expanded]);
      },
    };
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { [b.id]: [] },
        expandedBoards: { [b.id]: true },
        callbacks,
      }),
    );
    const todoNode = findNode(tree, `status:${b.id}:todo`)!;
    const doneNode = findNode(tree, `status:${b.id}:done`)!;
    expect(todoNode.expanded).toBe(true);
    expect(doneNode.expanded).toBe(false);
    // Openable: it still has a chevron, and toggling calls back with what it
    // was actually showing (false) so the store can flip it correctly.
    expect(doneNode.expandable).toBe(true);
    doneNode.onToggle?.();
    expect(toggled).toEqual([[b.id, "done", false]]);
  });

  it("respects an explicit fold over the terminal default", () => {
    const b = board();
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { [b.id]: [] },
        expandedBoards: { [b.id]: true },
        expandedStatuses: { [`${b.id}:done`]: true },
      }),
    );
    expect(findNode(tree, `status:${b.id}:done`)!.expanded).toBe(true);
  });
});

describe("buildTasksTree — archived tasks", () => {
  it("excludes an archived task even though the caller did not filter it out", () => {
    const b = board();
    const live = task({ id: "live", key: "DEV-1" });
    const archived = task({ id: "gone", key: "DEV-2", archivedAt: "2026-01-01T00:00:00Z" });
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { [b.id]: [live, archived] },
        expandedBoards: { [b.id]: true },
      }),
    );
    expect(findNode(tree, "task:live")).toBeDefined();
    expect(findNode(tree, "task:gone")).toBeUndefined();
    // The board's own open count does not include it either.
    expect(findNode(tree, `board:${b.id}`)!.count).toBe(1);
  });
});

describe("buildTasksTree — search", () => {
  it("keeps only the branch containing a match and forces every ancestor open", () => {
    const b = board({ id: "bd", name: "DevKit" });
    const hit = task({ id: "hit", boardId: "bd", key: "DEV-9", title: "Fix the flaky login test", status: "doing" });
    const miss = task({ id: "miss", boardId: "bd", key: "DEV-8", title: "Unrelated", status: "todo" });
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        tasksByBoard: { bd: [hit, miss] },
        // Nothing pre-expanded: a hit four levels down that stays folded is
        // the same as no hit, so the search alone must force the path open.
        needle: "flaky",
      }),
    );
    const boardNode = findNode(tree, "board:bd")!;
    expect(boardNode.expanded).toBe(true);
    const statusNode = findNode(tree, "status:bd:doing")!;
    expect(statusNode.expanded).toBe(true);
    expect(statusNode.children?.map((n) => n.id)).toEqual(["task:hit"]);
    // The non-matching column and the non-matching task are gone outright.
    expect(findNode(tree, "status:bd:todo")).toBeUndefined();
    expect(findNode(tree, "task:miss")).toBeUndefined();
  });

  it("keeps a task as a match ancestor when only its subtask matches, forcing it open", () => {
    const b = board({ id: "bd" });
    const parent = task({ id: "p", boardId: "bd", key: "DEV-1", title: "Ordinary parent", status: "todo" });
    const child = task({ id: "c", boardId: "bd", key: "DEV-2", title: "special sauce", status: "todo", parentId: "p" });
    const tree = buildTasksTree(
      baseInput({ boards: [b], tasksByBoard: { bd: [parent, child] }, needle: "sauce" }),
    );
    const parentNode = findNode(tree, "task:p")!;
    expect(parentNode.expanded).toBe(true);
    expect(parentNode.children?.map((n) => n.id)).toEqual(["task:c"]);
  });

  it("matches a board by name and forces it open even with no matching task", () => {
    const b = board({ id: "bd", name: "Billing" });
    const t = task({ id: "t1", boardId: "bd", key: "DEV-1", title: "Nothing related" });
    const tree = buildTasksTree(
      baseInput({ boards: [b], tasksByBoard: { bd: [t] }, needle: "billing" }),
    );
    const boardNode = findNode(tree, "board:bd")!;
    expect(boardNode).toBeDefined();
    expect(boardNode.expanded).toBe(true);
    // The board matched by name only — its tasks don't, so none show.
    expect(boardNode.children).toEqual([]);
  });

  it("drops a board whose name and tasks both miss entirely", () => {
    const b = board({ id: "bd", name: "Billing" });
    const t = task({ id: "t1", boardId: "bd", key: "DEV-1", title: "Nothing related" });
    const tree = buildTasksTree(
      baseInput({ boards: [b], tasksByBoard: { bd: [t] }, needle: "nowhere-to-be-found" }),
    );
    expect(findNode(tree, "board:bd")).toBeUndefined();
  });

  it("forces the matching workspace group open and drops a non-matching, boardless one", () => {
    const wsHit = workspace({ id: "hit-ws", name: "Payments" });
    const wsMiss = workspace({ id: "miss-ws", name: "Marketing" });
    const b = board({ id: "bd", name: "General", workspaceId: "hit-ws" });
    const t = task({ id: "t1", boardId: "bd", key: "DEV-1", title: "Refund flow" });
    const tree = buildTasksTree(
      baseInput({
        boards: [b],
        workspaces: [wsHit, wsMiss],
        tasksByBoard: { bd: [t] },
        needle: "refund",
      }),
    );
    expect(tree.map((n) => n.id)).toEqual(["ws:hit-ws"]);
    expect(findNode(tree, "ws:hit-ws")!.expanded).toBe(true);
  });
});

describe("buildTasksTree — an unfetched board", () => {
  it("shows loading and can only be matched by its own name", () => {
    const matching = board({ id: "match-me", name: "Onboarding" });
    const notMatching = board({ id: "no-match", name: "Something else entirely" });
    const tree = buildTasksTree(
      baseInput({
        boards: [matching, notMatching],
        tasksByBoard: {}, // neither board has been fetched
        needle: "onboard",
      }),
    );
    const matchNode = findNode(tree, "board:match-me")!;
    expect(matchNode).toBeDefined();
    expect(matchNode.loading).toBe(true);
    expect(matchNode.children).toBeUndefined();
    expect(matchNode.count).toBeUndefined();
    expect(findNode(tree, "board:no-match")).toBeUndefined();
  });

  it("is expandable and loading even with no search, so opening it can trigger the fetch", () => {
    const b = board();
    const tree = buildTasksTree(baseInput({ boards: [b], tasksByBoard: {} }));
    const node = findNode(tree, `board:${b.id}`)!;
    expect(node.expandable).toBe(true);
    expect(node.loading).toBe(true);
  });
});

describe("matching helpers", () => {
  it("matchesTask checks key, title and labels", () => {
    const t = task({ key: "DEV-42", title: "Ship the widget", labels: ["urgent"] });
    expect(matchesTask(t, "")).toBe(true);
    expect(matchesTask(t, "dev-42")).toBe(true);
    expect(matchesTask(t, "widget")).toBe(true);
    expect(matchesTask(t, "URGENT")).toBe(true);
    expect(matchesTask(t, "nope")).toBe(false);
  });

  it("matchesBoardName and matchesWorkspaceName are case-insensitive substring matches", () => {
    expect(matchesBoardName({ name: "DevKit" }, "kit")).toBe(true);
    expect(matchesBoardName({ name: "DevKit" }, "nope")).toBe(false);
    expect(matchesWorkspaceName({ name: "Payments" }, "PAY")).toBe(true);
  });
});

describe("createBoardChangeDebouncer", () => {
  it("coalesces a burst of triggers for one board into a single fire", async () => {
    const fired: string[] = [];
    const debouncer = createBoardChangeDebouncer((boardId) => fired.push(boardId), 10);
    debouncer.trigger("board-1");
    debouncer.trigger("board-1");
    debouncer.trigger("board-1");
    await tick(30);
    expect(fired).toEqual(["board-1"]);
  });

  it("keeps separate boards on separate timers, so one board's burst doesn't delay another's", async () => {
    const fired: string[] = [];
    const debouncer = createBoardChangeDebouncer((boardId) => fired.push(boardId), 10);
    debouncer.trigger("board-1");
    debouncer.trigger("board-2");
    await tick(30);
    expect(fired.sort()).toEqual(["board-1", "board-2"]);
  });

  it("restarts the window on every trigger for the same board", async () => {
    const fired: string[] = [];
    const debouncer = createBoardChangeDebouncer((boardId) => fired.push(boardId), 20);
    debouncer.trigger("board-1");
    await tick(10);
    debouncer.trigger("board-1"); // resets the 20ms window
    await tick(15);
    // 25ms since the first trigger, but only 15ms since the second — too soon.
    expect(fired).toEqual([]);
    await tick(15);
    expect(fired).toEqual(["board-1"]);
  });
});
