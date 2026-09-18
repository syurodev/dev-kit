import * as React from "react";
import {
  agentHubPermissionFileDiff,
  permissionFileEdits,
  type PendingPermission,
  type PermissionFileDiff,
} from "@/bridge/agenthub";
import { Maximize2 } from "lucide-react";
import { DiffLineRow, DiffPane, DiffSplitPanes } from "@/components/ui/diff";
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
import { cn } from "@/lib/utils";

/** The path as the project names it: relative inside the session's folder. */
function displayPath(path: string, projectPath?: string): string {
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
function useDiffHighlight(diff: PermissionFileDiff): { before: SyntaxSegment[][]; after: SyntaxSegment[][] } | null {
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

function FileEdit({
  diff,
  projectPath,
  actions,
  className,
}: {
  diff: PermissionFileDiff;
  projectPath?: string;
  /** The card's own Deny / Allow buttons, repeated in the full-diff dialog. */
  actions?: React.ReactNode;
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

  const note = diff.binary
    ? "binary file"
    : diff.truncated
      ? "too large to preview"
      : diff.beforeMissing
        ? "new file"
        : diff.stale
          ? "file changed since the agent read it"
          : null;

  return (
    <DiffPane
      label={displayPath(diff.path, projectPath)}
      caps={false}
      className={className}
      bodyClassName="max-h-60"
      meta={
        <span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">
          {note ? <span className={diff.stale ? "text-status-warning" : "text-text-tertiary"}>{note}</span> : null}
          {counts}
          {lines.length > 0 ? (
            <ActionTooltip label="Review the whole file">
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                data-testid="permission-diff-expand"
                aria-label={`Review the whole diff of ${name}`}
                onClick={() => setExpanded(true)}
                className="text-text-tertiary"
              >
                <Maximize2 aria-hidden="true" />
              </Button>
            </ActionTooltip>
          ) : null}
        </span>
      }
    >
      {rows.map((row, i) => (
        <DiffLineRow
          key={i}
          row={row}
          segments={segmentsFor(row)}
          // The side the line is from: a removed line's number is the one it
          // has in the file on disk, everything else the one it will have.
          lineNumber={row.kind === "gap" ? undefined : (row.kind === "removed" ? row.before : row.after) + 1}
        />
      ))}

      {/* The card shows the edit; this shows the file. Every line, unfolded,
          with the same two answers at the foot — a diff worth opening full
          size is one you are deciding on, and closing it to press Allow would
          lose your place in it. */}
      <Dialog open={expanded} onOpenChange={setExpanded}>
        {/* No fixed height: DialogContent lays its children out in a h-fit
            body, so the dialog is as tall as the diff up to the cap below. */}
        <DialogContent className="w-[min(1100px,92vw)] max-w-none gap-3 p-4 sm:max-w-none">
          <DialogHeader className="flex-row items-center gap-3 space-y-0 pr-8">
            <DialogTitle className="min-w-0 flex-1 truncate text-code-12 text-text-primary">{name}</DialogTitle>
            <span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">{counts}</span>
          </DialogHeader>
          {/* Two panes — Figma 08 Diff, example D. The file as it is beside
              what the edit makes of it, changed lines facing each other. The
              proposed side takes the active emphasis: it is the one being
              decided on. */}
          <div className="max-h-[70vh] overflow-auto select-text">
            <DiffSplitPanes
              pairs={pairs}
              runs={runs}
              leftLabel={diff.beforeMissing ? "Not there yet" : "On disk"}
              rightLabel="Proposed"
              rightMeta={<span className="flex shrink-0 items-center gap-2 text-caption-11 whitespace-nowrap">{counts}</span>}
              segmentsFor={(line, which) =>
                which === "left" ? highlight?.before[line.before] : highlight?.after[line.after]
              }
            />
          </div>
          {actions ? <div className="flex items-center justify-end gap-2">{actions}</div> : null}
        </DialogContent>
      </Dialog>
    </DiffPane>
  );
}

/**
 * The files a permission request would change, shown as 08 Diff panes in the
 * card that asks for it — what is being allowed, not only which tool.
 *
 * Drawn at once from what the request carries (the agent's snippet), then
 * replaced by the whole-file preview the backend reads from disk, which places
 * the edit among its surrounding lines and tells a new file from an
 * overwrite. Nothing for a request that edits no file.
 */
export function PermissionFileEdits({
  pending,
  projectPath,
  actions,
  className,
}: {
  pending: PendingPermission;
  projectPath?: string;
  /** The card's Deny / Allow buttons, so the full-diff dialog can answer too. */
  actions?: React.ReactNode;
  className?: string;
}) {
  const fromRequest = React.useMemo(() => permissionFileEdits(pending.params), [pending.params]);
  const [fromDisk, setFromDisk] = React.useState<PermissionFileDiff[] | null>(null);

  React.useEffect(() => {
    if (fromRequest.length === 0) return;
    let live = true;
    setFromDisk(null);
    agentHubPermissionFileDiff(pending.id)
      .then((diffs) => {
        if (live && diffs.length > 0) setFromDisk(diffs);
      })
      .catch(() => {
        // The request's own snippet stays on screen; it is still accurate.
      });
    return () => {
      live = false;
    };
  }, [pending.id, fromRequest.length]);

  const diffs = fromDisk ?? fromRequest;
  if (diffs.length === 0) return null;
  return (
    <div data-testid="permission-file-edits" className={cn("flex w-full min-w-0 flex-col gap-2", className)}>
      {diffs.map((diff, i) => (
        <FileEdit key={`${diff.path}:${i}`} diff={diff} projectPath={projectPath} actions={actions} />
      ))}
    </div>
  );
}
