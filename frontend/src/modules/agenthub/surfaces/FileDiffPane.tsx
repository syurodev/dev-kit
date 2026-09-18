import * as React from "react";
import { Maximize2 } from "lucide-react";

import { DiffLineRow, DiffPane, DiffSplitPanes } from "@/components/ui/diff";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { changedRuns, diffStats, foldContext, sourceLines, splitPairs, wholeFileDiff } from "@/lib/diffHunks";
import { lineDiff } from "@/modules/notes/noteDiff";
import {
  HIGHLIGHT_LIMIT,
  languageForPath,
  loadParser,
  tokenizeLines,
  type SyntaxSegment,
} from "@/modules/agenthub/transcript/syntax";

/** One file's two versions, however the surface came by them. */
export type FileDiffView = {
  path: string;
  before: string;
  after: string;
  /** Not previewable: no lines are drawn and the header says which it is. */
  binary?: boolean;
  truncated?: boolean;
  /** There is no file yet — the edit creates it. */
  beforeMissing?: boolean;
  /** The agent's snippet no longer matches the file it means to change. */
  stale?: boolean;
};

/** The path as the project names it: relative inside the session's folder. */
export function displayPath(path: string, projectPath?: string): string {
  if (projectPath && path.startsWith(`${projectPath.replace(/\/$/, "")}/`)) {
    return path.slice(projectPath.replace(/\/$/, "").length + 1);
  }
  return path;
}

/**
 * Both versions of the file, coloured, a line at a time — the same Lezer parse
 * the transcript's code blocks use, picked by the file's extension.
 *
 * In an effect, after the plain diff has painted: the text is the same either
 * way, so the colours arrive without moving a line. A file too large to parse
 * (or a language with no grammar) simply stays plain.
 */
function useDiffHighlight(diff: FileDiffView): { before: SyntaxSegment[][]; after: SyntaxSegment[][] } | null {
  const [lines, setLines] = React.useState<{ before: SyntaxSegment[][]; after: SyntaxSegment[][] } | null>(null);
  const language = languageForPath(diff.path);
  const { before, after } = diff;

  React.useEffect(() => {
    setLines(null);
    if (!language || before.length + after.length > HIGHLIGHT_LIMIT) return;
    let cancelled = false;
    loadParser(language)
      .then((parser) => {
        if (cancelled) return;
        setLines({ before: tokenizeLines(before, parser), after: tokenizeLines(after, parser) });
      })
      .catch(() => {
        // No grammar, or it failed to load: the plain diff is already there.
      });
    return () => {
      cancelled = true;
    };
  }, [language, before, after]);

  return lines;
}

/**
 * One file's diff as an 08 Diff pane — the same thing wherever a file changes:
 * a permission asking to make the edit, the tool call that made it, a turn's
 * changed files.
 *
 * The pane shows the changes with three lines of context, folds the rest, and
 * caps its height; expanding opens the whole file as two panes (Figma 08 Diff,
 * example D), where `actions` — a permission's Deny / Allow — sits at the foot
 * so a diff worth reading in full can be answered without closing it.
 */
