import { useEffect, useMemo, useState } from "react";
import { Info, TriangleAlert } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Item, ItemContent, ItemDescription, ItemGroup, ItemHeader, ItemTitle } from "@/components/ui/item";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
import { describeUsage, focusOnField, splitOnField, toFieldUsage, type FieldUser } from "./dataFlowRows";

const keyOf = (f: CodeGraphFieldRef) => `${f.classId}#${f.fieldName}`;
const className = (id: string) => id.slice(id.lastIndexOf(".") + 1);
const fileName = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/** How the inspected method uses a field, in words. */
function usedHere(f: CodeGraphFieldRef): string {
  if (f.reads > 0 && f.writes > 0) return "read and written here";
  return f.writes > 0 ? "written here" : "read here";
}

/**
 * "Where else is this used?" for the fields the inspected method touches.
 *
 * The tab answers one question per field: which methods change it, which
 * read it, and — in each — the line that does it. It used to show the raw
 * trace (a call-graph walk of `read :42` events), which was accurate and
 * unreadable. The walk survives only as "via", which says how the trace
 * reached a method.
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
  /** Opens a file at a line, beside the flow. */
  onOpenFile?: (path: string, line: number) => void;
}) {
  const [fields, setFields] = useState<CodeGraphFieldRef[] | null>(null);
  const [pickedKey, setPickedKey] = useState("");
  const [trace, setTrace] = useState<CodeGraphDataFlowTrace | null>(null);
  const [error, setError] = useState("");
  const picked = fields?.find((f) => keyOf(f) === pickedKey) ?? null;

  useEffect(() => {
    let live = true;
    setFields(null);
    setPickedKey("");
    setTrace(null);
    setError("");
    listCodeGraphFields(nodeId)
      .then((f) => {
        if (!live) return;
        setFields(f);
        // A field something writes is the more telling first answer.
        const first = f.find((x) => x.writes > 0) ?? f[0];
        if (first) setPickedKey(keyOf(first));
      })
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [nodeId]);

  useEffect(() => {
    const [classId, fieldName] = pickedKey.split("#");
    if (!classId || !fieldName) return;
    let live = true;
    setTrace(null);
    traceCodeGraphDataFlow(classId, fieldName)
      .then((t) => live && setTrace(t))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [pickedKey]);

  const usage = useMemo(() => (trace ? toFieldUsage(trace.steps) : null), [trace]);

  if (error) return <p className="px-5 py-4 text-body-12 text-status-danger">{error}</p>;
  if (!fields) return <p className="px-5 py-4 text-body-12 text-text-tertiary">Reading the fields this method uses…</p>;
  if (fields.length === 0) {
    return (
      <p className="px-5 py-4 text-body-12 text-text-tertiary">
        This method reads and writes no fields, so there is no data to follow from it.
      </p>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 pt-4">
      <div className="flex shrink-0 flex-col gap-1.5 px-5">
        <div className="flex items-center gap-1">
          <span className="text-label-11-caps text-text-tertiary">Field this method uses</span>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label="What this shows"
                className="flex size-5 items-center justify-center rounded-sm text-icon-secondary hover:bg-state-soft"
              >
                <Info className="size-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-64">
              Pick a field this method reads or writes to see every other method in the project that does — and the line
              where it does.
            </TooltipContent>
          </Tooltip>
        </div>
        <Select value={pickedKey} onValueChange={setPickedKey}>
          <SelectTrigger size="sm" className="max-w-none font-mono">
            <SelectValue>{picked?.fieldName}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {fields.map((f) => (
              <SelectItem key={keyOf(f)} value={keyOf(f)} className="font-mono">
                {f.fieldName}
                <span className="ml-2 font-sans text-caption-11 text-text-tertiary">
                  {className(f.classId)} · {usedHere(f)}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {!usage || !picked ? (
        <p className="px-5 text-body-12 text-text-tertiary">Finding where {picked?.fieldName} is used…</p>
      ) : (
        // Radix lays the viewport's content out as a table, which grows to
        // its widest line — a long line of code pushed every card past the
        // panel's edge instead of truncating. As a block it takes the panel's.
        <ScrollArea className="min-h-0 flex-1 [&_[data-slot=scroll-area-viewport]>div]:block!">
          <div className="flex flex-col gap-4 px-5 pb-4">
            <p className="text-body-13 text-text-secondary">
              {describeUsage(picked.fieldName, usage)}{" "}
              <span className="text-text-tertiary">Declared in {className(picked.classId)}.</span>
            </p>

            <UserSection
              title="Written by"
              users={usage.writers}
              field={picked.fieldName}
              current={nodeId}
              tone="write"
              onJump={onJump}
              canJump={canJump}
              onOpenFile={onOpenFile}
            />
            <UserSection
              title="Read by"
              users={usage.readers}
              field={picked.fieldName}
              current={nodeId}
              tone="read"
              onJump={onJump}
              canJump={canJump}
              onOpenFile={onOpenFile}
            />

            {usage.gaps.length > 0 ? (
              <Alert variant="warning" className="h-auto min-h-0 items-start">
                <TriangleAlert />
                <AlertDescription className="text-body-12">
                  The trace could not follow {usage.gaps.length === 1 ? "one call" : `${usage.gaps.length} calls`} (
                  {[...new Set(usage.gaps.map((g) => g.reason.split(" — ")[0]))].join(", ")}), so some uses may be
                  missing.
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        </ScrollArea>
      )}
    </div>
  );
}

function UserSection({
  title,
  users,
  field,
  current,
  tone,
  onJump,
  canJump,
  onOpenFile,
}: {
  title: string;
  users: FieldUser[];
  field: string;
  current: string;
  tone: "read" | "write";
  onJump: (id: string) => void;
  canJump: (id: string) => boolean;
  onOpenFile?: (path: string, line: number) => void;
}) {
  if (users.length === 0) return null;
  return (
    <section className="flex flex-col gap-1.5">
      <h3 className="flex items-center gap-1.5 text-label-11-caps text-text-tertiary">
        {title}
        <Badge variant="secondary" className="h-4 px-1.5 text-code-11">
          {users.length}
        </Badge>
      </h3>
      {/* Rows, not cards: a bordered card around a code well was a box in a
          box. The row lines up with the section title (-mx-2 undoes its
          padding) and only lights up on hover. */}
      <ItemGroup className="-mx-2 gap-0.5">
        {users.map((u) => (
          <Item key={u.nodeId} size="xs" className="gap-1 px-2 py-1.5 hover:bg-state-soft">
            <ItemHeader>
              <ItemContent className="min-w-0 gap-0.5">
                <ItemTitle className="w-full min-w-0 gap-1.5">
                  {canJump(u.nodeId) && u.nodeId !== current ? (
                    <button
                      type="button"
                      onClick={() => onJump(u.nodeId)}
                      title={`${u.label} — inspect`}
                      className="min-w-0 truncate text-left hover:underline"
                    >
                      {shortLabel(u.label, "method")}
                    </button>
                  ) : (
                    <span className="min-w-0 truncate" title={u.label}>
                      {shortLabel(u.label, "method")}
                    </span>
                  )}
                  {u.nodeId === current ? (
                    <Badge variant="secondary" className="h-4 shrink-0 px-1.5 text-caption-11">
                      this method
                    </Badge>
                  ) : null}
                </ItemTitle>
                <ItemDescription className="truncate text-code-11 text-text-tertiary" title={u.filePath}>
                  {fileName(u.filePath)}
                  {u.via ? <span className="font-sans"> · via {shortLabel(u.via, "method")}</span> : null}
                </ItemDescription>
              </ItemContent>
            </ItemHeader>
            <ul className="flex min-w-0 basis-full flex-col gap-px">
              {u.accesses.map((a) => (
                <li key={a.line} className="min-w-0">
                  <button
                    type="button"
                    disabled={!onOpenFile}
                    onClick={() => onOpenFile?.(u.filePath, a.line)}
                    title={`Open ${u.filePath}:${a.line}`}
                    className="flex w-full min-w-0 items-baseline gap-2 rounded-sm bg-surface-well px-2 py-1 text-left text-code-11 enabled:hover:bg-state-soft"
                  >
                    <span className="shrink-0 text-text-tertiary tabular-nums">:{a.line}</span>
                    <span className="min-w-0 truncate text-text-secondary" title={a.code}>
                      {a.code
                        ? splitOnField(focusOnField(a.code, field), field).map((p, i) =>
                            p.hit ? (
                              <mark
                                key={i}
                                className={cn(
                                  "bg-transparent font-semibold",
                                  tone === "write" ? "text-status-warning" : "text-status-info",
                                )}
                              >
                                {p.text}
                              </mark>
                            ) : (
                              <span key={i}>{p.text}</span>
                            ),
                          )
                        : tone === "write"
                          ? "writes it"
                          : "reads it"}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </Item>
        ))}
      </ItemGroup>
    </section>
  );
}
