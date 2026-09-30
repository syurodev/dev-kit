import { useDeferredValue, useState, type ReactElement } from "react";
import { Search } from "lucide-react";
import type { Board, Task } from "@/bridge/tasks";
import { Command, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { StatusDot } from "./TaskBits";
import { matchesTask } from "./tasksTree";
import { statusOf } from "./taskModel";

/** More than a person scans; the search box is how the rest are reached. */
const SHOWN = 50;

/**
 * Pick one task of a board — a parent, a blocker. A searchable list rather
 * than a Select: a board holds hundreds of tasks over its life, and a menu of
 * hundreds is scrolled, not read.
 *
 * The box updates on every key; the list filters on a deferred copy of it, so
 * typing never waits on the list (smoothness 6.5). Filtering is local — the
 * candidates are the board's loaded summaries — so nothing waits on a bridge
 * call either.
 */
export function TaskPicker({
  board,
  candidates,
  onPick,
  placeholder,
  empty,
  children,
}: {
  board: Board;
  candidates: readonly Task[];
  onPick: (task: Task) => void;
  placeholder: string;
  empty: string;
  /** The trigger. */
  children: ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const needle = useDeferredValue(query).trim();
  const matches = candidates.filter((task) => matchesTask(task, needle)).slice(0, SHOWN);

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent align="start" className="w-80 gap-0 p-0">
        {/* Transparent on the popover's own glass, the way the composer's
            lists sit inside theirs: one float surface, not two stacked. */}
        <Command shouldFilter={false} className="w-full max-w-none rounded-none border-0 bg-transparent">
          <div className="flex h-9 items-center gap-2 border-b border-border-subtle px-3">
            <Search className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
            <input
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={placeholder}
              aria-label={placeholder}
              className="min-w-0 flex-1 bg-transparent text-body-13 text-text-primary outline-none placeholder:text-text-tertiary"
            />
          </div>
          <CommandList className="max-h-64 gap-0 p-1">
            {matches.length === 0 ? (
              <p className="flex h-8 items-center px-2 text-body-13 text-text-tertiary">{empty}</p>
            ) : (
              matches.map((task) => (
                <CommandItem
                  key={task.id}
                  value={task.id}
                  indicator={false}
                  onSelect={() => {
                    onPick(task);
                    setOpen(false);
                    setQuery("");
                  }}
                  className="h-8 gap-2 rounded-sm px-2"
                >
                  <StatusDot category={statusOf(board, task.status)?.category} />
                  <span className="shrink-0 text-code-11 text-text-tertiary">{task.key}</span>
                  <span className="min-w-0 flex-1 truncate">{task.title}</span>
                </CommandItem>
              ))
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
