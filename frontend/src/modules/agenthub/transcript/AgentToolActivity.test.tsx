import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import { StepGroup } from "../surfaces/AgentTurn";
import { AgentToolActivity, isToolActivity } from "./AgentToolActivity";
import { toolActivityOf } from "@/workbench/toolActivity";
import { parseTaskResult } from "@/modules/tasks/taskResult";
// The owning modules register their activities (and result views) as their
// manifests load, the way the app loads them.
import "@/modules/tasks/manifest";
import "@/modules/git/manifest";
import "@/modules/notes/manifest";
import "@/modules/apiclient/manifest";

const boardInput = { name: "Backend", keyPrefix: "be" };
const board = { id: "b1", name: "Backend", keyPrefix: "BE", color: "", workflow: { statuses: [] }, workspaceId: "ws-a" };

function call(name: string, status: string, rawInput: unknown, rawOutput?: unknown) {
  return { id: `${name}-${status}`, name, kind: "other", title: name, status, rawInput, rawOutput };
}

describe("tool activities", () => {
  it("are found by the Go name and by the wire name a vendor reports", () => {
    expect(toolActivityOf("tasks.board.create")?.done).toBe("Created board");
    expect(toolActivityOf("mcp__devkit__tasks_board_create")?.done).toBe("Created board");
    expect(toolActivityOf("mcp__devkit__tasks_list")).toBeUndefined();
    expect(isToolActivity({ id: "x", name: "mcp__devkit__git_push" })).toBe(true);
    expect(isToolActivity({ id: "x", name: "Read" })).toBe(false);
  });

  it("name their target from the input while running and from the answer once done", () => {
    const activity = toolActivityOf("tasks.board.create")!;
    expect(activity.target({ name: "", rawInput: boardInput, rawOutput: undefined, state: "running" })).toEqual({
      label: "Backend",
      detail: "BE",
      ref: undefined,
    });
    expect(activity.target({ name: "", rawInput: boardInput, rawOutput: board, state: "done" })?.ref).toEqual({
      module: "tasks",
      kind: "board",
      id: "b1",
      label: "Backend",
    });
    // rawInput as a JSON string, as some transports deliver it.
    const commit = toolActivityOf("git.commit")!;
    expect(commit.target({ name: "", rawInput: JSON.stringify({ message: "Fix login\n\nlong body" }), rawOutput: undefined, state: "running" }))
      .toEqual({ label: "Fix login" });
  });

  it("read tasks.transition's answer, which carries the task under `task`", () => {
    const task = { id: "t1", key: "BE-1a2b3c", title: "Fix login", status: "done" };
    expect(parseTaskResult({ task, verifying: false })).toEqual(task);
    const target = toolActivityOf("tasks.transition")!.target({
      name: "",
      rawInput: { taskId: "t1", to: "done" },
      rawOutput: { task, verifying: false },
      state: "done",
    });
    expect(target).toMatchObject({ label: "Fix login", detail: "BE-1a2b3c → done", ref: { kind: "task", id: "t1" } });
  });
});

describe("AgentToolActivity", () => {
  it("says what is happening, to what, while the call runs", () => {
    const html = renderToString(<AgentToolActivity call={call("mcp__devkit__tasks_board_create", "in_progress", boardInput)} />);
    expect(html).toContain('data-state="running"');
    expect(html).toContain("Creating board…");
    expect(html).toContain("Backend");
    expect(html).not.toContain(">Open<");
    // Born live: it arrives with motion.
    expect(html).toContain("activity-enter");
  });

  it("offers Open once done, and loaded history stands still", () => {
    const html = renderToString(<AgentToolActivity call={call("tasks.board.create", "completed", boardInput, board)} />);
    expect(html).toContain('data-state="done"');
    expect(html).toContain("Created board");
    expect(html).toContain("Open");
    expect(html).not.toContain("activity-enter");
  });

  it("says it failed, in the failure's colour", () => {
    const html = renderToString(<AgentToolActivity call={call("git.push", "failed", { branch: "feature/x" })} />);
    expect(html).toContain('data-state="failed"');
    expect(html).toContain("Couldn&#x27;t push");
    expect(html).toContain("feature/x");
  });

  it("is nothing for a tool that changes nothing", () => {
    expect(renderToString(<AgentToolActivity call={call("tasks.list", "completed", {})} />)).toBe("");
  });
});

describe("StepGroup with activities", () => {
  const read: TranscriptItem = { id: "r", kind: "tool_call", toolCall: { id: "r", kind: "read", title: "Read a.go", status: "completed" } };
  const create: TranscriptItem = {
    id: "c",
    kind: "tool_call",
    toolCall: call("mcp__devkit__tasks_board_create", "in_progress", boardInput),
  };

  it("keeps the card on screen while the rest of the run is folded", () => {
    const html = renderToString(<StepGroup items={[read, create]} />);
    expect(html).toContain('data-state="collapsed"');
    expect(html).toContain('data-testid="step-activities"');
    expect(html).toContain("Creating board…");
    expect(html).not.toContain("Read a.go");
  });

  it("gives a lone activity its line too, the way to its raw call", () => {
    const html = renderToString(<StepGroup items={[create]} />);
    expect(html).toContain('data-testid="step-group"');
    expect(html).toContain('data-testid="tool-activity"');
  });

  it("leaves a run of reads exactly as it was", () => {
    const html = renderToString(<StepGroup items={[read]} />);
    expect(html).not.toContain('data-testid="step-group"');
    expect(html).not.toContain("step-activities");
  });
});

describe("git history activities", () => {
  const head = { sha: "abc123def", short: "abc123d", subject: "feat: x" };

  it("name the new head once a rebase finished, and the stop when it did not", () => {
    const rebase = toolActivityOf("mcp__devkit__git_rebase")!;
    expect(rebase.target({ name: "", rawInput: { onto: "main" }, rawOutput: undefined, state: "running" })).toEqual({ label: "onto main" });
    expect(rebase.target({ name: "", rawInput: { onto: "main" }, rawOutput: { head }, state: "done" })?.ref).toMatchObject({ kind: "commit", id: "abc123def" });
    expect(
      rebase.target({ name: "", rawInput: { onto: "main" }, rawOutput: { head, stopped: "rebase", conflicted: ["a.go", "b.go"] }, state: "done" }),
    ).toEqual({ label: "Stopped on 2 conflicts", detail: "a.go" });
  });

  it("count a squash and name picked commits", () => {
    const squash = toolActivityOf("git.squash")!;
    expect(squash.target({ name: "", rawInput: { count: 3 }, rawOutput: head, state: "done" })).toMatchObject({ label: "feat: x", detail: "3 commits · abc123d" });
    const pick = toolActivityOf("git.cherryPick")!;
    expect(pick.target({ name: "", rawInput: { commits: ["abc", "def"] }, rawOutput: undefined, state: "running" })).toEqual({ label: "abc +1" });
    expect(toolActivityOf("git.revert")!.failed).toBe("Couldn't revert");
  });

  it("say where the branch stands after a pull", () => {
    const pull = toolActivityOf("git.pull")!;
    expect(pull.target({ name: "", rawInput: {}, rawOutput: { branch: "main", upstream: "origin/main", ahead: 0, behind: 0 }, state: "done" })).toEqual({
      label: "main",
      detail: "origin/main · up to date",
    });
  });
});
