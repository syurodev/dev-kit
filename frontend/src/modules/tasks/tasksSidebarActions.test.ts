import { describe, expect, test } from "bun:test";
import type { Board, Task } from "@/bridge/tasks";
import type { Workspace } from "@/bridge/workspace";
import {
  boardActions,
  boardOfTask,
  isUrgentPriority,
  newBoardBlockedReason,
  newTaskTarget,
  sidebarBody,
  suggestKeyPrefix,
  taskActions,
  workspaceActions,
  type SidebarAction,
} from "./tasksSidebarActions";

function board(overrides: Partial<Board> = {}): Board {
  return {
    id: "b1",
    name: "DevKit",
    keyPrefix: "DEV",
    color: "blue",
    workflow: { statuses: [{ id: "todo", name: "To do", category: "open" }] },
    ...overrides,
  };
}

function workspace(id: string, name = id): Workspace {
  return { id, name, root: `/tmp/${id}` } as Workspace;
}

/** Labels in order, a separator as "—" and a submenu as "label ▸ [items]". */
function labels(actions: SidebarAction[]): string[] {
  return actions.map((action) => {
    if (action.kind === "separator") return "—";
    if (action.kind === "submenu") return `${action.label} ▸ ${labels(action.items).join(", ")}`;
    return action.label;
  });
}

function item(actions: SidebarAction[], id: string) {
  for (const action of actions) {
    if (action.kind === "item" && action.id === id) return action;
  }
  return undefined;
}

describe("New board", () => {
  test("is off, and says why, with no workspace open", () => {
    expect(newBoardBlockedReason(null, [workspace("w1")])).toContain("workspace");
  });

  test("is on in the current workspace", () => {
    expect(newBoardBlockedReason("w1", [workspace("w1")])).toBe("");
  });

  // A board bound to a removed workspace would be refused by the service —
  // the button must not offer it.
  test("is off when the current workspace no longer exists", () => {
    expect(newBoardBlockedReason("gone", [workspace("w1")])).not.toBe("");
  });

  test("trusts the current id before the list has loaded", () => {
    expect(newBoardBlockedReason("w1", null)).toBe("");
  });
});

describe("New task's target", () => {
  const boards = [board({ id: "a", name: "A" }), board({ id: "b", name: "B" })];

  test("the first candidate that is still a board wins", () => {
    expect(newTaskTarget(boards, ["", "b", "a"])?.name).toBe("B");
  });

  test("a deleted board is skipped, not trusted", () => {
    expect(newTaskTarget(boards, ["deleted", "a"])?.name).toBe("A");
  });

  test("none when nothing is chosen or boards have not loaded", () => {
    expect(newTaskTarget(boards, ["", ""])).toBeNull();
    expect(newTaskTarget(null, ["a"])).toBeNull();
  });

  test("a task tab places its board through the loaded tasks", () => {
    const tasks = { a: [{ id: "t1" } as Task], b: [{ id: "t2" } as Task] };
    expect(boardOfTask(tasks, "t2")).toBe("b");
    expect(boardOfTask(tasks, "unknown")).toBe("");
    expect(boardOfTask(tasks, "")).toBe("");
  });
});

describe("a workspace's menu", () => {
  test("is New board", () => {
    expect(labels(workspaceActions())).toEqual(["New board"]);
  });
});

describe("a board's menu", () => {
  const workspaces = [workspace("w1", "Acme"), workspace("w2", "Beta")];

  test("a linked board can make tasks from the plan, move and unlink", () => {
    const actions = boardActions(board({ workspaceId: "w1" }), { currentWorkspaceId: "w1", workspaces });
    expect(labels(actions)).toEqual([
      "Open",
      "New task",
      "—",
      "Verify settings…",
      "Create tasks from plan",
      "—",
      "Move to workspace ▸ Acme, Beta",
      "Unlink from workspace",
    ]);
    // Moving to where it already is, is not a move.
    const move = actions.find((action) => action.kind === "submenu");
    expect(move?.kind === "submenu" && item(move.items, "board.linkTo")?.disabled).toBe(true);
  });

  test("an unlinked board offers linking instead, and no plan", () => {
    const actions = boardActions(board(), { currentWorkspaceId: "w2", workspaces });
    const shown = labels(actions);
    expect(shown).toContain("Link to current workspace");
    expect(shown).toContain("Link to workspace ▸ Acme, Beta");
    expect(shown).not.toContain("Create tasks from plan");
    expect(shown).not.toContain("Unlink from workspace");
    expect(item(actions, "board.linkCurrent")?.disabled).toBe(false);
  });

  test("linking to the current workspace is off when there is none", () => {
    const actions = boardActions(board(), { currentWorkspaceId: null, workspaces });
    expect(item(actions, "board.linkCurrent")?.disabled).toBe(true);
  });

  // Bound to a workspace this machine no longer has: the tree puts it in the
  // unlinked group, and its menu must agree with where it is drawn.
  test("a board bound to a removed workspace is treated as unlinked", () => {
    const actions = boardActions(board({ workspaceId: "gone" }), { currentWorkspaceId: "w1", workspaces });
    expect(labels(actions)).toContain("Link to current workspace");
  });

  test("with one workspace there is nowhere to move to", () => {
    const actions = boardActions(board({ workspaceId: "w1" }), {
      currentWorkspaceId: "w1",
      workspaces: [workspace("w1")],
    });
    expect(labels(actions).some((label) => label.startsWith("Move to workspace"))).toBe(false);
  });
});

describe("a task's menu", () => {
  test("opens, opens beside, copies and archives — and never deletes", () => {
    const shown = labels(taskActions({ archivedAt: "" }));
    expect(shown).toEqual(["Open", "Open beside", "Copy key", "—", "Archive"]);
    expect(shown.some((label) => /delete/i.test(label))).toBe(false);
  });

  test("an archived task cannot be archived again", () => {
    expect(item(taskActions({ archivedAt: "2026-09-26T00:00:00Z" }), "task.archive")?.disabled).toBe(true);
  });
});

describe("priority", () => {
  test("only the words that mean drop everything are urgent", () => {
    expect(isUrgentPriority("Urgent")).toBe(true);
    expect(isUrgentPriority(" p0 ")).toBe(true);
    expect(isUrgentPriority("high")).toBe(false);
    expect(isUrgentPriority("")).toBe(false);
  });
});

describe("the suggested key prefix", () => {
  test("initials for several words, the first letters for one", () => {
    expect(suggestKeyPrefix("Dev Kit App")).toBe("DKA");
    expect(suggestKeyPrefix("payments")).toBe("PAY");
    expect(suggestKeyPrefix("  ")).toBe("");
  });
});

describe("what the rail's body shows", () => {
  test("nothing while the first read is in flight", () => {
    expect(sidebarBody({ boards: null, workspaces: null, treeLength: 0, needle: "" })).toBe("hidden");
  });

  test("the empty state with no boards and no workspaces", () => {
    expect(sidebarBody({ boards: [], workspaces: [], treeLength: 0, needle: "" })).toBe("empty");
  });

  test("the tree when a workspace exists, even with no boards", () => {
    expect(sidebarBody({ boards: [], workspaces: [workspace("w1")], treeLength: 1, needle: "" })).toBe("tree");
  });

  test("no match when a search emptied the tree", () => {
    expect(sidebarBody({ boards: [board()], workspaces: [], treeLength: 0, needle: "zzz" })).toBe("noMatch");
  });
});
