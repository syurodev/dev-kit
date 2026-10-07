import { AnimatedNumber } from "@/components/ui/animated-number";
import { EASE_OUT_CSS, MOTION_BASE } from "@/lib/motion";
import { useState } from "react";
import { useDroppable } from "@dnd-kit/core";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { FoldHorizontal, Plus } from "lucide-react";
import type { Board, Task } from "@/bridge/tasks";
import { Button } from "@/components/ui/button";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { StatusDot } from "../TaskBits";
import { createIn } from "../taskActions";
import { collapsesWhenEmpty, openBlockers, type Column } from "../taskModel";
import { TaskCard } from "./TaskCard";

const LIST_MOTION = { duration: MOTION_BASE * 1000, easing: EASE_OUT_CSS };

/**
 * A column's "Add task": a button until it is used, then a card-shaped field
 * at the bottom of the lane. Enter adds and stays open for the next one —
 * adding several tasks to one column is the common case — Escape leaves.
 */
function QuickAdd({ board, statusId, columnName }: { board: Board; statusId: string; columnName: string }) {
  const [draft, setDraft] = useState<string | null>(null);

  if (draft === null) {
    return (
      <Button
        variant="ghost"
        size="sm"
        className="w-full justify-start px-2 text-text-tertiary"
        onClick={() => setDraft("")}
      >
        <Plus aria-hidden />
        Add task
      </Button>
    );
  }

  return (
    <div className="rounded-md border border-status-info bg-surface-raised p-2">
      <textarea
        autoFocus
        rows={2}
        value={draft}
        maxLength={200}
        aria-label={`New task in ${columnName}`}
        placeholder="Task title — Enter to add, Esc to stop"
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => {
          if (!draft.trim()) setDraft(null);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            setDraft(null);
          } else if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            const title = draft.trim();
            if (!title) return;
            setDraft("");
            void createIn(board, statusId, title);
          }
        }}
        className="w-full resize-none bg-transparent text-body-13 text-text-primary outline-none placeholder:text-text-tertiary"
      />
    </div>
  );
}

/**
 * One column: a lane with its own surface, so a short column still reads as
 * a column rather than as a few cards floating over nothing. A drop target
 * when it is a real status; the "No column" bucket (tasks whose status the
 * workflow lost) takes no drops and offers no "Add task" — there is no status
 * to put a task into.
 */
export function BoardColumn({
  column,
  board,
  childrenByParent,
  byId,
  inspectedId,
  onInspect,
  dragging,
}: {
  column: Column;
  board: Board;
  childrenByParent: ReadonlyMap<string, Task[]>;
  byId: ReadonlyMap<string, Task>;
  inspectedId: string | null;
  onInspect: (task: Task) => void;
  /** A card is being dragged somewhere on the board. */
  dragging: boolean;
}) {
  const status = column.status;
  const { setNodeRef, isOver } = useDroppable({ id: column.id, disabled: !status });
  const [listRef] = useAutoAnimate<HTMLUListElement>(LIST_MOTION);
  // Opened by hand: an empty finished column someone wants to add to. Stays
  // open for the life of the view rather than snapping shut under them the
  // moment its last card is dragged out.
  const [opened, setOpened] = useState(false);

  if (collapsesWhenEmpty(column) && !opened) {
    // Narrow but still a lane and still a drop target, full height so it is
    // easy to hit — and it does not widen mid-drag, which would move every
    // column to its right out from under the pointer.
    return (
      <section
        ref={setNodeRef}
        aria-label={column.name}
        className={cn(
          "flex w-10 shrink-0 flex-col rounded-lg border border-transparent bg-surface-well transition-colors duration-(--motion-fast)",
          dragging && "border-dashed border-status-info/40",
          isOver && "border-solid border-status-info/50 bg-state-soft",
        )}
      >
        <button
          type="button"
          title={`${column.name} — empty. Click to expand`}
          aria-label={`Expand ${column.name}`}
          onClick={() => setOpened(true)}
          className="flex flex-1 flex-col items-center gap-2 rounded-lg py-3 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-secondary"
        >
          <StatusDot category={status?.category} />
          <span className="text-label-12 writing-vertical">{column.name}</span>
          <span className="rounded-full bg-state-soft px-1 text-caption-11 tabular-nums">0</span>
        </button>
      </section>
    );
  }

  return (
    <section
      ref={setNodeRef}
      aria-label={column.name}
      className={cn(
        "flex w-72 shrink-0 flex-col rounded-lg border border-transparent bg-surface-well transition-colors duration-(--motion-fast)",
        isOver && "border-status-info/50 bg-state-soft",
      )}
    >
      <h3 className="flex h-10 shrink-0 items-center gap-2 px-3 text-label-12 text-text-secondary">
        <StatusDot category={status?.category} />
        <span className="min-w-0 truncate">{column.name}</span>
        <span className="rounded-full bg-state-soft px-1.5 text-caption-11 text-text-tertiary tabular-nums">
          <AnimatedNumber value={column.tasks.length} />
        </span>
        {opened && collapsesWhenEmpty(column) ? (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label={`Collapse ${column.name}`}
            className="ml-auto text-text-tertiary"
            onClick={() => setOpened(false)}
          >
            <FoldHorizontal aria-hidden />
          </Button>
        ) : null}
      </h3>
      <ul ref={listRef} className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-2">
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
        {column.tasks.length === 0 ? (
          // Room to drop into, and a word on what goes here. Always the same
          // box, so the column does not change height when a drag starts.
          <li
            className={cn(
              "flex h-20 list-none items-center justify-center rounded-md border border-dashed text-caption-11 transition-colors duration-(--motion-fast)",
              dragging && status ? "border-status-info/40 text-text-secondary" : "border-border-subtle text-text-tertiary",
            )}
          >
            {dragging && status ? "Drop here" : status?.category === "terminal" ? "Nothing finished yet" : "No tasks"}
          </li>
        ) : null}
      </ul>
      {status ? (
        <div className="shrink-0 px-2 pb-2">
          <QuickAdd board={board} statusId={status.id} columnName={column.name} />
        </div>
      ) : null}
    </section>
  );
}
