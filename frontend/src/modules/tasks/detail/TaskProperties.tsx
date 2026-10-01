import { useRef, useState, type ReactNode } from "react";
import { Plus, X } from "lucide-react";
import type { Board, Task } from "@/bridge/tasks";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TagInput } from "@/components/ui/tag-input";
import { cn } from "@/lib/utils";
import { openTaskDetail } from "../documents";
import { PriorityIcon, StatusDot } from "../TaskBits";
import { TaskPicker } from "../TaskPicker";
import { moveTo, setFields } from "../taskActions";
import {
  PRIORITIES,
  blockerCandidates,
  dependentCandidates,
  dependentsOf,
  isTerminal,
  parentCandidates,
  priorityLabel,
  relativeTime,
  statusOf,
} from "../taskModel";

/** Radix Select refuses "" as an item value; this stands in for "no priority". */
const NO_PRIORITY = "none";

/**
 * One property: its name on the left, its value on the right. A row per
 * property rather than a label over a field keeps the column about half as
 * tall, and the values line up down one edge where the eye can run along them.
 */
function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-8 items-start gap-3">
      <span className="flex h-8 w-24 shrink-0 items-center text-caption-11 text-text-tertiary">{label}</span>
      <div className="flex min-w-0 flex-1 flex-col justify-center">{children}</div>
    </div>
  );
}

/**
 * A control that reads as a value until it is pointed at: no box at rest, the
 * state layer on hover, the focus ring when it is being edited. A column of
 * eight bordered fields reads as a form to fill in; this reads as facts.
 */
const INLINE_CONTROL = "h-8 w-full max-w-none border-transparent bg-transparent px-2 hover:bg-state-soft";

/**
 * A text field that writes on Enter or on leaving it, never per keystroke —
 * every write is a vault item update and a `tasks:changed` for every open
 * view of the board. Shows the saved value whenever it is not being edited.
 */
function CommitInput({
  value,
  onCommit,
  placeholder,
  label,
  mono = false,
}: {
  value: string;
  onCommit: (next: string) => void;
  placeholder: string;
  label: string;
  mono?: boolean;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  // Escape leaves through a blur, and the blur handler still sees the draft
  // in its closure — so the cancel is recorded where it can read it.
  const cancelled = useRef(false);
  return (
    <Input
      inputSize="sm"
      value={draft ?? value}
      placeholder={placeholder}
      aria-label={label}
      onFocus={() => setDraft(value)}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => {
        const next = (draft ?? value).trim();
        setDraft(null);
        if (cancelled.current) {
          cancelled.current = false;
          return;
        }
        if (next !== value) onCommit(next);
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") cancelled.current = true;
        if (event.key === "Enter" || event.key === "Escape") event.currentTarget.blur();
      }}
      className={cn(INLINE_CONTROL, mono && "font-mono")}
    />
  );
}

/** One linked task — a parent or a blocker — with a way to open it and to unlink it. */
function LinkedTask({
  board,
  task,
  missingId,
  onRemove,
  removeLabel,
}: {
  board: Board;
  task: Task | undefined;
  missingId: string;
  onRemove: () => void;
  removeLabel: string;
}) {
  return (
    <div className="group/linked flex h-7 items-center gap-2 rounded-md px-1.5 transition-colors duration-(--motion-fast) hover:bg-surface-raised">
      {task ? (
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          onClick={() => openTaskDetail(task.id, `${task.key} ${task.title}`)}
          onDoubleClick={() => openTaskDetail(task.id, `${task.key} ${task.title}`, { preview: false })}
        >
          <StatusDot category={statusOf(board, task.status)?.category} />
          <span className="shrink-0 text-code-11 text-text-tertiary">{task.key}</span>
          <span
            className={cn(
              "min-w-0 truncate text-body-12",
              isTerminal(board, task.status) ? "text-text-tertiary line-through" : "text-text-primary",
            )}
          >
            {task.title}
          </span>
        </button>
      ) : (
        // A link to a task this board does not list (deleted, archived,
        // moved): say so rather than drop it, so it can still be removed.
        <span className="min-w-0 flex-1 truncate text-body-12 text-text-tertiary">
          Unavailable task <span className="text-code-11">{missingId.slice(0, 6)}</span>
        </span>
      )}
      <Button
        size="icon-xs"
        variant="ghost"
        aria-label={removeLabel}
        onClick={onRemove}
        className="opacity-0 group-hover/linked:opacity-100 focus-visible:opacity-100"
      >
        <X aria-hidden />
      </Button>
    </div>
  );
}

