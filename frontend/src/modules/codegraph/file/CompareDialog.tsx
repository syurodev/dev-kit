import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DiffLineRow } from "@/components/ui/diff";
import { diffStats, foldContext, wholeFileDiff } from "@/lib/diffHunks";
import { lineDiff } from "@/lib/lineDiff";
import { errorMessage } from "@/bridge/errors";
import { readDiskVersion } from "../fileActions";
import type { FileBuffer } from "../workspace/bufferModel";
import { detectEol } from "../workspace/eol";

/** The line diff is quadratic in the changed middle; past this many cells it is not attempted. */
const MAX_DIFF_CELLS = 4_000_000;

/** Size of the part of two texts that is left once their common head and tail are set aside. */
function middleCells(a: string, b: string): number {
  const x = a.split("\n");
  const y = b.split("\n");
  let head = 0;
  while (head < x.length && head < y.length && x[head] === y[head]) head++;
  let tail = 0;
  while (tail < x.length - head && tail < y.length - head && x[x.length - 1 - tail] === y[y.length - 1 - tail]) tail++;
  return (x.length - head - tail) * (y.length - head - tail);
}

/**
 * The disk's version of a file beside the buffer's, as a diff of what saving
 * would change on disk: removed lines are what disk has and the buffer lacks,
 * added lines the opposite. A two-way comparison — the text the buffer was
 * loaded as is kept, but without the disk's intermediate history it cannot
 * merge the two sides, so deciding is left to the person.
 */
export function CompareDialog({
  open,
  onOpenChange,
  bufferKey,
  buffer,
  onReload,
  onOverwrite,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  bufferKey: string;
  buffer: FileBuffer;
  onReload: () => void;
  onOverwrite: () => void;
}) {
  const [disk, setDisk] = useState<string | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    let live = true;
    setDisk(null);
    setError("");
    readDiskVersion(bufferKey).then(
      (v) => live && (v ? setDisk(detectEol(v.content).text) : setError("The file could not be read from disk — it may have been deleted or is not text.")),
      (e) => live && setError(errorMessage(e)),
    );
    return () => {
      live = false;
    };
  }, [open, bufferKey, buffer.diskRevision]);

  // One diff, computed once per pair of texts — and not at all when the
  // changed middle is so large that the line diff (quadratic in it) would stall
  // the window; the person is told, and can still decide.
  const diff = useMemo(() => {
    if (disk === null) return null;
    if (middleCells(disk, buffer.content) > MAX_DIFF_CELLS) return "too-large" as const;
    const lines = wholeFileDiff(disk, buffer.content, lineDiff);
    return { rows: foldContext(lines, 3), stats: diffStats(lines) };
  }, [disk, buffer.content]);
  const rows = diff && diff !== "too-large" ? diff.rows : [];
  const stats = diff && diff !== "too-large" ? diff.stats : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[80vh] max-w-4xl flex-col">
        <DialogHeader>
          <DialogTitle>Compare with disk</DialogTitle>
          <DialogDescription>
            {buffer.path} — what saving your buffer would change on disk
            {stats ? ` (${stats.added} added, ${stats.removed} removed)` : ""}.
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border-subtle bg-surface-well">
          {error ? (
            <p role="alert" className="p-3 text-body-12 text-status-danger">{error}</p>
          ) : disk === null ? (
            <p role="status" className="p-3 text-body-12 text-text-tertiary">Reading the file…</p>
          ) : diff === "too-large" ? (
            <p role="status" className="p-3 text-body-12 text-text-tertiary">
              These versions differ in too many lines to compare here. Choose Reload from disk to take the disk's version,
              or Overwrite disk to keep yours.
            </p>
          ) : stats && stats.added === 0 && stats.removed === 0 ? (
            <p role="status" className="p-3 text-body-12 text-text-tertiary">Your buffer and the disk have the same text.</p>
          ) : (
            rows.map((row, i) => <DiffLineRow key={i} row={row} />)
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Close
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              onReload();
              onOpenChange(false);
            }}
          >
            Reload from disk
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              onOverwrite();
              onOpenChange(false);
            }}
          >
            Overwrite disk
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
