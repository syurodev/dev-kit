import { describe, expect, it } from "bun:test";
import { ORPHAN_COLUMN, columnsOf, dropTarget } from "./board";
import type { Board, Task } from "@/bridge/tasks";

const BOARD: Board = {
  id: "b1",
  name: "DevKit",
  keyPrefix: "DEV",
  color: "blue",
  workflow: {
    statuses: [
      { id: "todo", name: "To do", category: "open" },
      { id: "doing", name: "Doing", category: "active" },
      { id: "done", name: "Đã ship", category: "terminal" },
    ],
  },
};

function task(over: Partial<Task>): Task {
  return {
    id: "t", boardId: "b1", key: "DEV-1", title: "A task", status: "todo",
    priority: "normal", labels: null, assignee: "", parentId: "", blockedBy: null,
    planNodeId: "", sessionId: "", branch: "", archivedAt: "", history: null,
    createdAt: "", updatedAt: "", ...over,
  };
}

describe("columnsOf", () => {
  it("keeps the workflow's own order, not the tasks'", () => {
    const columns = columnsOf(BOARD, [
      task({ id: "1", status: "done" }),
      task({ id: "2", status: "todo" }),
    ]);
    expect(columns.map((column) => column.id)).toEqual(["todo", "doing", "done"]);
    expect(columns[1].tasks).toHaveLength(0);
  });

  it("leaves subtasks to their parent's card", () => {
    const columns = columnsOf(BOARD, [
      task({ id: "parent", status: "todo" }),
      task({ id: "child", status: "todo", parentId: "parent" }),
    ]);
    expect(columns[0].tasks.map((t) => t.id)).toEqual(["parent"]);
  });

  it("surfaces a task whose status the workflow no longer has", () => {
    // It used to vanish from the board entirely: still there, still assigned,
    // and the only way to move it was a dropdown on a card nobody could see.
    const columns = columnsOf(BOARD, [
      task({ id: "1", status: "todo" }),
      task({ id: "2", status: "in-review" }),
    ]);
    const last = columns[columns.length - 1];
    expect(last.name).toBe(ORPHAN_COLUMN);
    expect(last.orphaned).toBe(true);
    expect(last.tasks.map((t) => t.id)).toEqual(["2"]);
  });

  it("adds no bucket when every status is known", () => {
    const columns = columnsOf(BOARD, [task({ id: "1", status: "doing" })]);
    expect(columns).toHaveLength(3);
    expect(columns.some((column) => column.orphaned)).toBe(false);
  });
});

describe("dropTarget", () => {
  it("ignores a card dropped back where it came from", () => {
    // The commonest drop of all — a drag somebody thought better of. Sending
    // it would burn a verify run, an audit line and maybe a confirmation on a
    // move that is not a move.
    expect(dropTarget("todo", "todo")).toBe("");
    expect(dropTarget("todo", null)).toBe("");
  });

  it("returns the column a card actually moved to", () => {
    expect(dropTarget("todo", "doing")).toBe("doing");
  });
});
