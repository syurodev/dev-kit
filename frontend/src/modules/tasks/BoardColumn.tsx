import { useDraggable, useDroppable } from "@dnd-kit/core";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { cn } from "@/lib/utils";
import { progressOf, type Board, type Task } from "@/bridge/tasks";
import type { BoardColumnModel } from "./board";

/**
 * A card's height is fixed, and that is what makes windowing exact.
 *
 * A virtualizer told a size it then has to correct scrolls in jumps, because
 * every row it has not measured is a guess about where the scrollbar should
 * be. A kanban card has no reason to be taller than its neighbour — the title
 * clamps to two lines and carries the rest in a tooltip.
 */
const CARD_HEIGHT = 96;
const CARD_GAP = 8;
const ROW_HEIGHT = CARD_HEIGHT + CARD_GAP;

export function BoardColumn({
  column,
  board,
  allTasks,
  onMove,
}: {
  column: BoardColumnModel;
  board: Board;
  /** Every task, so a card can count its own subtasks. */
  allTasks: Task[];
  onMove: (task: Task, to: string) => void;
}) {
  // A column with no status is the orphan bucket: dropping a card into it
  // would mean writing a status the workflow does not have.
  const droppable = useDroppable({ id: column.id, disabled: column.orphaned });
  const rows = useVirtualRows(column.tasks.length, ROW_HEIGHT);

  return (
    <section className="flex w-64 shrink-0 flex-col">
      <h3
        className={cn(
          "pb-2 text-xs font-medium",
          column.orphaned ? "text-amber-500" : "text-fg-muted",
        )}
        title={
          column.orphaned
            ? "These tasks have a status this board's workflow no longer has. Move them with the dropdown on the card."
            : undefined
        }
      >
        {column.name} ({column.tasks.length})
      </h3>
      <div
        ref={(node) => {
          droppable.setNodeRef(node);
          rows.scrollRef.current = node;
        }}
        className={cn(
          "min-h-0 flex-1 overflow-auto rounded-md border border-transparent p-1",
          droppable.isOver && "border-dashed border-border-subtle bg-white/2",
        )}
      >
        <ul>
          {rows.padTop > 0 ? <li aria-hidden style={{ height: rows.padTop }} /> : null}
          {rows.indices.map((index) => {
            const task = column.tasks[index];
            return (
              <TaskCard
                key={task.id}
                task={task}
                board={board}
                siblings={allTasks}
                onMove={(to) => onMove(task, to)}
              />
            );
          })}
          {rows.padBottom > 0 ? <li aria-hidden style={{ height: rows.padBottom }} /> : null}
        </ul>
      </div>
    </section>
  );
}

/**
 * One card. Dragging it changes its status; the dropdown does the same thing
 * and stays, because it is the keyboard path and the only way to move a card
 * out of the orphan bucket.
 */
export function TaskCard({
  task,
  board,
  siblings,
  onMove,
  overlay,
}: {
  task: Task;
  board: Board;
  siblings: Task[];
  onMove: (to: string) => void;
  /** True for the copy that follows the cursor: it is not a drop target. */
  overlay?: boolean;
}) {
  const draggable = useDraggable({ id: task.id, disabled: overlay });
  const children = siblings.filter((candidate) => candidate.parentId === task.id);
  const progress = children.length > 0 ? progressOf(board, children) : null;

  return (
    <li
      ref={overlay ? undefined : draggable.setNodeRef}
      {...(overlay ? {} : draggable.listeners)}
      {...(overlay ? {} : draggable.attributes)}
      style={{ height: CARD_HEIGHT, marginBottom: overlay ? 0 : CARD_GAP }}
      className={cn(
        "flex flex-col overflow-hidden rounded-md border border-border-subtle bg-surface-well p-2",
        !overlay && "cursor-grab active:cursor-grabbing",
        // The original stays in place as a gap rather than disappearing, so
        // the column does not reflow under the cursor mid-drag.
        draggable.isDragging && !overlay && "opacity-40",
        overlay && "cursor-grabbing shadow-lg",
      )}
    >
      <div className="flex items-baseline gap-2">
        <code className="text-xs text-fg-muted">{task.key}</code>
        {task.sessionId ? (
          <span
            className="size-1.5 rounded-full bg-emerald-500"
            title="An agent session holds this task"
            aria-label="Held by an agent"
          />
        ) : null}
      </div>
      <div className="line-clamp-2 flex-1 pt-0.5 text-sm" title={task.title}>
        {task.title}
      </div>
      <div className="flex items-center gap-2 text-xs text-fg-muted">
        {progress ? (
          // Counted by category, so a board that renamed its Done column still
          // counts. It counts without blocking: the parent can still close.
          <span>
            {progress.done}/{progress.total}
          </span>
        ) : null}
        {task.planNodeId ? <span title="Implements a Code Graph plan node">plan</span> : null}
        {task.branch ? <span className="min-w-0 truncate font-mono">{task.branch}</span> : null}
        <div className="flex-1" />
        {/* The pointer sensor must not claim this: a drag started on a select
            is somebody opening the menu, and the menu is the keyboard path. */}
        <select
          className="bg-transparent text-xs"
          value={task.status}
          aria-label={`Status of ${task.key}`}
          onPointerDown={(event) => event.stopPropagation()}
          onKeyDown={(event) => event.stopPropagation()}
          onChange={(event) => onMove(event.target.value)}
        >
          {/* A card in the orphan bucket has a status no option names, which
              would silently show the first one. Naming it keeps the dropdown
              honest until somebody picks a real column. */}
          {board.workflow.statuses.every((status) => status.id !== task.status) ? (
            <option value={task.status}>{task.status} (unknown)</option>
          ) : null}
          {board.workflow.statuses.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </select>
      </div>
    </li>
  );
}
