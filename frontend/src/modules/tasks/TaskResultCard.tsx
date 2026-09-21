import { KanbanSquare } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { TaskResult } from "./taskResult";

/**
 * ToolResultView shared by tasks.create, tasks.get, tasks.update and
 * tasks.transition (docs/dev-kit/tool-result-view.mdx) — all four answer
 * with the same Task shape. "Open" goes through the existing `tasks.reveal`
 * intent.
 */
export function TaskResultCard({ task }: { task: TaskResult }) {
  return (
    <ToolResultCard
      icon={KanbanSquare}
      title={`${task.key} — ${task.title}`}
      subtitle={task.status}
      actionLabel="Open"
      onAction={() =>
        void dispatchIntent(
          "tasks.reveal",
          { module: "tasks", kind: "task", id: task.id, label: task.key },
          { placement: "beside" },
        )
      }
    />
  );
}
