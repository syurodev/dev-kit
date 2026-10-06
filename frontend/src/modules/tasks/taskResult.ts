import { parseToolOutput } from "@/workbench/toolResultViews";

/**
 * The slice of internal/tasks/tasks.go: Task that the card shows — tasks.create,
 * tasks.get, tasks.update and tasks.transition all answer with a full Task.
 */
export type TaskResult = {
  id: string;
  key: string;
  title: string;
  status: string;
};

function isTaskResult(value: unknown): value is TaskResult {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.id === "string" && typeof v.key === "string" && typeof v.status === "string";
}

/**
 * Guards a task tool's result before the Card component is imported — shared
 * by every tasks.* renderer that answers with a Task (create/get/update/transition).
 */
export function parseTaskResult(rawOutput: unknown): TaskResult | null {
  const parsed = parseToolOutput(rawOutput);
  if (isTaskResult(parsed)) return parsed;
  // tasks.transition answers with a TransitionResult — the task under `task`,
  // beside whether a verify run now decides the close. Read as a bare Task it
  // matched nothing, and its card fell back to JSON every time.
  const task = parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>).task : undefined;
  return isTaskResult(task) ? task : null;
}
