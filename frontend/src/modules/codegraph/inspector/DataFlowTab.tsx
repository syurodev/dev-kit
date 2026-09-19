import { useEffect, useMemo, useState, type ReactNode } from "react";
import { CornerDownRight, Info, TriangleAlert } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  listCodeGraphFields,
  traceCodeGraphDataFlow,
  type CodeGraphFieldRef,
  type CodeGraphDataFlowTrace,
} from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import { shortLabel } from "../graph/labels";
import {
  filterFlow,
  summarizeFlow,
  toDataFlowGroups,
  type DataFlowEvent,
  type DataFlowFilter,
  type DataFlowGroup,
} from "./dataFlowRows";

const CHIP = "inline-flex h-6 shrink-0 items-center gap-1.5 rounded-sm border px-2 text-code-11";
const PILL = "inline-flex h-5 max-w-full items-center gap-1 rounded-sm px-1.5 text-code-11";

/** The field whose trace opens first: one something writes, else the first. */
function firstField(fields: CodeGraphFieldRef[]): CodeGraphFieldRef | null {
  return fields.find((f) => f.writes > 0) ?? fields[0] ?? null;
}

const fileName = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/**
 * Where a field of this node's class is read and written, method by method.
 *
 * Opens on a field — one that something writes, when there is one — rather
 * than an empty "pick a field": a trace is bounded and cheap, and the empty
 * state was a click everybody made. The summary doubles as the filter, since
 * "who writes this?" is the question a trace is usually opened for.
 */
export function DataFlowTab({
  nodeId,
  onJump,
  canJump = () => true,
  onOpenFile,
}: {
  nodeId: string;
  onJump: (id: string) => void;
  /** Whether a node is on the canvas, so jumping to it can show it. */
  canJump?: (id: string) => boolean;
  /** Opens the method's file at the line, beside the flow. */
  onOpenFile?: (path: string, line: number) => void;
}) {
  const [fields, setFields] = useState<CodeGraphFieldRef[] | null>(null);
  const [picked, setPicked] = useState<CodeGraphFieldRef | null>(null);
  const [trace, setTrace] = useState<CodeGraphDataFlowTrace | null>(null);
  const [filter, setFilter] = useState<DataFlowFilter>("all");
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    setFields(null);
    setPicked(null);
    setTrace(null);
    setError("");
    listCodeGraphFields(nodeId)
      .then((f) => {
        if (!live) return;
        setFields(f);
        setPicked(firstField(f));
      })
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [nodeId]);

  useEffect(() => {
    if (!picked) return;
    let live = true;
    setTrace(null);
    setFilter("all");
    traceCodeGraphDataFlow(picked.classId, picked.fieldName)
      .then((t) => live && setTrace(t))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [picked]);

  const groups = useMemo(() => (trace ? toDataFlowGroups(trace.steps) : []), [trace]);
  const summary = useMemo(() => summarizeFlow(groups), [groups]);
  const shown = useMemo(() => filterFlow(groups, filter), [groups, filter]);

  if (error) return <p className="px-5 py-4 text-body-12 text-status-danger">{error}</p>;
  if (!fields) return <p className="px-5 py-4 text-body-12 text-text-tertiary">Reading the fields it touches…</p>;
  if (fields.length === 0) {
    return (
      <p className="px-5 py-4 text-body-12 text-text-tertiary">
        This node touches no fields, so there is no data to follow.
      </p>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 pt-4">
      <section className="flex shrink-0 flex-col gap-1.5 px-5">
        <span className="text-label-11-caps text-text-tertiary">Field</span>
        <div className="flex flex-wrap gap-1" role="radiogroup" aria-label="Field to trace">
          {fields.map((f) => {
            const on = picked?.classId === f.classId && picked.fieldName === f.fieldName;
            return (
              <button
                key={`${f.classId}.${f.fieldName}`}
                type="button"
                role="radio"
                aria-checked={on}
                title={`${f.classId}.${f.fieldName} — read ${f.reads}×, written ${f.writes}×`}
                onClick={() => setPicked(f)}
                className={cn(
                  CHIP,
                  on
                    ? "border-(--border-default) bg-surface-raised text-text-primary"
                    : "border-border-subtle text-text-secondary hover:bg-state-soft",
                )}
              >
                <span className="max-w-40 truncate">{f.fieldName}</span>
                {f.writes > 0 ? <span className="text-status-warning">W{f.writes}</span> : null}
                <span className="text-text-tertiary">R{f.reads}</span>
              </button>
            );
          })}
        </div>
      </section>

      {!trace ? (
        <p className="px-5 text-body-12 text-text-tertiary">Tracing {picked?.fieldName}…</p>
      ) : (
        <>
          <div className="flex shrink-0 items-center gap-1 px-5" role="radiogroup" aria-label="Show">
            <FilterChip on={filter === "all"} onClick={() => setFilter("all")}>
              ALL · {groups.length}
            </FilterChip>
            <FilterChip on={filter === "write"} onClick={() => setFilter("write")} disabled={summary.writers === 0}>
              {summary.writers === 0 ? "NEVER WRITTEN" : `WRITTEN IN ${summary.writers}`}
            </FilterChip>
            <FilterChip on={filter === "read"} onClick={() => setFilter("read")} disabled={summary.readers === 0}>
              READ IN {summary.readers}
            </FilterChip>
            <Tooltip>
              <TooltipTrigger asChild>
                <button type="button" className="ml-auto flex size-6 items-center justify-center rounded-sm text-icon-secondary hover:bg-state-soft" aria-label="About the order">
                  <Info className="size-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent className="max-w-64">
                Methods in the order the call graph reaches them; lines in source order within each — not the order they run in.
                {summary.walls > 0 ? ` The trace stopped at ${summary.walls} wall(s), marked below.` : ""}
              </TooltipContent>
            </Tooltip>
          </div>

          <ScrollArea className="min-h-0 flex-1">
            <ol className="flex flex-col px-3 pb-4">
              {shown.map((g) => (
                <MethodRow key={g.key} group={g} onJump={onJump} canJump={canJump} onOpenFile={onOpenFile} />
              ))}
              {shown.length === 0 ? (
                <li className="px-2 py-3 text-body-12 text-text-tertiary">Nothing reaches {picked?.fieldName} this way.</li>
              ) : null}
            </ol>
          </ScrollArea>
        </>
      )}
    </div>
  );
}

function FilterChip({
  on,
  disabled,
  onClick,
  children,
}: {
  on: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={on}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11 disabled:opacity-50",
        on
          ? "border-(--border-default) bg-surface-raised text-text-primary"
          : "border-border-subtle text-text-tertiary enabled:hover:text-text-secondary",
      )}
    >
      {children}
    </button>
  );
}

