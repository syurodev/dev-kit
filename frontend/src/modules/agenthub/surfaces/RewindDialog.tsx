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
import { cn } from "@/lib/utils";
import { agentHubApplyRewind, agentHubPlanRewind } from "@/bridge/agenthub";
import { forcedPaths, toggleForced } from "../restore";
import { RestoreFileList } from "./RestoreFileList";
import {
  attemptRewind,
  canApplyRewind,
  defaultRewindMode,
  queueBlocksRewind,
  rewindFailureSpendsThePlan,
  rewindOptions,
  rewindSummary,
  rewindTitle,
  type RewindFailure,
} from "../rewind";
import {
  RewindMode,
  type CheckpointSummary,
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
  checkpoint?: CheckpointSummary;
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
  // A rewind that changed the files and did not finish: not a success, not "nothing
  // changed". The plan's token is spent, so the confirm button stays off until the
  // person asks for a new plan (another scope, or reopening).
  const [outcome, setOutcome] = React.useState<RewindFailure | null>(null);
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
      setOutcome(null);
      return;
    }
    let cancelled = false;
    setPreview(null);
    setForced(new Set());
    setError("");
    setOutcome(null);
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
  const ready = canApplyRewind(preview, forced) && !busy && !outcome;

  async function confirm() {
    if (!preview) return;
    setBusy(true);
    setError("");
    const attempt = await attemptRewind(() => agentHubApplyRewind(preview.token, forcedPaths(files, forced)));
    setBusy(false);
    if (attempt.done) {
      onRewound?.(attempt.result, editAfter);
      onOpenChange(false);
      return;
    }
    // Only a success refreshes the surfaces that depend on the rewind having happened.
    if (rewindFailureSpendsThePlan(attempt.failure)) setOutcome(attempt.failure);
    else setError(attempt.failure.message);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-testid="rewind-dialog">
        <DialogHeader>
          <DialogTitle>{editAfter ? "Edit and run again from here" : words.title}</DialogTitle>
          <DialogDescription>{outcome ? "This rewind did not finish." : rewindSummary(preview)}</DialogDescription>
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

          {(outcome ? [] : (preview?.warnings ?? [])).map((warning) => (
            <p key={warning} className="flex items-start gap-2 text-caption-11 text-status-warning">
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">{warning}</span>
            </p>
          ))}

          {!outcome ? (
            <RestoreFileList
              actions={actions}
              conflicts={conflicts}
              forced={forced}
              onToggle={(path) => setForced((previous) => toggleForced(previous, path))}
            />
          ) : null}

          {outcome ? (
            <p
              role="alert"
              data-testid="rewind-outcome"
              data-kind={outcome.kind}
              className="flex items-start gap-2 text-caption-11 text-status-warning"
            >
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">{outcome.message}</span>
            </p>
          ) : null}

          {error ? (
            <p role="alert" className="text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
        </div>

        <DialogFooter className="mt-2">
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {outcome ? "Close" : "Cancel"}
          </Button>
          <Button type="button" disabled={!ready} onClick={() => void confirm()} data-testid="rewind-confirm">
            {editAfter ? "Rewind and edit" : words.confirm}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
