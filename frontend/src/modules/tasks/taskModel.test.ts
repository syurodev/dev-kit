import { describe, expect, test } from "bun:test";
import type { Board, Task } from "@/bridge/tasks";
import {
  EMPTY_FILTER,
  UNKNOWN_COLUMN,
  blockerCandidates,
  boardLabels,
  boardSummary,
  columnsOf,
  dependentCandidates,
  dependentsOf,
  dependsOn,
  indexById,
  initials,
  openBlockers,
  parentCandidates,
  priorityLabel,
  priorityRank,
  relativeTime,
  upsert,
  withStatus,
} from "./taskModel";

const board: Board = {
  id: "b1",
  name: "Board",
  keyPrefix: "B",
  color: "",
  workflow: {
    statuses: [
      { id: "todo", name: "To do", category: "open" },
      { id: "doing", name: "Doing", category: "active" },
      { id: "done", name: "Done", category: "terminal" },
    ],
  },
};

function task(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    boardId: "b1",
    key: `B-${id}`,
    title: `Task ${id}`,
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
    createdAt: `2026-01-0${id.length}T00:00:00Z`,
    updatedAt: "",
    ...overrides,
  };
}

describe("columnsOf", () => {
  test("puts top-level tasks in their column, subtasks on their parent only", () => {
    const tasks = [task("a"), task("b", { status: "doing" }), task("c", { parentId: "a" })];
    const columns = columnsOf(board, tasks, EMPTY_FILTER);
    expect(columns.map((column) => column.tasks.map((t) => t.id))).toEqual([["a"], ["b"], []]);
  });

  test("a task whose status the workflow lost gets a column instead of vanishing", () => {
    const columns = columnsOf(board, [task("a", { status: "gone" })], EMPTY_FILTER);
    const last = columns[columns.length - 1];
    expect(last.id).toBe(UNKNOWN_COLUMN);
    expect(last.status).toBeNull();
    expect(last.tasks.map((t) => t.id)).toEqual(["a"]);
  });

  test("no unknown column when every status is known", () => {
    expect(columnsOf(board, [task("a")], EMPTY_FILTER).some((c) => c.id === UNKNOWN_COLUMN)).toBe(false);
  });

  test("sorts a column by priority, then by age", () => {
    const tasks = [
      task("a", { priority: "" }),
      task("bb", { priority: "low" }),
      task("ccc", { priority: "urgent" }),
      task("d", { priority: "urgent" }),
    ];
    expect(columnsOf(board, tasks, EMPTY_FILTER)[0].tasks.map((t) => t.id)).toEqual(["d", "ccc", "bb", "a"]);
  });

  test("a subtask matching the filter keeps its parent's card on the board", () => {
    const tasks = [task("a", { title: "Parent" }), task("b", { title: "refund bug", parentId: "a" }), task("c")];
    const columns = columnsOf(board, tasks, { ...EMPTY_FILTER, query: "refund" });
    expect(columns[0].tasks.map((t) => t.id)).toEqual(["a"]);
  });

  test("filters by priority, 'none', label and agent", () => {
    const tasks = [
      task("a", { priority: "high", labels: ["ui"] }),
      task("b", { sessionId: "s1" }),
      task("c", { labels: ["api"] }),
    ];
    const ids = (filter: Partial<typeof EMPTY_FILTER>) =>
      columnsOf(board, tasks, { ...EMPTY_FILTER, ...filter })[0].tasks.map((t) => t.id).sort();
    expect(ids({ priority: "high" })).toEqual(["a"]);
    expect(ids({ priority: "none" })).toEqual(["b", "c"]);
    expect(ids({ label: "api" })).toEqual(["c"]);
    expect(ids({ heldByAgent: true })).toEqual(["b"]);
  });
});

