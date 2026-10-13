import { useDraggable } from "@dnd-kit/core";
import { Archive, Copy, Ellipsis, GitBranch, Hourglass, Maximize2, PanelRight } from "lucide-react";
import { toast } from "sonner";
import { progressOf, type Board, type Task } from "@/bridge/tasks";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { openTaskDetail } from "../documents";
import { AgentHeld, AssigneeAvatar, PriorityIcon, StatusDot } from "../TaskBits";
import { relativeTime } from "../taskModel";
import { archive, moveTo } from "../taskActions";

/** Labels shown on a card before the rest fold into "+n". */
const SHOWN_LABELS = 2;

function title(task: Task): string {
  return `${task.key} ${task.title}`;
}

/**
 * What a card draws. Split from the draggable wrapper so the drag overlay
 * draws the same card without registering a second draggable for it.
 */
export function TaskCardBody({
  task,
  board,
  subtasks,
  waitingOn,
}: {
  task: Task;
  board: Board;
  subtasks: readonly Task[];
  waitingOn: number;
}) {
  const progress = subtasks.length > 0 ? progressOf(board, [...subtasks]) : null;
  const labels = task.labels ?? [];
  const hasTags = waitingOn > 0 || labels.length > 0 || !!task.branch;

  return (
    <>
      <div className="flex h-4 items-center gap-1.5">
        <span className="text-code-11 text-text-tertiary">{task.key}</span>
        {task.sessionId ? <AgentHeld className="size-(--icon-12)" /> : null}
        <span className="flex-1" />
        {/* Under the card's menu button, which takes this corner on hover. */}
        {task.priority ? <PriorityIcon priority={task.priority} className="group-hover/card:invisible" /> : null}
      </div>
      <p className="line-clamp-2 text-body-13 text-text-primary">{task.title}</p>
      {hasTags ? (
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-caption-11 text-text-tertiary">
          {waitingOn > 0 ? (
            <span
              className="inline-flex h-5 items-center gap-1 rounded-sm bg-status-warning/10 px-1.5 text-status-warning"
              title="Waiting on unfinished tasks"
            >
              <Hourglass aria-hidden className="size-(--icon-12)" />
              <span className="tabular-nums">{waitingOn}</span>
            </span>
          ) : null}
          {labels.slice(0, SHOWN_LABELS).map((label) => (
            <span key={label} className="inline-flex h-5 max-w-24 items-center truncate rounded-sm bg-state-soft px-1.5">
              {label}
            </span>
          ))}
          {labels.length > SHOWN_LABELS ? <span>+{labels.length - SHOWN_LABELS}</span> : null}
          {task.branch ? (
            <span className="inline-flex min-w-0 items-center gap-1">
              <GitBranch aria-hidden className="size-(--icon-12) shrink-0" />
              <span className="truncate font-mono">{task.branch}</span>
            </span>
          ) : null}
        </div>
      ) : null}
      {/* Always drawn, so every card ends on the same line: progress on the
          left, who and when on the right. */}
      <div className="flex h-5 items-center gap-2 text-caption-11 text-text-tertiary">
        {progress ? (
          // Counted by category, so a board that renamed its Done column
          // still counts. It counts without blocking: the parent can close.
          <span className="inline-flex items-center gap-1.5" title="Subtasks done">
            <Progress value={(progress.done / progress.total) * 100} className="w-10" aria-hidden />
            <span className="tabular-nums">
              {progress.done}/{progress.total}
            </span>
          </span>
        ) : null}
        <span className="flex-1" />
        <span className="tabular-nums" title={task.updatedAt}>
          {relativeTime(task.updatedAt)}
        </span>
        <AssigneeAvatar name={task.assignee} />
      </div>
    </>
  );
}

export const CARD_CLASS =
  "group/card relative flex w-full flex-col gap-1.5 rounded-md border border-border-subtle bg-surface-raised p-2.5 text-left transition-colors duration-(--motion-fast)";

/**
 * One card: dragged to move it, clicked to show it in the board's right-hand
 * pane (double click opens it as its own tab). The pointer has to travel before a press becomes a
 * drag, so a click is still a click. The menu is the same move for a
 * keyboard, and for anyone who would rather not drag.
 */
export function TaskCard({
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
  /** Shown in the board's right-hand pane. */
  selected: boolean;
  /** Show this task in the board's right-hand pane. */
  onInspect: (task: Task) => void;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: task.id, data: { task } });

  return (
    <li className="list-none">
      <div
        ref={setNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`${title(task)} — show details`}
        aria-current={selected || undefined}
        onClick={() => onInspect(task)}
        onDoubleClick={() => openTaskDetail(task.id, title(task), { preview: false })}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onInspect(task);
          }
        }}
        className={cn(
          CARD_CLASS,
          "cursor-pointer hover:border-status-info/40 focus-visible:border-border-focus focus-visible:outline-none",
          selected && "border-status-info bg-state-soft hover:border-status-info",
          // The original stays where it was, faded, while its overlay moves:
          // the column does not reflow under the pointer mid-drag.
          isDragging && "opacity-38",
        )}
      >
        <TaskCardBody task={task} board={board} subtasks={subtasks} waitingOn={waitingOn} />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label={`Actions for ${task.key}`}
              // Not a drag handle and not a click on the card.
              onPointerDown={(event) => event.stopPropagation()}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => event.stopPropagation()}
              className="absolute top-1.5 right-1.5 opacity-0 group-hover/card:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
            >
              <Ellipsis aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>Move to</DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                {board.workflow.statuses.map((status) => (
                  <DropdownMenuItem
                    key={status.id}
                    disabled={status.id === task.status}
                    icon={<StatusDot category={status.category} />}
                    onSelect={() => void moveTo(task, status.id)}
                  >
                    {status.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuItem icon={<PanelRight aria-hidden />} onSelect={() => onInspect(task)}>
              Show details
            </DropdownMenuItem>
            <DropdownMenuItem
              icon={<Maximize2 aria-hidden />}
              onSelect={() => openTaskDetail(task.id, title(task), { preview: false })}
            >
              Open as tab
            </DropdownMenuItem>
            <DropdownMenuItem
              icon={<Copy aria-hidden />}
              onSelect={() => {
                void navigator.clipboard.writeText(task.key).then(() => toast.success(`Copied ${task.key}`));
              }}
            >
              Copy key
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem icon={<Archive aria-hidden />} onSelect={() => void archive(task)}>
              Archive
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  );
}
