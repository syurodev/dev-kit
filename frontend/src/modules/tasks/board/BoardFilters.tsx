import { Bot, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Toggle } from "@/components/ui/toggle";
import { PriorityIcon } from "../TaskBits";
import { EMPTY_FILTER, PRIORITIES, isFiltering, type TaskFilter } from "../taskModel";

/** Radix Select refuses "" as an item value. */
const ANY = "any";

/**
 * The board's filter row. Every control changes a local value and the board
 * filters its loaded tasks — nothing here calls the bridge, so a key press is
 * answered in the same frame (the list itself reads a deferred copy of the
 * query, smoothness 6.5).
 */
export function BoardFilters({
  filter,
  labels,
  onChange,
  shown,
}: {
  filter: TaskFilter;
  labels: readonly string[];
  onChange: (next: TaskFilter) => void;
  /** Top-level cards still on the board after filtering. */
  shown: number;
}) {
  const filtering = isFiltering(filter);
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
      <label className="flex h-(--control-height-sm) w-60 items-center gap-2 rounded-md border border-status-info/16 bg-white/6 px-2.5 transition-colors focus-within:border-status-info">
        <Search aria-hidden className="size-(--icon-14) shrink-0 text-text-tertiary" />
        <input
          value={filter.query}
          onChange={(event) => onChange({ ...filter, query: event.target.value })}
          onKeyDown={(event) => {
            if (event.key === "Escape") onChange({ ...filter, query: "" });
          }}
          placeholder="Filter by key, title or label"
          aria-label="Filter tasks"
          className="min-w-0 flex-1 bg-transparent text-body-12 text-text-primary outline-none placeholder:text-text-tertiary"
        />
      </label>
      <Select
        value={filter.priority || ANY}
        onValueChange={(value) => onChange({ ...filter, priority: value === ANY ? "" : value })}
      >
        <SelectTrigger size="sm" aria-label="Priority filter" className="w-36">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>Any priority</SelectItem>
          {PRIORITIES.map((priority) => (
            <SelectItem key={priority.id} value={priority.id}>
              <span className="flex items-center gap-2">
                <PriorityIcon priority={priority.id} />
                {priority.label}
              </span>
            </SelectItem>
          ))}
          <SelectItem value="none">No priority</SelectItem>
        </SelectContent>
      </Select>
      {labels.length > 0 || filter.label ? (
        <Select
          value={filter.label || ANY}
          onValueChange={(value) => onChange({ ...filter, label: value === ANY ? "" : value })}
        >
          <SelectTrigger size="sm" aria-label="Label filter" className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ANY}>Any label</SelectItem>
            {labels.map((label) => (
              <SelectItem key={label} value={label}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
      <Toggle
        size="sm"
        pressed={filter.heldByAgent}
        onPressedChange={(heldByAgent) => onChange({ ...filter, heldByAgent })}
        aria-label="Only tasks an agent holds"
      >
        <Bot aria-hidden />
        Agent
      </Toggle>
      {/* Always mounted, invisible when idle: the row keeps its width when a
          filter turns on (smoothness 3.2). */}
      <span
        className={
          filtering
            ? "flex items-center gap-1 text-caption-11 text-text-tertiary tabular-nums"
            : "invisible flex items-center gap-1 text-caption-11 tabular-nums"
        }
        aria-hidden={!filtering}
      >
        {shown} shown
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Clear filters"
          tabIndex={filtering ? 0 : -1}
          onClick={() => onChange(EMPTY_FILTER)}
        >
          <X aria-hidden />
        </Button>
      </span>
    </div>
  );
}
