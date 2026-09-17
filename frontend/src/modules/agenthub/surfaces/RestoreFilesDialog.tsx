import * as React from "react";
import { AlertTriangle, FileMinus, FilePlus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { agentHubApplyRestore, agentHubPlanRestore } from "@/bridge/agenthub";
import {
  canApplyRestore,
  conflictReason,
  forcedPaths,
  restoreCounts,
  restoreSummary,
  restoreTitle,
  toggleForced,
} from "../restore";
import {
  RestoreMode,
  type RestorePreview,
  type RestoreResult,
} from "../../../../bindings/devkit/internal/agenthub/models";

export type RestoreFilesDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  /** The turn being reversed. */
  promptMessageId: string;
  /** Called once files have actually changed. */
  onRestored?: (result: RestoreResult) => void;
};

/**
 * The confirmation for putting a turn's files back.
 *
 * It plans before it asks. A dialog that said "undo this turn?" and only found
 * out what that meant after the user agreed would be asking them to consent to
 * something neither of them had seen — so the list of files, the conflicts and
 * the warnings are all on screen before the button is live, and the button is
 * dead until there is something for it to do.
 *
 * Changing the scope re-plans, because the two scopes are different questions
 * about the same project and the answer to one says nothing about the other.
 */
export function RestoreFilesDialog({
  open,
  onOpenChange,
  sessionId,
  promptMessageId,
  onRestored,
}: RestoreFilesDialogProps) {
  const [mode, setMode] = React.useState<RestoreMode>(RestoreMode.RestoreUndoTurn);
  const [preview, setPreview] = React.useState<RestorePreview | null>(null);
  const [forced, setForced] = React.useState<ReadonlySet<string>>(new Set());
  const [error, setError] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (!open) {
      setPreview(null);
      setForced(new Set());
      setError("");
      return;
    }
    let cancelled = false;
    setPreview(null);
    setForced(new Set());
    setError("");
    agentHubPlanRestore({ sessionId, mode, promptMessageId, checkpointId: "" })
      .then((result) => {
        if (!cancelled) setPreview(result);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "This turn cannot be restored.");
      });
    return () => {
      cancelled = true;
    };
  }, [open, sessionId, promptMessageId, mode]);

  const words = restoreTitle(mode);
  const counts = restoreCounts(preview);
  // The bindings model Go's nil slices as null; every reader here wants a list.
  const actions = preview?.actions ?? [];
  const conflicts = preview?.conflicts ?? [];
  const ready = canApplyRestore(preview, forced) && !busy;

  async function confirm() {
    if (!preview) return;
    setBusy(true);
    setError("");
    try {
      const result = await agentHubApplyRestore(preview.token, forcedPaths(preview, forced));
      onRestored?.(result);
      onOpenChange(false);
    } catch (err: unknown) {
      // The plan went stale, or the session started a turn while the dialog was
      // open. Either way nothing was written; say so and let them try again.
      setError(err instanceof Error ? err.message : "Nothing was changed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-testid="restore-files-dialog">
        <DialogHeader>
          <DialogTitle>{words.title}</DialogTitle>
          <DialogDescription>{restoreSummary(preview)}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <div className="flex gap-2" role="radiogroup" aria-label="How much to restore">
            <ScopeButton
              selected={mode === RestoreMode.RestoreUndoTurn}
              onSelect={() => setMode(RestoreMode.RestoreUndoTurn)}
              label="Just this turn"
              hint="Leaves later turns alone"
            />
            <ScopeButton
              selected={mode === RestoreMode.RestoreToBeforeTurn}
              onSelect={() => setMode(RestoreMode.RestoreToBeforeTurn)}
              label="This turn and everything after"
              hint="Files go back to before this message"
            />
          </div>

          {(preview?.warnings ?? []).map((warning) => (
            <p key={warning} className="flex items-start gap-2 text-caption-11 text-status-warning">
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">{warning}</span>
            </p>
          ))}

          {preview ? (
            <div className="max-h-64 overflow-y-auto">
              <ul className="flex flex-col gap-1">
                {actions.map((action) => (
                  <li key={action.path} data-testid="restore-action" className="flex items-center gap-2">
                    {action.kind === "delete" ? (
                      <FileMinus aria-hidden="true" className="size-(--icon-12) shrink-0 text-status-warning" />
                    ) : (
                      <FilePlus aria-hidden="true" className="size-(--icon-12) shrink-0 text-brand-lime" />
                    )}
                    <span className="min-w-0 truncate text-code-12 text-text-secondary">{action.path}</span>
                  </li>
                ))}
              </ul>

              {conflicts.length > 0 ? (
                <div className="mt-3 flex flex-col gap-2 border-t border-border-subtle pt-3">
                  <p className="text-caption-11 text-text-tertiary">
                    {counts.blocked > 0 && counts.overridable === 0
                      ? "These cannot be restored:"
                      : "These are left alone unless you say otherwise:"}
                  </p>
                  {conflicts.map((conflict) => (
                    <label
                      key={conflict.path}
                      data-testid="restore-conflict"
                      className={cn("flex items-start gap-2", conflict.overridable ? "cursor-pointer" : "cursor-default")}
                    >
                      <Checkbox
                        className="mt-0.5"
                        disabled={!conflict.overridable}
                        checked={forced.has(conflict.path)}
                        onCheckedChange={() => setForced((previous) => toggleForced(previous, conflict.path))}
                        aria-label={`Overwrite ${conflict.path}`}
                      />
                      <span className="flex min-w-0 flex-col">
                        <span className="min-w-0 truncate text-code-12 text-text-secondary">{conflict.path}</span>
                        <span className="text-caption-11 text-text-tertiary">{conflictReason(conflict)}</span>
                      </span>
                    </label>
                  ))}
                </div>
              ) : null}
            </div>
          ) : error ? null : (
            <p className="text-caption-11 text-text-tertiary">Working out what would change…</p>
          )}

          {error ? (
            <p role="alert" className="text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
        </div>

        <DialogFooter className="mt-2">
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" disabled={!ready} onClick={() => void confirm()} data-testid="restore-confirm">
            {words.confirm}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ScopeButton({
  selected,
  onSelect,
  label,
  hint,
}: {
  selected: boolean;
  onSelect: () => void;
  label: string;
  hint: string;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex flex-1 cursor-pointer flex-col items-start gap-0.5 rounded-md border px-3 py-2 text-left",
        selected
          ? "border-(length:--stroke-1-5) border-border-strong bg-surface-tinted"
          : "border-border-subtle hover:bg-surface-tinted",
      )}
    >
      <span className="text-body-13 text-text-primary">{label}</span>
      <span className="text-caption-11 text-text-tertiary">{hint}</span>
    </button>
  );
}
