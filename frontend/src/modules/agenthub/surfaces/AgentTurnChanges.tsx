import * as React from "react";
import { AlertTriangle, ChevronRight, FileDiff } from "lucide-react";

import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { cn } from "@/lib/utils";
import { FileDiffPane } from "./FileDiffPane";
import {
  changeCounts,
  changeSummary,
  orderChangedFiles,
  overlapNote,
  partialReason,
  sourceLabel,
} from "../checkpoints";
import {
  ChangedSource,
  type ChangedFile,
  type CheckpointSummary,
  type FileDiff as CheckpointFileDiff,
} from "../../../../bindings/devkit/internal/agenthub/models";

export type AgentTurnChangesProps = {
  checkpoint: CheckpointSummary;
  /** Loads one file's two versions. Called once per file, when it is opened. */
  loadDiff?: (checkpointId: string, path: string) => Promise<CheckpointFileDiff>;
  /**
   * Stages exactly this turn's paths and drafts a commit message.
   *
   * Absent when there is no repository, which is most of the time — and the
   * action is then not drawn rather than drawn and failing.
   */
  commitTurn?: (checkpointId: string) => Promise<void>;
  className?: string;
};

/**
 * What a turn changed on disk, under the line that says how long it worked.
 *
 * A turn's tool calls already show what the agent meant to do; this shows what
 * actually happened to the project, which is not the same list — a shell
 * command that rewrites twenty files is one tool call, and the agent's own
 * report does not mention any of them.
 *
 * Each file carries where DevKit thinks it came from, and "Unattributed" is a
 * real answer rather than a failure to label: the honest version of "something
 * changed this and nothing accounts for it" is more useful than a confident
 * guess, and the note underneath names the other sessions that were running if
 * there were any.
 */
export function AgentTurnChanges({ checkpoint, loadDiff, commitTurn, className }: AgentTurnChangesProps) {
  const [open, setOpen] = React.useState(false);
  const [staging, setStaging] = React.useState(false);
  const [stageError, setStageError] = React.useState("");
  const summary = React.useMemo(() => changeSummary(checkpoint), [checkpoint]);
  const files = React.useMemo(() => orderChangedFiles(checkpoint.changed ?? []), [checkpoint.changed]);
  const overlap = React.useMemo(() => overlapNote(checkpoint), [checkpoint]);
  const partial = React.useMemo(() => partialReason(checkpoint), [checkpoint]);

  // A turn with no checkpoint shows nothing at all — not a zero, which would be
  // a claim about a project DevKit never looked at.
  if (!summary) return null;

  return (
    <div
      data-testid="turn-changes"
      data-state={open ? "expanded" : "collapsed"}
      className={cn("flex w-full flex-col", className)}
    >
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex w-full min-w-0 cursor-pointer transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50 items-center gap-2 text-left text-body-13 text-text-tertiary hover:text-text-secondary"
      >
        <FileDiff aria-hidden="true" className="size-(--icon-12) shrink-0" />
        <span className="min-w-0 truncate">{summary.label}</span>
        {/* The same pairing AgentToolDiff uses: lime for what arrived, warning
            for what was lost. A diff has no opinion about which is good. */}
        <span className="shrink-0 text-caption-11 text-status-success">{`+${summary.added}`}</span>
        <span className="shrink-0 text-caption-11 text-status-warning">{`−${summary.removed}`}</span>
        <ChevronRight
          aria-hidden="true"
          className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none", open && "rotate-90")}
        />
      </button>

      {/* Outside the fold. "These changes cannot be undone" is the one thing
          here that changes what the user can do next, and a warning behind a
          disclosure is a warning nobody reads. */}
      {partial ? (
        <p
          data-testid="turn-changes-partial"
          className="flex items-start gap-2 pt-1 text-caption-11 text-status-warning"
        >
          <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
          <span className="min-w-0">{partial}</span>
        </p>
      ) : null}

      <AnimatedCollapse open={open} className="flex w-full flex-col gap-2 pt-2">
        {files.map((file) => (
          <ChangedFileRow
            key={file.path}
            checkpointId={checkpoint.id}
            file={file}
            overlap={overlap}
            loadDiff={loadDiff}
          />
        ))}
        {/* The bridge between two histories of one work tree: the checkpoints
            DevKit took per turn, and the commits the user authors. It stages
            and drafts and stops — committing stays a decision. */}
        {commitTurn ? (
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={staging}
              onClick={() => {
                setStaging(true);
                setStageError("");
                commitTurn(checkpoint.id)
                  .catch((error: Error) => setStageError(error.message))
                  .finally(() => setStaging(false));
              }}
              className="cursor-pointer text-caption-11 text-text-tertiary hover:text-text-secondary disabled:cursor-default"
            >
              {staging ? "Staging…" : "Commit this turn"}
            </button>
            {stageError ? (
              <span className="min-w-0 truncate text-caption-11 text-status-warning">{stageError}</span>
            ) : null}
          </div>
        ) : null}
      </AnimatedCollapse>
    </div>
  );
}

