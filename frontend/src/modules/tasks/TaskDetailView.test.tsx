import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Board, Comment, Run, Task } from "@/bridge/tasks";
import { taskKeys } from "./taskActions";
import { useTasksStore } from "./tasksStore";
import { TaskDetailView } from "./TaskDetailView";

/**
 * Server-rendered with the query cache seeded, so nothing reaches the bridge:
 * what a task tab draws in its first frame when it has been read before.
 */

const board: Board = {
  id: "b1",
  name: "Payments board",
  keyPrefix: "PAY",
  color: "",
  workspaceId: "w1",
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
    key: `PAY-${id}`,
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
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

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

function render(
  shown: Task,
  boardTasks: Task[],
  extra: { comments?: Comment[]; runs?: Run[] } = {},
): string {
  const client = new QueryClient();
  client.setQueryData(taskKeys.task(shown.id), shown);
  client.setQueryData(taskKeys.comments(shown.id), extra.comments ?? []);
  client.setQueryData(taskKeys.runs(shown.id), extra.runs ?? []);
  seed(useTasksStore, { boards: [board], tasksByBoard: { b1: boardTasks } });
  // React separates adjacent text with <!-- -->; the reader does not see it.
  return renderToString(
    <QueryClientProvider client={client}>
      <TaskDetailView params={{ taskId: shown.id }} />
    </QueryClientProvider>,
  ).replace(/<!-- -->/g, "");
}

beforeEach(() => {
  seed(useTasksStore, { boards: null, tasksByBoard: {}, error: "" });
});

describe("TaskDetailView", () => {
  test("draws nothing before the task has been read", () => {
    seed(useTasksStore, { boards: [board] });
    const html = renderToString(
      <QueryClientProvider client={new QueryClient()}>
        <TaskDetailView params={{ taskId: "a" }} />
      </QueryClientProvider>,
    );
    expect(html).toBe("");
  });

  test("the title is an editable field and the description is shown", () => {
    const a = task("a", { title: "Refund flow", description: "Money goes back" });
    const html = render(a, [a]);
    expect(html).toContain('aria-label="Task title"');
    expect(html).toContain('value="Refund flow"');
    expect(html).toContain("Money goes back");
    expect(html).toContain("Payments board");
  });

  test("an empty description offers to add one", () => {
    const a = task("a");
    expect(render(a, [a])).toContain("Add a description");
  });

  test("dependencies read from both ends, and an unfinished one is waited on", () => {
    const a = task("a", { blockedBy: ["b"] });
    const b = task("b", { title: "Ledger schema" });
    const c = task("c", { title: "Payout report", blockedBy: ["a"] });
    const html = render(a, [a, b, c]);
    expect(html).toContain("Depends on");
    expect(html).toContain("Ledger schema");
    expect(html).toContain("Blocks");
    expect(html).toContain("Payout report");
    expect(html).toContain("Waiting on 1 task");
  });

  test("a finished dependency is not waited on", () => {
    const a = task("a", { blockedBy: ["b"] });
    const b = task("b", { status: "done" });
    expect(render(a, [a, b])).not.toContain("Waiting on");
  });

  test("a dependency this board no longer lists is shown so it can be removed", () => {
    const a = task("a", { blockedBy: ["deadbeef00"] });
    expect(render(a, [a])).toContain("Unavailable task");
  });

  test("subtasks are counted by category", () => {
    const a = task("a");
    const html = render(a, [
      a,
      task("s1", { parentId: "a", status: "done" }),
      task("s2", { parentId: "a" }),
    ]);
    expect(html).toContain("Subtasks");
    expect(html).toContain("1/2");
  });

  test("a live verify run offers Cancel", () => {
    const a = task("a");
    const run: Run = {
      id: "r1",
      taskId: "a",
      target: "done",
      command: "go test ./...",
      state: "running",
      exitCode: 0,
      output: "",
      requestedAt: "2026-09-01T00:00:00Z",
    };
    const html = render(a, [a], { runs: [run] });
    expect(html).toContain("go test ./...");
    expect(html).toContain("Cancel");
  });

  test("conversation and activity are separate", () => {
    const a = task("a");
    const comments: Comment[] = [
      { id: "c1", taskId: "a", author: "agent:s1", kind: "agent", body: "Started on it", createdAt: "" },
      { id: "c2", taskId: "a", author: "system", kind: "system", body: "verify passed", createdAt: "" },
    ];
    const html = render(a, [a], { comments });
    expect(html).toContain("Started on it");
    // The Activity tab is not the one showing.
    expect(html).not.toContain("verify passed");
  });
});
