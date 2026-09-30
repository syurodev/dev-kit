import { useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { createTask, progressOf, type Board, type Task } from "@/bridge/tasks";
import { errorMessage } from "@/bridge/errors";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import { openTaskDetail } from "../documents";
import { PriorityIcon, StatusDot } from "../TaskBits";
import { moveTo } from "../taskActions";
import { useTasksStore } from "../tasksStore";
import { isTerminal, statusOf } from "../taskModel";

/** The list motion every other list in the app uses (smoothness 4.4). */
const LIST_MOTION = { duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" };

/**
 * A parent's checklist. The dot is a checkbox: done moves a subtask to the
 * board's first terminal column, undone back to its first open one — by
 * category, so a board that renamed "Done" still ticks. Progress counts the
 * same way and never blocks the parent from closing.
 */
export function TaskSubtasks({
  task,
  board,
  subtasks,
}: {
  task: Task;
  board: Board;
  subtasks: readonly Task[];
}) {
  const [draft, setDraft] = useState("");
  const [listRef] = useAutoAnimate<HTMLUListElement>(LIST_MOTION);
  const progress = progressOf(board, [...subtasks]);
  const firstOpen = board.workflow.statuses.find((status) => status.category === "open")?.id;
  const firstDone = board.workflow.statuses.find((status) => status.category === "terminal")?.id;

  const add = () => {
    const title = draft.trim();
    if (!title) return;
    setDraft("");
    createTask({ boardId: task.boardId, title, parentId: task.id })
      .then((created) => useTasksStore.getState().upsertTask(created))
      .catch((error) => toast.error(errorMessage(error)));
  };

  return (
    <section className="flex flex-col gap-2">
      <header className="flex items-center gap-3">
        <h3 className="text-title-13 text-text-primary">Subtasks</h3>
        {progress.total > 0 ? (
          <>
            <span className="text-caption-11 text-text-tertiary tabular-nums">
              {progress.done}/{progress.total}
            </span>
            <Progress value={(progress.done / progress.total) * 100} className="max-w-32" aria-label="Subtasks done" />
          </>
        ) : null}
      </header>
      <ul ref={listRef} className="flex flex-col">
        {subtasks.map((child) => {
          const done = isTerminal(board, child.status);
          const target = done ? firstOpen : firstDone;
          return (
            <li
              key={child.id}
              className="flex h-8 items-center gap-2 rounded-md px-1.5 transition-colors duration-(--motion-fast) hover:bg-surface-raised"
            >
              <button
                type="button"
                aria-label={done ? `Reopen ${child.key}` : `Mark ${child.key} done`}
                disabled={!target}
                onClick={() => target && void moveTo(child, target)}
                className="flex size-5 items-center justify-center rounded-sm"
              >
                <StatusDot category={statusOf(board, child.status)?.category} className="size-2.5" />
              </button>
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                onClick={() => openTaskDetail(child.id, `${child.key} ${child.title}`)}
                onDoubleClick={() => openTaskDetail(child.id, `${child.key} ${child.title}`, { preview: false })}
              >
                <span className="shrink-0 text-code-11 text-text-tertiary">{child.key}</span>
                <span
                  className={cn(
                    "min-w-0 truncate text-body-13 transition-colors duration-(--motion-fast)",
                    done ? "text-text-tertiary line-through" : "text-text-primary",
                  )}
                >
                  {child.title}
                </span>
              </button>
              {child.priority ? <PriorityIcon priority={child.priority} /> : null}
            </li>
          );
        })}
      </ul>
      <div className="flex h-8 items-center gap-2 px-1.5">
        <Plus className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
        <input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              add();
            } else if (event.key === "Escape") {
              setDraft("");
              event.currentTarget.blur();
            }
          }}
          placeholder="Add a subtask"
          aria-label="New subtask title"
          maxLength={200}
          className="min-w-0 flex-1 bg-transparent text-body-13 text-text-primary outline-none placeholder:text-text-tertiary"
        />
      </div>
    </section>
  );
}
