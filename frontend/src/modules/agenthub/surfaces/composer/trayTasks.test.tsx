import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { Board, Task } from "@/bridge/tasks";
import { sessionLinked, type TrayTask } from "./useTrayStatus";
import { TrayTaskList } from "./agentTray";

function task(id: string, patch: Partial<Task> = {}): Task {
  return {
    id, boardId: "b", key: `BE-${id}`, title: `Task ${id}`, status: "todo", priority: "", labels: [], assignee: "",
    parentId: "", blockedBy: [], planNodeId: "", sessionId: "", branch: "", archivedAt: "", history: [],
    createdAt: "", updatedAt: "", ...patch,
  } as Task;
}
const board = { id: "b", name: "Backend", workflow: { statuses: [] } } as unknown as Board;
const row = (t: Task, extra: Partial<TrayTask> = {}): TrayTask => ({ task: t, board, statusName: "To do", active: false, done: false, held: false, ...extra });

describe("a session's tasks", () => {
  it("are the ones it made or picked up — not every task in the workspace, nor one it only moved", () => {
    const made = task("1", { history: [{ from: "", to: "todo", by: "agent:s1", at: "" }] });
    const moved = task("2", { history: [{ from: "todo", to: "doing", by: "agent:s1", at: "" }] });
    const held = task("3", { sessionId: "s1" });
    const someoneElses = task("4", { history: [{ from: "", to: "todo", by: "agent:s2", at: "" }] });
    const user = task("5", { history: [{ from: "", to: "todo", by: "user", at: "" }] });
    expect([made, moved, held, someoneElses, user].map((t) => sessionLinked(t, "s1"))).toEqual([true, false, true, false, false]);
  });

  it("belong to both the session that made them and the one that picked them up, even after letting go", () => {
    // s1 made it; s2 picked it up (moved into the active column, then the
    // pick) and has since let go: no holder any more.
    const history = [
      { from: "", to: "todo", by: "agent:s1", at: "" },
      { from: "todo", to: "doing", by: "agent:s2", at: "" },
      { from: "doing", to: "doing", by: "agent:s2", at: "" },
    ];
    const released = task("6", { history, sessionId: "" });
    expect(sessionLinked(released, "s1")).toBe(true);
    expect(sessionLinked(released, "s2")).toBe(true);
    expect(sessionLinked(released, "s3")).toBe(false);
    // Taken over by s3: still s1's and s2's, now s3's too.
    expect(sessionLinked(task("7", { history, sessionId: "s3" }), "s3")).toBe(true);
    // A draft has no session, so nothing is its own.
    expect(sessionLinked(released, "")).toBe(false);
  });

  it("are listed apart from the workspace's other open tasks", () => {
    const html = renderToString(
      <TrayTaskList tasks={[row(task("1"), { held: true })]} others={[row(task("9"))]} session />,
    );
    expect(html).toContain("This session");
    expect(html).toContain("Open in the workspace");
    expect(html.match(/data-testid="tray-task"/g)?.length).toBe(1);
    expect(html.match(/data-testid="tray-task-other"/g)?.length).toBe(1);
    expect(html).toContain("held");
  });

  it("say so when the session has none yet", () => {
    const html = renderToString(<TrayTaskList tasks={[]} others={[row(task("9"))]} session />);
    expect(html).toContain("No tasks from this session yet");
  });
});

describe("the tray's repositories", () => {
  const repo = (name: string, patch: Record<string, unknown> = {}) => ({
    path: `/ws/${name}`, name, branch: "main", detached: false, ahead: 0, behind: 0, changed: 0, conflicted: 0, error: "", own: false, ...patch,
  });

  it("lists every repository of a multi-repo workspace with what needs attention", async () => {
    const { TrayRepoList } = await import("./agentTray");
    const html = renderToString(
      <TrayRepoList
        repos={[
          repo("api", { own: true, ahead: 2, changed: 3 }),
          repo("web", { branch: "feature/x", behind: 1 }),
          repo("infra", { conflicted: 1 }),
          repo("broken", { error: "not a git repository" }),
        ]}
      />,
    );
    expect(html.match(/data-testid="tray-repo"/g)?.length).toBe(4);
    expect(html).toContain("this session");
    expect(html).toContain("↑2");
    expect(html).toContain("3 changed");
    expect(html).toContain("feature/x");
    expect(html).toContain("↓1");
    expect(html).toContain("1 conflicted");
    expect(html).toContain("unreadable");
    expect(html).toContain("clean");
  });
});
