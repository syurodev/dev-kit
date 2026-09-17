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
import { agentHubApplyRewind, agentHubPlanRewind } from "@/bridge/agenthub";
import { conflictReason, forcedPaths, toggleForced } from "../restore";
import {
  canApplyRewind,
  defaultRewindMode,
  queueBlocksRewind,
  rewindOptions,
  rewindSummary,
  rewindTitle,
} from "../rewind";
import {
  RewindMode,
  type CheckpointRecord,
  type RewindPreview,
  type RewindResult,
} from "../../../../bindings/devkit/internal/agenthub/models";

export type RewindDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  /** The turn being rewound. */
  promptMessageId: string;
  /** That turn's checkpoint, which decides which rewinds are on offer. */
  checkpoint?: CheckpointRecord;
  /** How many messages are queued behind this session. */
  queuedPrompts?: number;
  /** The user picked the pencil: put the message back in the composer after. */
  editAfter?: boolean;
  onRewound?: (result: RewindResult, editAfter: boolean) => void;
};

/**
 * The confirmation for taking a session back to a message.
 *
 * Two operations behind one button, and they are not symmetrical: the files can
 * be put back from a safety snapshot afterwards, and the conversation cannot —
 * the old vendor session is deleted. So the asymmetry is on screen before the
 * button is pressed, and the scope picker is a real choice rather than a
 * detail: a user who only wants their files back should never have to lose the
 * conversation to get them.
 */
export function RewindDialog({
  open,
  onOpenChange,
  sessionId,
  promptMessageId,
  checkpoint,
  queuedPrompts = 0,
  editAfter = false,
  onRewound,
}: RewindDialogProps) {
  const options = React.useMemo(() => rewindOptions(checkpoint), [checkpoint]);
  const [mode, setMode] = React.useState<RewindMode>(() => defaultRewindMode(checkpoint));
  const [preview, setPreview] = React.useState<RewindPreview | null>(null);
  const [forced, setForced] = React.useState<ReadonlySet<string>>(new Set());
  const [error, setError] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  // Reopening on another turn starts from that turn's own default rather than
  // whatever the last one happened to be.
  React.useEffect(() => {
    if (open) setMode(defaultRewindMode(checkpoint));
  }, [open, promptMessageId, checkpoint]);

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
    agentHubPlanRewind({ sessionId, promptMessageId, mode })
      .then((result) => {
        if (!cancelled) setPreview(result);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "This message cannot be rewound to.");
      });
    return () => {
      cancelled = true;
    };
  }, [open, sessionId, promptMessageId, mode]);

  const words = rewindTitle(mode);
  const files = preview?.files;
  const actions = files?.actions ?? [];
  const conflicts = files?.conflicts ?? [];
  const queueWarning = queueBlocksRewind(mode === RewindMode.RewindCode ? 0 : queuedPrompts);
  const ready = canApplyRewind(preview, forced) && !busy;

  async function confirm() {
    if (!preview) return;
    setBusy(true);
    setError("");
    try {
      const result = await agentHubApplyRewind(preview.token, forcedPaths(files, forced));
      onRewound?.(result, editAfter);
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Nothing was changed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-testid="rewind-dialog">
        <DialogHeader>
          <DialogTitle>{editAfter ? "Edit and run again from here" : words.title}</DialogTitle>
          <DialogDescription>{rewindSummary(preview)}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          {options.length > 1 ? (
            <div className="flex flex-col gap-1" role="radiogroup" aria-label="What to rewind">
              {options.map((option) => (
                <button
                  key={option.mode}
                  type="button"
                  role="radio"
                  aria-checked={mode === option.mode}
                  onClick={() => setMode(option.mode)}
                  className={cn(
                    "flex cursor-pointer flex-col items-start gap-0.5 rounded-md border px-3 py-2 text-left",
                    mode === option.mode
                      ? "border-(length:--stroke-1-5) border-border-strong bg-surface-tinted"
                      : "border-border-subtle hover:bg-surface-tinted",
                  )}
                >
                  <span className="text-body-13 text-text-primary">{option.label}</span>
                  <span className="text-caption-11 text-text-tertiary">{option.hint}</span>
                </button>
              ))}
            </div>
          ) : null}

          {queueWarning ? (
            <p data-testid="rewind-queue-warning" className="flex items-start gap-2 text-caption-11 text-status-warning">
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">{queueWarning}</span>
            </p>
          ) : null}

          {/* The pencil's one limitation, said before it matters: the text comes
              back, the files and images that went with it do not. */}
          {editAfter ? (
            <p className="text-caption-11 text-text-tertiary">
              The message goes back in the composer for you to change. Files and images attached to it are not restored.
            </p>
          ) : null}

          {(preview?.warnings ?? []).map((warning) => (
            <p key={warning} className="flex items-start gap-2 text-caption-11 text-status-warning">
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">{warning}</span>
            </p>
          ))}

          {actions.length > 0 || conflicts.length > 0 ? (
            <div className="max-h-56 overflow-y-auto">
              <ul className="flex flex-col gap-1">
                {actions.map((action) => (
                  <li key={action.path} data-testid="rewind-action" className="flex items-center gap-2">
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
                  <p className="text-caption-11 text-text-tertiary">These are left alone unless you say otherwise:</p>
                  {conflicts.map((conflict) => (
                    <label
                      key={conflict.path}
                      data-testid="rewind-conflict"
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
          ) : null}

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
          <Button type="button" disabled={!ready} onClick={() => void confirm()} data-testid="rewind-confirm">
            {editAfter ? "Rewind and edit" : words.confirm}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
