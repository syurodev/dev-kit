import { useDroppable } from "@dnd-kit/core";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import type { Board, Task } from "@/bridge/tasks";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { StatusDot } from "../TaskBits";
import { openBlockers, type Column } from "../taskModel";
import { TaskCard } from "./TaskCard";

const LIST_MOTION = { duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" };

/**
 * One column: a drop target when it is a real status. The "No column" bucket
 * (tasks whose status the workflow lost) takes no drops — there is no status
 * to move a task into.
 */
export function BoardColumn({
  column,
  board,
  childrenByParent,
  byId,
  inspectedId,
  onInspect,
}: {
  column: Column;
  board: Board;
  childrenByParent: ReadonlyMap<string, Task[]>;
  byId: ReadonlyMap<string, Task>;
  inspectedId: string | null;
  onInspect: (task: Task) => void;
}) {
  const droppable = !!column.status;
  const { setNodeRef, isOver } = useDroppable({ id: column.id, disabled: !droppable });
  const [listRef] = useAutoAnimate<HTMLUListElement>(LIST_MOTION);

  return (
    <section
      ref={setNodeRef}
      aria-label={column.name}
      className={cn(
        "flex w-72 shrink-0 flex-col rounded-lg border border-transparent transition-colors duration-(--motion-fast)",
        isOver && "border-status-info/40 bg-state-soft",
      )}
    >
      <h3 className="flex h-8 shrink-0 items-center gap-2 px-2 text-label-12 text-text-secondary">
        <StatusDot category={column.status?.category} />
        <span className="min-w-0 truncate">{column.name}</span>
        <span className="text-caption-11 text-text-tertiary tabular-nums">{column.tasks.length}</span>
      </h3>
      <ul ref={listRef} className="flex min-h-16 flex-1 flex-col gap-2 overflow-y-auto px-1 pb-2">
        {column.tasks.map((task) => (
          <TaskCard
            key={task.id}
            task={task}
            board={board}
            subtasks={childrenByParent.get(task.id) ?? EMPTY_ARRAY}
            waitingOn={openBlockers(task, byId, board).length}
            selected={task.id === inspectedId}
            onInspect={onInspect}
          />
        ))}
      </ul>
    </section>
  );
}
