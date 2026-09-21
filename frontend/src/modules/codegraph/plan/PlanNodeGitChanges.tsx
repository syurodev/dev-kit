import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import { DiffLineRow } from "@/components/ui/diff";
import { cn } from "@/lib/utils";
import { getCodeGraphNeighbors } from "@/bridge/codegraph";
import { gitFilePatch } from "@/bridge/git";
import { patchLines } from "@/modules/git/commitView";
import { usePatchHighlight } from "@/modules/git/gitSyntax";
import { parsePatch, type FilePatch } from "@/modules/git/hunks";

/** Long diffs are shown to their first this-many lines — matches Git / Hunk's own preview cap. */
const PREVIEW_LINES = 60;

function changeCounts(rows: ReturnType<typeof patchLines>): string {
  let added = 0;
  let removed = 0;
  for (const row of rows) {
    if (row.kind === "added") added++;
    else if (row.kind === "removed") removed++;
  }
  return `+${added} −${removed}`;
}

/**
 * A plan node's own git diff, folded into its detail strip.
 *
 * "Does the code match the plan" and "what did I actually write" are the same
 * review, asked from two angles — without this, answering the second meant
 * leaving Plan for the Git module and finding the file again by hand.
 *
 * Always renders the row, in one state or another, rather than only once
 * there is a diff to show: a row that appears only sometimes reads as absent
 * everywhere else, and a not_implemented node — most of a fresh plan — used to
 * render nothing at all, which is indistinguishable from the feature not
 * existing. Only the FQN/relink row above it is conditional on that.
 *
 * The lookup is a neighbor query at depth 0 — CodeGraphGetNeighbors' own
 * contract for "just this node" — since a plan node carries an FQN and a
 * realNodeId, never the file path itself. Reads the work tree via the same
 * gitFilePatch the Git module's own hunk list uses, on whichever repository
 * work-folder sync currently has current — the one Code Graph itself set
 * when this project was opened.
 */
export function PlanNodeGitChanges({ realNodeId }: { realNodeId: string }) {
  const [filePath, setFilePath] = useState<string | null>(null);
  const [patch, setPatch] = useState<FilePatch | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);

  useEffect(() => {
    setFilePath(null);
    setPatch(null);
    setError("");
    setOpen(false);
    if (!realNodeId) return;
    let live = true;
    getCodeGraphNeighbors(realNodeId, 0)
      .then((sub) => {
        if (!live) return;
        setFilePath(sub.nodes.find((n) => n.id === realNodeId)?.filePath ?? "");
      })
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [realNodeId]);

  useEffect(() => {
    if (!filePath) return;
    let live = true;
    gitFilePatch(filePath)
      .then((text) => live && setPatch(parsePatch(text)))
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [filePath]);

  const highlight = usePatchHighlight(patch, filePath ?? "");
  const rows = patch ? patchLines(patch) : [];
  const expandable = rows.length > 0;

  const status = !realNodeId
    ? "not implemented yet"
    : error
      ? error
      : !patch
        ? "checking…"
        : rows.length === 0
          ? "up to date"
          : changeCounts(rows);

  return (
    <div className="border-t border-border-subtle pt-1.5">
      <button
        type="button"
        disabled={!expandable}
        onClick={() => setOpen((value) => !value)}
        aria-expanded={expandable ? open : undefined}
        className={cn(
          "flex w-full min-w-0 items-center gap-2 text-left text-caption-11 text-text-tertiary",
          expandable ? "cursor-pointer hover:text-text-secondary" : "cursor-default",
        )}
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 transition-transform duration-(--motion-fast)",
            !expandable && "opacity-0",
            expandable && open && "rotate-90",
          )}
        />
        <span className="min-w-0 flex-1 truncate">Git changes</span>
        <span className={cn("shrink-0 tabular-nums", error && "text-status-danger")}>{status}</span>
      </button>
      {open && expandable ? (
        <div className="mt-2 overflow-x-auto rounded-lg border border-border-subtle bg-surface-well pb-1">
          {rows.slice(0, PREVIEW_LINES).map((row, index) => (
            <DiffLineRow
              key={index}
              row={row}
              segments={row.kind === "removed" ? highlight?.before.get(row.before) : highlight?.after.get(row.after)}
            />
          ))}
          {rows.length > PREVIEW_LINES ? (
            <div className="px-3 py-1 text-code-11 text-text-tertiary">…{rows.length - PREVIEW_LINES} more lines</div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
