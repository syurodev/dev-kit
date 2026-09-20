import { describe, expect, it } from "bun:test";
import { boardForProject, chipsByPlanNode } from "./planTasks";
import type { Board, Task } from "@/bridge/tasks";

const board = (id: string, projectPath: string): Board =>
  ({ id, name: id, keyPrefix: "X", color: "", workflow: { statuses: [] }, projectPath }) as Board;

const task = (id: string, over: Partial<Task> = {}): Task =>
  ({
    id,
    key: `DEV-${id}`,
    title: `task ${id}`,
    status: "todo",
    archivedAt: "",
    ...over,
  }) as Task;

describe("boardForProject", () => {
  it("picks the board that names this project", () => {
    const boards = [board("a", "/other"), board("b", "/here")];
    expect(boardForProject(boards, "/here")?.id).toBe("b");
  });

  it("is null when no board names it — the common case, not a failure", () => {
    expect(boardForProject([board("a", "/other")], "/here")).toBeNull();
    // A board that declares no project belongs to no project.
    expect(boardForProject([board("a", "")], "/here")).toBeNull();
  });

  it("is null with no project open, so nothing is drawn", () => {
    expect(boardForProject([board("a", "")], "")).toBeNull();
  });
});

describe("chipsByPlanNode", () => {
  it("narrows a task to what a chip draws", () => {
    const chips = chipsByPlanNode({ n1: [task("1")] });
    expect(chips.n1).toEqual([{ id: "1", key: "DEV-1", title: "task 1", status: "todo" }]);
  });

  it("drops archived tasks, which are not current work", () => {
    const chips = chipsByPlanNode({ n1: [task("1", { archivedAt: "2026-01-01T00:00:00Z" })] });
    // The node gets no entry at all rather than an empty array, so the canvas
    // has nothing to render and no empty row to draw.
    expect(chips.n1).toBeUndefined();
  });

  it("keeps a node whose tasks are only partly archived", () => {
    const chips = chipsByPlanNode({
      n1: [task("1", { archivedAt: "2026-01-01T00:00:00Z" }), task("2")],
    });
    expect(chips.n1?.map((chip) => chip.id)).toEqual(["2"]);
  });
});
