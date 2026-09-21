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
 * Only for a node that resolved to real code: realNodeId is empty for
 * not_implemented, and there is nothing on disk yet to diff. The lookup is a
 * neighbor query at depth 0 — CodeGraphGetNeighbors' own contract for
 * "just this node" — since a plan node carries an FQN and a realNodeId, never
 * the file path itself.
 *
 * Reads the work tree via the same gitFilePatch the Git module's own hunk
 * list uses, on whichever repository work-folder sync currently has current —
 * the one Code Graph itself set when this project was opened.
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

  if (!realNodeId) return null;
  if (error) {
    return <p className="text-caption-11 text-status-danger">{error}</p>;
  }
  // Quiet until there is something to say: still resolving the file, or
  // waiting on its patch.
  if (!patch) return null;

  const rows = patchLines(patch);
  if (rows.length === 0) {
    return <p className="text-caption-11 text-text-tertiary">No uncommitted changes to this file.</p>;
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 text-left text-caption-11 text-text-tertiary hover:text-text-secondary"
      >
        <ChevronRight
          aria-hidden="true"
          className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast)", open && "rotate-90")}
        />
        <span className="min-w-0 flex-1 truncate">Git changes</span>
        <span className="shrink-0 tabular-nums">{changeCounts(rows)}</span>
      </button>
      {open ? (
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
