import { TaskDetail } from "./detail/TaskDetail";

/** A task as its own tab (`tasks.detail`). The board shows the same thing in its right-hand pane. */
export function TaskDetailView({ params }: { params?: Record<string, unknown> }) {
  const taskId = typeof params?.taskId === "string" ? params.taskId : "";
  return <TaskDetail taskId={taskId} layout="page" />;
}
