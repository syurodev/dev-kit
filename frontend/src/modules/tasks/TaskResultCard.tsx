import { KanbanSquare } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { TaskResult } from "./taskResult";

/**
 * ToolResultView shared by tasks.create, tasks.get, tasks.update and
 * tasks.transition (docs/dev-kit/tool-result-view.mdx) — all four answer
 * with the same Task shape. The whole row opens it through the existing
 * `tasks.reveal` intent.
 */
export function TaskResultCard({ task }: { task: TaskResult }) {
  return (
    <ToolResultRow
      icon={KanbanSquare}
      label={
        <>
          <span className="text-code-12-medium">{task.key}</span> {task.title}
        </>
      }
      trailing={task.status}
      onClick={() =>
        void dispatchIntent(
          "tasks.reveal",
          { module: "tasks", kind: "task", id: task.id, label: task.key },
          { placement: "beside" },
        )
      }
    />
  );
}