/**
 * One changed file: its path, where it came from, its counts, and — once
 * opened — the two versions side by side.
 *
 * The diff is fetched when the row is opened and not before. A turn that
 * touched forty files would otherwise pull forty file pairs out of git to draw
 * a list nobody has looked at yet.
 */
export function ChangedFileRow({
  checkpointId,
  file,
  overlap,
  loadDiff,
  defaultOpen,
}: {
  checkpointId: string;
  file: ChangedFile;
  overlap: string | null;
  loadDiff?: (checkpointId: string, path: string) => Promise<CheckpointFileDiff>;
  /** Starts open, its diff loading on mount: for a caller that shows this one file. */
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = React.useState(defaultOpen ?? false);
  const [diff, setDiff] = React.useState<CheckpointFileDiff | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const source = sourceLabel(file);
  const counts = changeCounts(file);
  const canOpen = Boolean(loadDiff) && !file.binary;

  React.useEffect(() => {
    if (!open || diff || !loadDiff) return;
    let cancelled = false;
    loadDiff(checkpointId, file.path)
      .then((result) => {
        if (!cancelled) setDiff(result);
      })
      .catch((err: unknown) => {
        // A checkpoint retention has swept, or a project that is no longer
        // there. The row stays; only its drawer says why it is empty.
        if (!cancelled) setError(err instanceof Error ? err.message : "This file could not be read.");
      });
    return () => {
      cancelled = true;
    };
  }, [open, diff, loadDiff, checkpointId, file.path]);

  return (
    <div data-testid="turn-changed-file" data-source={file.source} className="flex w-full flex-col gap-1">
      <button
        type="button"
        disabled={!canOpen}
        onClick={() => setOpen((value) => !value)}
        aria-expanded={canOpen ? open : undefined}
        className={cn(
          "flex h-(--control-height-sm) w-full min-w-0 items-center gap-2 rounded-md px-2 text-left transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50",
          canOpen ? "cursor-pointer hover:bg-surface-tinted" : "cursor-default",
        )}
      >
        <span className="min-w-0 flex-1 truncate text-code-12 text-text-secondary">{file.path}</span>
        <span
          className={cn(
            "shrink-0 rounded-sm px-1.5 text-caption-11",
            source.tone === "agent" && "bg-(--state-soft) text-text-secondary",
            source.tone === "child" && "bg-(--state-soft) text-text-secondary",
            source.tone === "unknown" && "bg-(--state-soft) text-text-tertiary",
          )}
        >
          {source.text}
        </span>
        {counts ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary">{counts}</span>
        ) : (
          <span className="shrink-0 text-caption-11 text-text-tertiary">Binary</span>
        )}
      </button>

      {/* Only under an unattributed file, and only when there was in fact
          another session: this is the evidence behind the label, not a
          disclaimer stapled to every row. */}
      {file.source === ChangedSource.SourceUnattributed && overlap ? (
        <p data-testid="turn-changed-file-overlap" className="px-2 text-caption-11 text-text-tertiary">
          {overlap}
        </p>
      ) : null}

      <AnimatedCollapse open={open} className="pt-1">
        {error ? (
          <p className="px-2 text-caption-11 text-status-warning">{error}</p>
        ) : diff ? (
          // Headless: the row above already names the file and counts its
          // lines. truncated, not a shortened diff: half a file draws a
          // difference at the cut that is not in the file, and the pane says
          // "too large to preview" for exactly this.
          <FileDiffPane
            diff={{
              path: diff.path,
              before: diff.before,
              after: diff.after,
              truncated: diff.truncated,
              beforeMissing: diff.beforeMissing,
            }}
            headless
            labels={{ before: "Before the turn", after: "After the turn" }}
            testIdPrefix="turn-diff"
          />
        ) : (
          <p className="px-2 text-caption-11 text-text-tertiary">Loading…</p>
        )}
      </AnimatedCollapse>
    </div>
  );
}