describe("dependencies", () => {
  // a waits on b, b waits on c.
  const tasks = [task("a", { blockedBy: ["b"] }), task("b", { blockedBy: ["c"] }), task("c"), task("d")];
  const byId = indexById(tasks);

  test("dependsOn follows blocked_by transitively", () => {
    expect(dependsOn(byId, "a", "c")).toBe(true);
    expect(dependsOn(byId, "c", "a")).toBe(false);
    expect(dependsOn(byId, "d", "a")).toBe(false);
  });

  test("dependsOn terminates on a cycle already in the data", () => {
    const looped = indexById([task("x", { blockedBy: ["y"] }), task("y", { blockedBy: ["x"] })]);
    expect(dependsOn(looped, "x", "z")).toBe(false);
  });

  test("a task is never offered a dependency that would loop back to it", () => {
    // c depending on a would close a → b → c → a.
    const offered = blockerCandidates(tasks, byId.get("c")!).map((t) => t.id);
    expect(offered).toEqual(["d"]);
  });

  test("a task already listed or the task itself is not offered", () => {
    expect(blockerCandidates(tasks, byId.get("a")!).map((t) => t.id)).toEqual(["c", "d"]);
  });

  test("dependentsOf is the other side of blocked_by", () => {
    expect(dependentsOf(tasks, "b").map((t) => t.id)).toEqual(["a"]);
    expect(dependentsOf(tasks, "d")).toEqual([]);
  });

  test("a task cannot be made to block something it waits on", () => {
    // a waits on b and (through b) on c, so neither can be made to wait on a.
    const offered = dependentCandidates(tasks, byId.get("a")!).map((t) => t.id);
    expect(offered).toEqual(["d"]);
  });

  test("openBlockers skips finished and missing blockers", () => {
    const withDone = [task("a", { blockedBy: ["b", "c", "gone"] }), task("b", { status: "done" }), task("c")];
    expect(openBlockers(withDone[0], indexById(withDone), board).map((t) => t.id)).toEqual(["c"]);
  });
});

describe("parentCandidates", () => {
  test("top-level tasks other than itself", () => {
    const tasks = [task("a"), task("b"), task("c", { parentId: "a" })];
    expect(parentCandidates(tasks, tasks[1]).map((t) => t.id)).toEqual(["a"]);
  });

  test("nothing for a task that already has subtasks", () => {
    const tasks = [task("a"), task("b"), task("c", { parentId: "a" })];
    expect(parentCandidates(tasks, tasks[0])).toEqual([]);
  });
});

describe("upsert", () => {
  test("replaces in place and never keeps a description", () => {
    const next = upsert([task("a"), task("b")], "b1", task("a", { title: "New", description: "secret" }));
    expect(next.map((t) => t.title)).toEqual(["New", "Task b"]);
    expect("description" in next[0]).toBe(false);
  });

  test("drops an archived task and one moved to another board", () => {
    expect(upsert([task("a")], "b1", task("a", { archivedAt: "now" }))).toEqual([]);
    expect(upsert([task("a")], "b1", task("a", { boardId: "b2" }))).toEqual([]);
  });

  test("appends a new task of this board, ignores one of another", () => {
    expect(upsert([], "b1", task("a")).map((t) => t.id)).toEqual(["a"]);
    expect(upsert([], "b1", task("a", { boardId: "b2" }))).toEqual([]);
  });
});

describe("small helpers", () => {
  test("withStatus moves one task", () => {
    expect(withStatus([task("a"), task("b")], "b", "done").map((t) => t.status)).toEqual(["todo", "done"]);
  });

  test("priority label and rank keep an unknown value", () => {
    expect(priorityLabel("")).toBe("No priority");
    expect(priorityLabel("p0")).toBe("p0");
    expect(priorityRank("urgent")).toBeLessThan(priorityRank("p0"));
    expect(priorityRank("p0")).toBeLessThan(priorityRank(""));
  });

  test("boardLabels is sorted and unique", () => {
    expect(boardLabels([task("a", { labels: ["ui", "api"] }), task("b", { labels: ["ui"] })])).toEqual(["api", "ui"]);
  });

  test("relativeTime", () => {
    const now = Date.parse("2026-09-30T12:00:00Z");
    expect(relativeTime("2026-09-30T11:59:50Z", now)).toBe("just now");
    expect(relativeTime("2026-09-30T11:55:00Z", now)).toBe("5m ago");
    expect(relativeTime("2026-09-30T09:00:00Z", now)).toBe("3h ago");
    expect(relativeTime("2026-09-28T12:00:00Z", now)).toBe("2d ago");
    expect(relativeTime("not a date", now)).toBe("");
  });
});

describe("boardSummary", () => {
  test("counts by category; finished work is neither blocked nor held", () => {
    const tasks = [
      task("a", { status: "doing", blockedBy: ["b"], sessionId: "s1" }),
      task("b"),
      task("c", { status: "done", sessionId: "s2", blockedBy: ["b"] }),
      task("d", { parentId: "b", status: "done" }),
    ];
    expect(boardSummary(board, tasks)).toEqual({ total: 4, done: 2, active: 1, blocked: 1, heldByAgent: 1 });
  });
});

describe("initials", () => {
  test("one word, several words, separators, empty", () => {
    expect(initials("syuro")).toBe("SY");
    expect(initials("Ada Lovelace")).toBe("AL");
    expect(initials("jane.doe")).toBe("JD");
    expect(initials("  ")).toBe("");
  });
});