export function FileDiffPane({
  diff,
  projectPath,
  actions,
  labels,
  headless = false,
  testIdPrefix = "file-diff",
  className,
}: {
  diff: FileDiffView;
  projectPath?: string;
  /** Buttons repeated at the foot of the expanded view. */
  actions?: React.ReactNode;
  /** The two panes' headers when expanded; defaults suit an edit already made. */
  labels?: { before: string; after: string };
  /**
   * Drops the pane header, for a surface that is already the header: a turn's
   * changed-file row names the file and counts its lines, and repeating both
   * inside the drawer it opens says everything twice.
   */
  headless?: boolean;
  testIdPrefix?: string;
  className?: string;
}) {
  const lines = React.useMemo(
    () => (diff.binary || diff.truncated ? [] : wholeFileDiff(diff.before, diff.after, lineDiff)),
    [diff],
  );
  const sourced = React.useMemo(() => sourceLines(lines), [lines]);
  const pairs = React.useMemo(() => splitPairs(sourced), [sourced]);
  const runs = React.useMemo(() => changedRuns(pairs), [pairs]);
  const rows = React.useMemo(() => foldContext(sourced), [sourced]);
  const [expanded, setExpanded] = React.useState(false);
  const { added, removed } = diffStats(lines);
  const highlight = useDiffHighlight(diff);

  const name = displayPath(diff.path, projectPath);
  // Same lookup in the pane and in the dialog: which side a line came from
  // decides which file's colours it takes.
  const segmentsFor = (row: (typeof rows)[number]) =>
    row.kind === "gap" ? undefined : row.kind === "removed" ? highlight?.before[row.before] : highlight?.after[row.after];

  const counts = (
    <>
      {added > 0 ? <span className="text-(--brand-text)">{`+${added}`}</span> : null}
      {removed > 0 ? <span className="text-status-danger">{`−${removed}`}</span> : null}
    </>
  );

  // Nothing to fold, nothing to expand: a diff of a file against itself is a
  // line saying so, not the file drawn twice.
  const unchanged = added === 0 && removed === 0;

  const note = diff.binary
    ? "binary file"
    : diff.truncated
      ? "too large to preview"
      : diff.beforeMissing
        ? "new file"
        : diff.stale
          ? "file changed since the agent read it"
          : unchanged
            ? "no textual change"
            : null;

  // The pane shows the change; this shows the file. Every line, unfolded.
  const dialog = (
    <Dialog open={expanded} onOpenChange={setExpanded}>
      {/* No fixed height: DialogContent lays its children out in a h-fit
          body, so the dialog is as tall as the diff up to the cap below. */}
      <DialogContent className="w-[min(1100px,92vw)] max-w-none gap-3 p-4 sm:max-w-none">
        <DialogHeader className="flex-row items-center gap-3 space-y-0 pr-8">
          <DialogTitle className="min-w-0 flex-1 truncate text-code-12 text-text-primary">{name}</DialogTitle>
          <span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">{counts}</span>
        </DialogHeader>
        {/* Two panes — Figma 08 Diff, example D. The file beside what it
            becomes, changed lines facing each other. The right side takes
            the active emphasis: it is the one being decided on, or the one
            that won. */}
        <div className="max-h-[70vh] overflow-auto select-text">
          <DiffSplitPanes
            pairs={pairs}
            runs={runs}
            leftLabel={labels?.before ?? (diff.beforeMissing ? "Not there yet" : "Before")}
            rightLabel={labels?.after ?? "After"}
            rightMeta={<span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">{counts}</span>}
            segmentsFor={(line, which) =>
              which === "left" ? highlight?.before[line.before] : highlight?.after[line.after]
            }
          />
        </div>
        {actions ? <div className="flex items-center justify-end gap-2">{actions}</div> : null}
      </DialogContent>
    </Dialog>
  );

  const expand = unchanged ? null : (
    <ActionTooltip label="Review the whole file">
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        data-testid={`${testIdPrefix}-expand`}
        aria-label={`Review the whole diff of ${name}`}
        onClick={() => setExpanded(true)}
        className="text-text-tertiary"
      >
        <Maximize2 aria-hidden="true" />
      </Button>
    </ActionTooltip>
  );

  const body = unchanged
    ? null
    : rows.map((row, i) => (
        <DiffLineRow
          key={i}
          row={row}
          segments={segmentsFor(row)}
          // The side the line is from: a removed line's number is the one it
          // has in the file as it was, everything else the one it has after.
          lineNumber={row.kind === "gap" ? undefined : (row.kind === "removed" ? row.before : row.after) + 1}
        />
      ));

  if (headless) {
    return (
      <div
        data-slot="diff-pane"
        data-headless="true"
        className={cn("relative w-full min-w-0 overflow-clip rounded-lg border border-border-subtle bg-(--tint-veil)/3 py-2", className)}
      >
        <div className="max-h-60 w-full overflow-auto select-text">
          {body ?? <p className="px-4 text-caption-11 text-text-tertiary">{note}</p>}
        </div>
        {/* Over the first line's right end, where the pane header's copy sits. */}
        {expand ? <div className="absolute top-1 right-1">{expand}</div> : null}
        {dialog}
      </div>
    );
  }

  return (
    <DiffPane
      label={name || "diff"}
      caps={false}
      className={className}
      bodyClassName="max-h-60"
      meta={
        <span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">
          {note ? <span className={diff.stale ? "text-status-warning" : "text-text-tertiary"}>{note}</span> : null}
          {counts}
          {expand}
        </span>
      }
    >
      {unchanged
        ? null
        : rows.map((row, i) => (
        <DiffLineRow
          key={i}
          row={row}
          segments={segmentsFor(row)}
          // The side the line is from: a removed line's number is the one it
          // has in the file as it was, everything else the one it has after.
            lineNumber={row.kind === "gap" ? undefined : (row.kind === "removed" ? row.before : row.after) + 1}
          />
        ))}

      {dialog}
    </DiffPane>
  );
}
