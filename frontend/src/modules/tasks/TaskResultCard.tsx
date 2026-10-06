import { KanbanSquare } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { TaskResult } from "./taskResult";

/** `tasks.create` / `get` / `update` / `transition` — all four answer with a Task, so one card: key, title, status. */
export function TaskResultCard({ task, heading }: { task: TaskResult; heading?: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={KanbanSquare}
        title={heading ?? "Task"}
        meta={task.status}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "tasks.reveal",
                { module: "tasks", kind: "task", id: task.id, label: task.key },
                { placement: "beside" },
              )
            }
          >
            Open
          </FramedCardAction>
        }
      />
      <FramedCardPreview className="flex-row items-baseline gap-(--space-inline)">
        <span className="shrink-0 text-code-12-medium text-text-secondary">{task.key}</span>
        <span className="min-w-0 truncate text-body-13 text-text-primary" title={task.title}>
          {task.title}
        </span>
      </FramedCardPreview>
    </FramedCard>
  );
}
