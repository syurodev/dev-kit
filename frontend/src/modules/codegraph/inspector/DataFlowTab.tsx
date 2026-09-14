import { useEffect, useState } from "react";
import { ArrowDownToLine, ArrowUpFromLine, CornerDownRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import {
  listCodeGraphFields,
  traceCodeGraphDataFlow,
  type CodeGraphFieldRef,
  type CodeGraphDataFlowTrace,
} from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import { shortLabel } from "../graph/labels";
import { toDataFlowRows } from "./dataFlowRows";

const KIND_ICON = {
  read: ArrowUpFromLine,
  write: ArrowDownToLine,
  call: CornerDownRight,
} as const;

/**
 * Two states in one tab, matching the Figma `Node Data Flow` variants: a node
 * whose class touches no field at all (NoAccess), and a picked field's trace
 * (Traced). The picker is not optional — TraceDataFlow needs a field name and
 * there is no sensible default to guess.
 */
export function DataFlowTab({ nodeId, onJump }: { nodeId: string; onJump: (id: string) => void }) {
  const [fields, setFields] = useState<CodeGraphFieldRef[] | null>(null);
  const [picked, setPicked] = useState<CodeGraphFieldRef | null>(null);
  const [trace, setTrace] = useState<CodeGraphDataFlowTrace | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    setFields(null);
    setPicked(null);
    setTrace(null);
    setError("");
    listCodeGraphFields(nodeId)
      .then((f) => live && setFields(f))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [nodeId]);

  useEffect(() => {
    if (!picked) return;
    let live = true;
    setTrace(null);
    traceCodeGraphDataFlow(picked.classId, picked.fieldName)
      .then((t) => live && setTrace(t))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [picked]);

  if (error) return <p className="p-3 text-body-12 text-status-danger">{error}</p>;
  if (!fields) return <p className="p-3 text-body-12 text-text-tertiary">Loading fields…</p>;

  if (fields.length === 0) {
    return (
      <p className="p-3 text-body-12 text-text-tertiary">
        This node touches no fields, so there is nothing to trace.
      </p>
    );
  }

  const rows = trace ? toDataFlowRows(trace.steps) : [];

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex flex-wrap gap-1 px-3 pt-2">
        {fields.map((f) => (
          <Button
            key={`${f.classId}.${f.fieldName}`}
            size="sm"
            variant={picked?.fieldName === f.fieldName ? "secondary" : "ghost"}
            className="h-6 gap-1 px-2 text-code-11"
            onClick={() => setPicked(f)}
          >
            {f.fieldName}
            <span className="text-text-tertiary">
              {f.reads}r/{f.writes}w
            </span>
          </Button>
        ))}
      </div>

      {!picked && (
        <p className="px-3 pb-3 text-body-12 text-text-tertiary">
          Pick a field to trace it through the call graph.
        </p>
      )}

      {picked && !trace && <p className="px-3 pb-3 text-body-12 text-text-tertiary">Tracing…</p>}

      {trace && (
        <>
          <p className="px-3 text-caption-11 text-text-tertiary">
            Ordered by <span className="font-mono">{trace.orderedBy}</span> — line order within each
            method, not execution order.
          </p>
          <ScrollArea className="min-h-0 flex-1">
            <ul className="flex flex-col gap-0.5 px-3 pb-3">
              {rows.map((row) =>
                row.type === "header" ? (
                  <li
                    key={row.key}
                    style={{ paddingLeft: `calc(${row.depth} * var(--space-flow-indent))` }}
                    className="mt-1.5 flex items-baseline gap-1.5 text-label-12 text-text-primary"
                  >
                    <span className="truncate">{shortLabel(row.label, "method")}</span>
                    <span className="truncate text-code-11 text-text-tertiary">
                      {row.filePath}
                    </span>
                  </li>
                ) : (
                  <li key={row.key} style={{ paddingLeft: `calc(${row.depth} * var(--space-flow-indent) + var(--space-flow-leaf))` }}>
                    <button
                      type="button"
                      disabled={!row.targetId}
                      onClick={() => row.targetId && onJump(row.targetId)}
                      className={cn(
                        "flex w-full items-center gap-1.5 rounded px-1 py-0.5 text-left text-code-11",
                        row.targetId
                          ? "text-text-secondary hover:bg-surface-raised"
                          : "cursor-default text-text-tertiary",
                      )}
                    >
                      {(() => {
                        const Icon = KIND_ICON[row.kind];
                        return <Icon size={11} className="shrink-0" />;
                      })()}
                      <span className="shrink-0 tabular-nums">:{row.line}</span>
                      <span className="truncate">{row.wall || row.kind}</span>
                    </button>
                  </li>
                ),
              )}
            </ul>
          </ScrollArea>
        </>
      )}
    </div>
  );
}
