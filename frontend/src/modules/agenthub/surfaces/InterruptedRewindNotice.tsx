import * as React from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { agentHubDismissInterruptedRewind } from "@/bridge/agenthub";
import {
  interruptedRewindText,
  noticeActions,
  settleInterruptedRewind,
  undoResultLine,
  type InterruptedRewind,
  type NoticeActionId,
  type NoticeState,
} from "../interruptedRewind";
import { useInterruptedRewind } from "../useInterruptedRewind";
import { UndoRewindFilesDialog } from "./UndoRewindFilesDialog";
import type { RestoreResult } from "../../../../bindings/devkit/internal/agenthub/models";

/**
 * The notice itself, for the state it is given. Pure markup: the container owns the state.
 *
 * It says what is true of a rewind that did not finish and offers only what the backend can
 * do. Warning tone sits in the dot and the border, like the session failure card, because the
 * chrome stays monochrome and a warning is a status.
 */
export function InterruptedRewindNoticeView({
  item,
  state,
  restoredLine,
  error,
  onAction,
  className,
}: {
  item: Pick<InterruptedRewind, "canUndo" | "others">;
  state: NoticeState;
  /** What the last restore did, once one succeeded. */
  restoredLine?: string;
  error?: string;
  onAction: (id: NoticeActionId) => void;
  className?: string;
}) {
  const text = interruptedRewindText(item);
  const actions = noticeActions(state);
  return (
    <div
      role="status"
      data-testid="interrupted-rewind-notice"
      className={cn(
        "flex w-full flex-col gap-2 rounded-control border border-(--border-warning) bg-surface-raised px-3 py-2",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-status-warning" />
        <p className="text-body-13-medium text-text-primary">{text.title}</p>
      </div>
      <div className="flex flex-col gap-1">
        {text.body.map((line) => (
          <p key={line} className="text-caption-11 text-text-tertiary">
            {line}
          </p>
        ))}
        {state.confirmingKeep ? (
          <p data-testid="interrupted-rewind-keep-warning" className="text-caption-11 text-text-secondary">
            {item.canUndo
              ? "This closes the notice and does not change any file. The copy is no longer offered here."
              : "This closes the notice and does not change any file."}
          </p>
        ) : null}
        {restoredLine ? (
          <p data-testid="interrupted-rewind-restored" className="text-caption-11 text-text-secondary">
            {`The last restore: ${restoredLine} Close this notice when the files look right.`}
          </p>
        ) : null}
      </div>
      {error ? (
        <p role="alert" data-testid="interrupted-rewind-error" className="text-caption-11 text-status-danger">
          {error}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {actions.map((action, index) => (
          <Button
            key={action.id}
            type="button"
            size="sm"
            variant={index === 0 ? "default" : "outline"}
            disabled={action.disabled}
            data-testid={`interrupted-rewind-${action.id}`}
            title={action.hint}
            onClick={() => onAction(action.id)}
            className={cn("h-8", index > 0 && "border-border-subtle")}
          >
            {action.label}
          </Button>
        ))}
      </div>
      {actions.some((action) => action.hint) ? (
        <p className="text-caption-11 text-text-tertiary">{actions.find((action) => action.hint)?.hint}</p>
      ) : null}
    </div>
  );
}

/**
 * Shown in a session's view only while that session has a rewind that did not finish. Hidden
 * while a rewind dialog is open (`suspended`): a rewind that is running holds the same pending
 * row, and it is not an interruption.
 */
export function InterruptedRewindNotice({
  sessionId,
  sessionRunning = false,
  suspended = false,
  className,
}: {
  sessionId: string;
  sessionRunning?: boolean;
  suspended?: boolean;
  className?: string;
}) {
  const { item, reload } = useInterruptedRewind(sessionId, { suspended });
  if (!item) return null;
  return (
    <NoticeForRow
      // New row or new session: a restore result or a question about one row never carries over.
      key={`${sessionId}:${item.record.id}`}
      sessionId={sessionId}
      item={item}
      sessionRunning={sessionRunning}
      reload={reload}
      className={className}
    />
  );
}

function NoticeForRow({
  sessionId,
  item,
  sessionRunning,
  reload,
  className,
}: {
  sessionId: string;
  item: InterruptedRewind;
  sessionRunning: boolean;
  reload: () => Promise<void>;
  className?: string;
}) {
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [confirmingKeep, setConfirmingKeep] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [restored, setRestored] = React.useState<RestoreResult | null>(null);
  const [error, setError] = React.useState("");

  const state: NoticeState = {
    canUndo: item.canUndo,
    sessionRunning,
    confirmingKeep,
    busy: busy || dialogOpen,
    restored: restored !== null,
  };

  async function settle() {
    setBusy(true);
    setError("");
    const outcome = await settleInterruptedRewind(item.record.id, agentHubDismissInterruptedRewind, reload);
    setBusy(false);
    setConfirmingKeep(false);
    if (!outcome.ok) setError(outcome.message);
  }

  function onAction(id: NoticeActionId) {
    switch (id) {
      case "undo":
        setError("");
        setDialogOpen(true);
        return;
      case "keep":
        setConfirmingKeep(true);
        return;
      case "cancelKeep":
        setConfirmingKeep(false);
        return;
      case "confirmKeep":
      case "done":
        void settle();
        return;
    }
  }

  return (
    <>
      <InterruptedRewindNoticeView
        item={item}
        state={state}
        restoredLine={restored ? undoResultLine(restored) : undefined}
        error={error}
        onAction={onAction}
        className={className}
      />
      {item.canUndo ? (
        <UndoRewindFilesDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          sessionId={sessionId}
          record={item.record}
          onRestored={setRestored}
          // The list may be out of date (the row settled, the session changed): read it again
          // rather than guess. The notice goes away on its own if the row is gone.
          onFailed={() => void reload()}
        />
      ) : null}
    </>
  );
}
