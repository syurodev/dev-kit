import { parseToolOutput } from "@/workbench/toolResultViews";
import { firstLine, registerToolActivity, stringField, toolRecord, type ToolActivityTarget } from "@/workbench/toolActivity";
import { parseTaskResult } from "./taskResult";

const preloadTaskCard = () => import("./TaskResultCard");

/** A task the tool answered with, or — while it runs — what its input names. */
function taskTarget(rawInput: unknown, rawOutput: unknown, done: boolean): ToolActivityTarget | null {
  const task = done ? parseTaskResult(rawOutput) : null;
  if (task) return { label: task.title, detail: task.key, ref: { module: "tasks", kind: "task", id: task.id, label: task.key } };
  const title = stringField(rawInput, "title");
  return title ? { label: title } : null;
}

function taskVerb(running: string, done: string, failed: string) {
  return {
    module: "tasks",
    running,
    done,
    failed,
    preload: preloadTaskCard,
    target: ({ rawInput, rawOutput, state }: { rawInput: unknown; rawOutput: unknown; state: string }) =>
      taskTarget(rawInput, rawOutput, state === "done"),
  };
}

/**
 * What the tasks.* tools that change something do (workbench/toolActivity.ts):
 * the transcript keeps these on screen while the reads fold away.
 */
export function registerTaskActivities(): void {
  registerToolActivity("tasks.board.create", {
    module: "tasks",
    running: "Creating board",
    done: "Created board",
    failed: "Couldn't create board",
    target: ({ rawInput, rawOutput, state }) => {
      const board = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
      const id = stringField(board, "id");
      const name = stringField(board, "name") || stringField(rawInput, "name");
      if (!name) return null;
      const prefix = stringField(board, "keyPrefix") || stringField(rawInput, "keyPrefix").toUpperCase();
      return { label: name, detail: prefix || undefined, ref: id ? { module: "tasks", kind: "board", id, label: name } : undefined };
    },
  });
  registerToolActivity("tasks.create", taskVerb("Creating task", "Created task", "Couldn't create task"));
  registerToolActivity("tasks.update", taskVerb("Updating task", "Updated task", "Couldn't update task"));
  registerToolActivity("tasks.setFields", taskVerb("Updating task", "Updated task", "Couldn't update task"));
  registerToolActivity("tasks.transition", {
    ...taskVerb("Moving task", "Moved task", "Couldn't move task"),
    target: ({ rawInput, rawOutput, state }) => {
      const target = taskTarget(rawInput, rawOutput, state === "done");
      const to = stringField(rawInput, "to");
      if (target) return { ...target, detail: [target.detail, to && `→ ${to}`].filter(Boolean).join(" ") };
      return to ? { label: `→ ${to}` } : null;
    },
  });
  registerToolActivity("tasks.claim", taskVerb("Claiming task", "Claimed task", "Couldn't claim task"));
  registerToolActivity("tasks.release", taskVerb("Releasing task", "Released task", "Couldn't release task"));
  registerToolActivity("tasks.archive", taskVerb("Archiving task", "Archived task", "Couldn't archive task"));
  registerToolActivity("tasks.comment.add", {
    module: "tasks",
    running: "Commenting",
    done: "Commented",
    failed: "Couldn't comment",
    target: ({ rawInput, rawOutput, state }) => {
      const comment = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
      const taskId = stringField(comment, "taskId") || stringField(rawInput, "taskId");
      const body = firstLine(stringField(rawInput, "body"));
      if (!body) return null;
      return { label: body, ref: taskId && state === "done" ? { module: "tasks", kind: "task", id: taskId, label: "Task" } : undefined };
    },
  });
  registerToolActivity("tasks.createFromPlan", {
    module: "tasks",
    running: "Creating tasks from the plan",
    done: "Created tasks from the plan",
    failed: "Couldn't create tasks from the plan",
    target: ({ rawOutput, state }) => {
      const created = state === "done" ? parseToolOutput(rawOutput) : null;
      if (!Array.isArray(created)) return null;
      return { label: `${created.length} task${created.length === 1 ? "" : "s"}` };
    },
  });
}