/**
 * One method: its name and where it lives on the first line, what it did to
 * the field as pills under it. Depth is drawn as guides, the way the tree
 * draws nesting, so a callee sits visibly inside its caller.
 */
function MethodRow({
  group,
  onJump,
  canJump,
  onOpenFile,
}: {
  group: DataFlowGroup;
  onJump: (id: string) => void;
  canJump: (id: string) => boolean;
  onOpenFile?: (path: string, line: number) => void;
}) {
  return (
    <li className="flex">
      {Array.from({ length: group.depth }, (_, i) => (
        <span key={i} aria-hidden className={cn("w-4 shrink-0 border-l border-border-subtle", i === 0 && "ml-2")} />
      ))}
      <div className="flex min-w-0 flex-1 flex-col gap-1 rounded-md px-2 py-1.5 hover:bg-state-soft">
        <div className="flex min-w-0 items-baseline gap-2">
          {canJump(group.nodeId) ? (
            <button
              type="button"
              onClick={() => onJump(group.nodeId)}
              title={`${group.label} — inspect`}
              className="min-w-0 truncate text-left text-body-13 text-text-primary hover:underline"
            >
              {shortLabel(group.label, "method")}
            </button>
          ) : (
            // Off the canvas: the inspector has nothing to show for it; its
            // file is the way in.
            <span title={group.label} className="min-w-0 truncate text-body-13 text-text-primary">
              {shortLabel(group.label, "method")}
            </span>
          )}
          {group.filePath ? (
            <button
              type="button"
              disabled={!onOpenFile}
              onClick={() => onOpenFile?.(group.filePath, group.line)}
              title={`${group.filePath}:${group.line}`}
              className="ml-auto max-w-1/2 shrink-0 truncate text-code-11 text-text-tertiary enabled:hover:text-text-secondary enabled:hover:underline"
            >
              {fileName(group.filePath)}:{group.line}
            </button>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-1">
          {group.events.map((e) => (
            <EventPill key={e.key} event={e} onJump={canJump(e.targetId) ? onJump : undefined} />
          ))}
        </div>
      </div>
    </li>
  );
}

function EventPill({ event: e, onJump }: { event: DataFlowEvent; onJump?: (id: string) => void }) {
  if (e.wall) {
    return (
      <span className={cn(PILL, "bg-(--feedback-danger-soft) text-status-danger")} title={e.wall}>
        <TriangleAlert className="size-3 shrink-0" />
        <span className="truncate">
          :{e.line} {e.wall.split(" — ")[0]}
        </span>
      </span>
    );
  }
  if (e.kind === "call") {
    return (
      <button
        type="button"
        disabled={!e.targetId || !onJump}
        onClick={() => onJump?.(e.targetId)}
        title={e.targetLabel}
        className={cn(PILL, "bg-state-soft text-text-secondary enabled:hover:text-text-primary")}
      >
        <CornerDownRight className="size-3 shrink-0" />
        <span className="truncate">
          :{e.line} {shortLabel(e.targetLabel, "method")}
        </span>
      </button>
    );
  }
  return (
    <span
      className={cn(
        PILL,
        "bg-state-soft",
        e.kind === "write" ? "text-status-warning" : "text-status-info",
      )}
    >
      {e.kind === "write" ? "write" : "read"} :{e.line}
    </span>
  );
}