/**
 * Everything about a task that is not its text. Each control writes its one
 * field (setFields, metadata-only — the description is never resealed for a
 * label), optimistically, so the value changes in the frame of the click.
 */
export function TaskProperties({
  task,
  board,
  boardTasks,
  byId,
}: {
  task: Task;
  board: Board;
  boardTasks: readonly Task[];
  byId: ReadonlyMap<string, Task>;
}) {
  const knownStatus = !!statusOf(board, task.status);
  const knownPriority = !task.priority || PRIORITIES.some((p) => p.id === task.priority);
  const parent = task.parentId ? byId.get(task.parentId) : undefined;
  const parents = parentCandidates(boardTasks, task);
  const hasChildren = boardTasks.some((candidate) => candidate.parentId === task.id);
  const blockedBy = task.blockedBy ?? [];
  const dependents = dependentsOf(boardTasks, task.id);

  return (
    <div className="flex flex-col gap-1">
      <Property label="Status">
        <Select value={task.status} onValueChange={(to) => void moveTo(task, to)}>
          <SelectTrigger size="sm" aria-label="Status" className={INLINE_CONTROL}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {board.workflow.statuses.map((status) => (
              <SelectItem key={status.id} value={status.id}>
                <span className="flex items-center gap-2">
                  <StatusDot category={status.category} />
                  {status.name}
                </span>
              </SelectItem>
            ))}
            {knownStatus ? null : (
              // The workflow lost this column; the task still says it is here.
              <SelectItem value={task.status} disabled>
                {task.status}
              </SelectItem>
            )}
          </SelectContent>
        </Select>
      </Property>

      <Property label="Priority">
        <Select
          value={task.priority || NO_PRIORITY}
          onValueChange={(value) => void setFields(task, { priority: value === NO_PRIORITY ? "" : value })}
        >
          <SelectTrigger size="sm" aria-label="Priority" className={INLINE_CONTROL}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NO_PRIORITY}>
              <span className="flex items-center gap-2">
                <PriorityIcon priority="" />
                No priority
              </span>
            </SelectItem>
            {PRIORITIES.map((priority) => (
              <SelectItem key={priority.id} value={priority.id}>
                <span className="flex items-center gap-2">
                  <PriorityIcon priority={priority.id} />
                  {priority.label}
                </span>
              </SelectItem>
            ))}
            {knownPriority ? null : (
              <SelectItem value={task.priority}>
                <span className="flex items-center gap-2">
                  <PriorityIcon priority={task.priority} />
                  {priorityLabel(task.priority)}
                </span>
              </SelectItem>
            )}
          </SelectContent>
        </Select>
      </Property>

      <Property label="Assignee">
        <CommitInput
          label="Assignee"
          value={task.assignee}
          placeholder="Nobody"
          onCommit={(assignee) => void setFields(task, { assignee })}
        />
      </Property>

      <Property label="Labels">
        <TagInput
          value={task.labels ?? []}
          onChange={(labels) => void setFields(task, { labels })}
          placeholder="Add a label"
          aria-label="Labels"
          className="min-h-8 max-w-none border-transparent bg-transparent px-1.5 hover:bg-state-soft"
        />
      </Property>

      <Property label="Parent">
        {parent || task.parentId ? (
          <LinkedTask
            board={board}
            task={parent}
            missingId={task.parentId}
            removeLabel="Detach from parent"
            onRemove={() => void setFields(task, { parentId: "" })}
          />
        ) : hasChildren ? (
          // One level: a task with subtasks cannot become one.
          <span className="flex h-8 items-center px-2 text-body-12 text-text-tertiary">Has subtasks of its own</span>
        ) : (
          <TaskPicker
            board={board}
            candidates={parents}
            placeholder="Find a parent task"
            empty="No task can be its parent"
            onPick={(picked) => void setFields(task, { parentId: picked.id })}
          >
            <Button size="xs" variant="ghost" className="h-8 w-fit px-2 text-text-tertiary">
              <Plus aria-hidden />
              Set parent
            </Button>
          </TaskPicker>
        )}
      </Property>

      {/* A dependency is blocked_by, read from both ends: what this task
          waits on, and what waits on it. Both pickers leave out the choices
          that would close a loop — the service refuses those anyway. */}
      <Property label="Depends on">
        {blockedBy.length > 0 ? (
          <div className="flex flex-col">
            {blockedBy.map((id) => (
              <LinkedTask
                key={id}
                board={board}
                task={byId.get(id)}
                missingId={id}
                removeLabel="Remove dependency"
                onRemove={() =>
                  void setFields(task, { blockedBy: blockedBy.filter((candidate) => candidate !== id) })
                }
              />
            ))}
          </div>
        ) : null}
        <TaskPicker
          board={board}
          candidates={blockerCandidates(boardTasks, task)}
          placeholder="Find a task this one waits on"
          empty="No task it could wait on"
          onPick={(picked) => void setFields(task, { blockedBy: [...blockedBy, picked.id] })}
        >
          <Button size="xs" variant="ghost" className="h-8 w-fit px-2 text-text-tertiary">
            <Plus aria-hidden />
            Add dependency
          </Button>
        </TaskPicker>
      </Property>

      <Property label="Blocks">
        {dependents.length > 0 ? (
          <div className="flex flex-col">
            {dependents.map((dependent) => (
              <LinkedTask
                key={dependent.id}
                board={board}
                task={dependent}
                missingId={dependent.id}
                removeLabel={`Stop ${dependent.key} waiting on this`}
                onRemove={() =>
                  void setFields(dependent, {
                    blockedBy: (dependent.blockedBy ?? []).filter((candidate) => candidate !== task.id),
                  })
                }
              />
            ))}
          </div>
        ) : null}
        <TaskPicker
          board={board}
          candidates={dependentCandidates(boardTasks, task)}
          placeholder="Find a task that waits on this one"
          empty="No task could wait on this one"
          onPick={(picked) =>
            void setFields(picked, { blockedBy: [...(picked.blockedBy ?? []), task.id] })
          }
        >
          <Button size="xs" variant="ghost" className="h-8 w-fit px-2 text-text-tertiary">
            <Plus aria-hidden />
            Add blocked task
          </Button>
        </TaskPicker>
      </Property>

      <Property label="Branch">
        <CommitInput
          label="Branch"
          value={task.branch}
          placeholder="No branch"
          mono
          onCommit={(branch) => void setFields(task, { branch })}
        />
      </Property>

      {task.planNodeId ? (
        <Property label="Plan node">
          <span className="flex h-8 items-center truncate px-2 text-code-11 text-text-secondary">{task.planNodeId}</span>
        </Property>
      ) : null}

      <div className="mt-2 flex flex-col gap-1 border-t border-border-subtle pt-3 text-caption-11 text-text-tertiary tabular-nums">
        <span title={task.createdAt}>Created {relativeTime(task.createdAt)}</span>
        <span title={task.updatedAt}>Updated {relativeTime(task.updatedAt)}</span>
      </div>
    </div>
  );
}
