import { AnimatedNumber } from "@/components/ui/animated-number";
import { useState } from "react";
import { ChevronRight, GitBranch, Hourglass } from "lucide-react";
import { progressOf, type Board, type Task } from "@/bridge/tasks";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { openTaskDetail } from "../documents";
import { AgentHeld, AssigneeAvatar, PriorityIcon, StatusDot } from "../TaskBits";
import { openBlockers, relativeTime, type Column } from "../taskModel";

/** Labels shown on a row before the rest fold into "+n". */
const SHOWN_LABELS = 2;

function Row({
  task,
  board,
  subtasks,
  waitingOn,
  selected,
  onInspect,
}: {
  task: Task;
  board: Board;
  subtasks: readonly Task[];
  waitingOn: number;
  selected: boolean;
  onInspect: (task: Task) => void;
}) {
  const progress = subtasks.length > 0 ? progressOf(board, [...subtasks]) : null;
  const labels = task.labels ?? [];
  return (
    <li className="list-none">
      <button
        type="button"
        aria-current={selected || undefined}
        onClick={() => onInspect(task)}
        onDoubleClick={() => openTaskDetail(task.id, `${task.key} ${task.title}`, { preview: false })}
        className={cn(
          // Fixed height whatever the row carries (smoothness 3.1).
          "flex h-9 w-full items-center gap-3 border-b border-border-subtle px-4 text-left transition-colors duration-(--motion-fast) hover:bg-state-soft",
          selected && "bg-state-soft",
        )}
      >
        <PriorityIcon priority={task.priority} />
        <span className="w-20 shrink-0 truncate text-code-11 text-text-tertiary">{task.key}</span>
        <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{task.title}</span>
        {task.sessionId ? <AgentHeld className="size-(--icon-12)" /> : null}
        {waitingOn > 0 ? (
          <span className="inline-flex items-center gap-1 text-caption-11 text-status-warning" title="Waiting on unfinished tasks">
            <Hourglass aria-hidden className="size-(--icon-12)" />
            <span className="tabular-nums">{waitingOn}</span>
          </span>
        ) : null}
        {labels.slice(0, SHOWN_LABELS).map((label) => (
          <span
            key={label}
            className="inline-flex h-5 max-w-24 shrink-0 items-center truncate rounded-sm bg-state-soft px-1.5 text-caption-11 text-text-tertiary"
          >
            {label}
          </span>
        ))}
        {labels.length > SHOWN_LABELS ? (
          <span className="text-caption-11 text-text-tertiary">+{labels.length - SHOWN_LABELS}</span>
        ) : null}
        {task.branch ? (
          <span className="inline-flex max-w-40 min-w-0 items-center gap-1 text-caption-11 text-text-tertiary">
            <GitBranch aria-hidden className="size-(--icon-12) shrink-0" />
            <span className="truncate font-mono">{task.branch}</span>
          </span>
        ) : null}
        <span className="w-10 shrink-0 text-right text-caption-11 text-text-tertiary tabular-nums">
          {progress ? `${progress.done}/${progress.total}` : ""}
        </span>
        <span className="w-16 shrink-0 text-right text-caption-11 text-text-tertiary tabular-nums" title={task.updatedAt}>
          {relativeTime(task.updatedAt)}
        </span>
        <span className="flex w-5 shrink-0 justify-end">
          <AssigneeAvatar name={task.assignee} />
        </span>
      </button>
    </li>
  );
}

/**
 * The same board as rows, grouped by column in workflow order — for a board
 * too long to read as cards, and for scanning keys, assignees and dates down
 * one edge. Finished groups start folded: they are the long ones and the
 * least read, and a folded group costs no rows at all.
 */
export function BoardList({
  board,
  columns,
  childrenByParent,
  byId,
  inspectedId,
  onInspect,
}: {
  board: Board;
  columns: readonly Column[];
  childrenByParent: ReadonlyMap<string, Task[]>;
  byId: ReadonlyMap<string, Task>;
  inspectedId: string | null;
  onInspect: (task: Task) => void;
}) {
  const [folded, setFolded] = useState<Record<string, boolean>>({});
  return (
    <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable">
      {columns.map((column) => {
        const isFolded = folded[column.id] ?? column.status?.category === "terminal";
        return (
          <section key={column.id} aria-label={column.name}>
            <button
              type="button"
              aria-expanded={!isFolded}
              onClick={() => setFolded((current) => ({ ...current, [column.id]: !isFolded }))}
              className="sticky top-0 z-10 flex h-9 w-full items-center gap-2 border-b border-border-subtle bg-surface-base px-4 text-left text-label-12 text-text-secondary"
            >
              <ChevronRight
                aria-hidden
                className={cn(
                  "size-(--icon-14) text-text-tertiary transition-transform duration-(--motion-fast)",
                  !isFolded && "rotate-90",
                )}
              />
              <StatusDot category={column.status?.category} />
              {column.name}
              <span className="rounded-full bg-state-soft px-1.5 text-caption-11 text-text-tertiary tabular-nums">
                <AnimatedNumber value={column.tasks.length} />
              </span>
            </button>
            <AnimatedCollapse open={!isFolded}>
              <ul>
                {column.tasks.length === 0 ? (
                  <li className="flex h-9 list-none items-center border-b border-border-subtle px-4 text-caption-11 text-text-tertiary">
                    No tasks
                  </li>
                ) : (
                  column.tasks.map((task) => (
                    <Row
                      key={task.id}
                      task={task}
                      board={board}
                      subtasks={childrenByParent.get(task.id) ?? EMPTY_ARRAY}
                      waitingOn={openBlockers(task, byId, board).length}
                      selected={task.id === inspectedId}
                      onInspect={onInspect}
                    />
                  ))
                )}
              </ul>
            </AnimatedCollapse>
          </section>
        );
      })}
    </div>
  );
}
