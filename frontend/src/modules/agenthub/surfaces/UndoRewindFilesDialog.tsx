import * as React from "react";
import { AlertTriangle } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { agentHubApplyRestore, agentHubPlanRestore } from "@/bridge/agenthub";
import { attemptUndo, failureMessage, undoRequest } from "../interruptedRewind";
import { canApplyRestore, forcedPaths, restoreSummary, toggleForced } from "../restore";
import { RestoreFileList } from "./RestoreFileList";
import type {
  CheckpointSummary,
  RestorePreview,
  RestoreResult,
} from "../../../../bindings/devkit/internal/agenthub/models";

/**
 * What the dialog shows for one preview. Pure: the dialog owns the state, this owns the layout,
 * so the text a person agrees to can be checked without a browser.
 */
export function UndoRewindFilesBody({
  preview,
  forced,
  onToggle,
  outcome,
  error,
}: {
  preview: RestorePreview | null;
  forced: ReadonlySet<string>;
  onToggle: (path: string) => void;
  /** An apply that did not finish: its plan is spent and the files may differ from the preview. */
  outcome: string;
  error: string;
}) {
  return (
    <div className="flex flex-col gap-3">
      {(outcome ? [] : (preview?.warnings ?? [])).map((warning) => (
        <p key={warning} className="flex items-start gap-2 text-caption-11 text-status-warning">
          <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
          <span className="min-w-0">{warning}</span>
        </p>
      ))}
      {!outcome ? (
        <RestoreFileList
          actions={preview?.actions ?? []}
          conflicts={preview?.conflicts ?? []}
          forced={forced}
          onToggle={onToggle}
        />
      ) : null}
      {outcome ? (
        <p
          role="alert"
          data-testid="undo-rewind-outcome"
          className="flex items-start gap-2 text-caption-11 text-status-warning"
        >
          <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
          <span className="min-w-0">{outcome}</span>
        </p>
      ) : null}
      {error ? (
        <p role="alert" data-testid="undo-rewind-error" className="text-caption-11 text-status-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

export type UndoRewindFilesDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  /** The interrupted rewind's safety row: what the files are put back from. */
  record: CheckpointSummary;
  onRestored: (result: RestoreResult) => void;
  /** A plan or an apply failed: the list the notice came from may be out of date. */
  onFailed: () => void;
};

/**
 * Puts the files of an interrupted rewind back, through the restore's own preview and
 * confirmation: what will change is read first (read only), conflicts are left alone unless
 * ticked, and nothing is written until the person confirms. The plan is single use; a failed
 * apply spends it, so a new look at the files is a new plan, never a silent repeat.
 */
export function UndoRewindFilesDialog({
  open,
  onOpenChange,
  sessionId,
  record,
  onRestored,
  onFailed,
}: UndoRewindFilesDialogProps) {
  const [preview, setPreview] = React.useState<RestorePreview | null>(null);
  const [forced, setForced] = React.useState<ReadonlySet<string>>(new Set());
  const [error, setError] = React.useState("");
  const [outcome, setOutcome] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  // Bumped to ask for a new plan after an apply that did not finish.
  const [planNonce, setPlanNonce] = React.useState(0);

  React.useEffect(() => {
    setPreview(null);
    setForced(new Set());
    setError("");
    setOutcome("");
    if (!open) return;
    let cancelled = false;
    agentHubPlanRestore(undoRequest(sessionId, record))
      .then((result) => {
        if (!cancelled) setPreview(result);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(failureMessage(err, "The files cannot be put back right now."));
        onFailed();
      });
    return () => {
      cancelled = true;
    };
    // onFailed is the parent's reload and changes identity per render; the plan depends on what is asked.
  },[open, sessionId, record.id, planNonce]);

  const ready = canApplyRestore(preview, forced) && !busy && !outcome;

  async function confirm() {
    if (!preview) return;
    setBusy(true);
    setError("");
    const attempt = await attemptUndo(() => agentHubApplyRestore(preview.token, forcedPaths(preview, forced)));
    setBusy(false);
    if (attempt.done) {
      onRestored(attempt.result);
      onOpenChange(false);
      return;
    }
    setPreview(null);
    setOutcome(attempt.message);
    onFailed();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-testid="undo-rewind-dialog">
        <DialogHeader>
          <DialogTitle>Put the files back as they were before the rewind</DialogTitle>
          <DialogDescription>
            {outcome
              ? "This restore did not finish."
              : preview
                ? restoreSummary(preview)
                : error
                  ? "Nothing was changed."
                  : "Working out what would change…"}
          </DialogDescription>
        </DialogHeader>
        <UndoRewindFilesBody
          preview={preview}
          forced={forced}
          onToggle={(path) => setForced((previous) => toggleForced(previous, path))}
          outcome={outcome}
          error={error}
        />
        <DialogFooter className="mt-2">
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {outcome || error ? "Close" : "Cancel"}
          </Button>
          {outcome ? (
            <Button type="button" variant="outline" onClick={() => setPlanNonce((n) => n + 1)} data-testid="undo-rewind-recheck">
              Check again
            </Button>
          ) : (
            <Button type="button" disabled={!ready} onClick={() => void confirm()} data-testid="undo-rewind-confirm">
              Put files back
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
