import {
  CheckpointKind,
  CheckpointStatus,
  RestoreMode,
  type CheckpointSummary,
  type RestoreRequest,
  type RestoreResult,
} from "../../../bindings/devkit/internal/agenthub/models";

/**
 * The notice for a rewind that stopped after it began and did not finish.
 *
 * What the backend keeps (see docs/superpowers/specs/2026-10-28-rewind-recovery-ui.md): a
 * `rewind_safety` row, pending, holding a snapshot of the project from before the rewind.
 * The row is settled `done` in the same transaction that cuts the conversation, so a row
 * that is still pending means the conversation was NOT rewound; the files may be untouched,
 * partly put back or fully put back. Everything in this file is a rule about what to say and
 * what to offer from that, and it offers only what the backend can do:
 *
 * - put the files back from the snapshot, through the existing undo-restore preview and
 *   confirmation (`PlanRestore` / `ApplyRestore`, mode `undoRestore`);
 * - settle the row without touching a file (`DismissInterruptedRewind`).
 *
 * There is no action that repeats a file restore or finishes the rewind: a rewind is a new
 * decision made from a message, with a new plan.
 */

export type InterruptedRewind = {
  /** The newest pending row. It is the one every action below is about. */
  record: CheckpointSummary;
  /** Further pending rows of the same session, older than `record`. */
  others: number;
  /** Whether a snapshot is held to put the files back from. */
  canUndo: boolean;
};

/** A safety row of a rewind that is still in flight or never finished. */
export function isInterruptedRewind(record: CheckpointSummary | null | undefined): boolean {
  return (
    record?.kind === CheckpointKind.CheckpointRewindSafety && record.status === CheckpointStatus.CheckpointPending
  );
}

/**
 * The interrupted rewind of one session, from that session's own checkpoint list.
 *
 * The list is the session's, read through the per-session call that the backend authorises,
 * so a row here is this session's by construction. `InterruptedRewinds` is not used for
 * this: it lists every session's rows and carries no session id.
 */
export function selectInterruptedRewind(
  records: readonly CheckpointSummary[] | null | undefined,
): InterruptedRewind | null {
  const pending = (records ?? []).filter(isInterruptedRewind);
  if (pending.length === 0) return null;
  // Newest first. Equal times keep the list's own (oldest first) order reversed, which is the
  // later row.
  const ordered = pending
    .map((record, index) => ({ record, index, at: Date.parse(record.createdAt as unknown as string) || 0 }))
    .sort((a, b) => b.at - a.at || b.index - a.index);
  const record = ordered[0].record;
  return { record, others: pending.length - 1, canUndo: Boolean(record.beforeTree) };
}

/** The existing restore request for putting a rewind's files back from its safety row. */
export function undoRequest(sessionId: string, record: CheckpointSummary): RestoreRequest {
  return {
    sessionId,
    mode: RestoreMode.RestoreUndoRestore,
    checkpointId: record.id,
    promptMessageId: record.promptMessageId,
  };
}

/** The notice's words. Fixed text: nothing from an error or a path goes in. */
export function interruptedRewindText(state: { canUndo: boolean; others: number }): {
  title: string;
  body: string[];
} {
  const body = [
    "The files in this project may have been put back, or only partly. The conversation was not rewound.",
    state.canUndo
      ? "DevKit kept a copy of the files as they were before the rewind."
      : "DevKit holds no complete copy of the files from before it, so they cannot be put back from here.",
  ];
  if (state.others > 0) {
    body.push(`${state.others} other ${state.others === 1 ? "rewind" : "rewinds"} in this session did not finish either.`);
  }
  return { title: "A rewind did not finish", body };
}

export type NoticeActionId = "undo" | "keep" | "confirmKeep" | "cancelKeep" | "done";

export type NoticeAction = { id: NoticeActionId; label: string; disabled: boolean; hint?: string };

export type NoticeState = {
  canUndo: boolean;
  /** The session is mid-turn: the backend refuses a restore then. */
  sessionRunning: boolean;
  /** The person pressed "keep the files": asking before the way back is closed. */
  confirmingKeep: boolean;
  /** Something is in flight: a read, the preview, the apply, or closing the notice. */
  busy: boolean;
  /** A restore from this snapshot succeeded in this window. */
  restored: boolean;
};

/**
 * Exactly the actions the backend can carry out, in order.
 *
 * Closing the notice without having put the files back ends the only way to do it from the
 * window, so it asks first. After a restore it is just done.
 */
export function noticeActions(state: NoticeState): NoticeAction[] {
  if (state.confirmingKeep) {
    return [
      { id: "confirmKeep", label: "Keep the files as they are", disabled: state.busy },
      { id: "cancelKeep", label: "Cancel", disabled: state.busy },
    ];
  }
  const actions: NoticeAction[] = [];
  if (state.canUndo) {
    actions.push({
      id: "undo",
      label: state.restored ? "Check the files again" : "Put the files back as they were",
      disabled: state.busy || state.sessionRunning,
      hint: state.sessionRunning ? "Available when the agent is not running." : undefined,
    });
  }
  actions.push(
    state.restored
      ? { id: "done", label: "Close this notice", disabled: state.busy }
      : { id: "keep", label: "Keep the files as they are", disabled: state.busy },
  );
  return actions;
}

/** One undo attempt: it resolved, or it did not and here is what to say. */
export type UndoAttempt = { done: true; result: RestoreResult } | { done: false; message: string };

/**
 * Runs the apply and sorts its outcome.
 *
 * A failure never says that nothing changed: the public error does not carry whether the
 * restore stopped before or after its first write, and the plan's token is spent either way.
 * The sentence is the host's own (when it has one) plus the one thing that is known.
 */
export async function attemptUndo(apply: () => Promise<RestoreResult>): Promise<UndoAttempt> {
  try {
    return { done: true, result: await apply() };
  } catch (error: unknown) {
    const host = error instanceof Error && error.message ? error.message.trim() : "";
    const lead = !host ? "The restore did not finish." : /[.!?]$/.test(host) ? host : `${host}.`;
    return {
      done: false,
      message: `${lead} DevKit cannot tell whether any file changed before it stopped. Check again to see what is left.`,
    };
  }
}

/** The message of a failed read or settle, the host's own or a plain fallback. */
export function failureMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

/** What a finished restore did, in counts. */
export function undoResultLine(result: RestoreResult | null | undefined): string {
  const written = result?.written?.length ?? 0;
  const deleted = result?.deleted?.length ?? 0;
  if (written === 0 && deleted === 0) return "Nothing needed to change.";
  const parts: string[] = [];
  if (written > 0) parts.push(`${written} ${written === 1 ? "file" : "files"} put back`);
  if (deleted > 0) parts.push(`${deleted} ${deleted === 1 ? "file" : "files"} deleted`);
  return `${parts.join(", ")}.`;
}

export type SettleOutcome = { ok: true } | { ok: false; message: string };

/**
 * Closes the notice: the backend settles the row without touching a file, then the list is
 * read again either way. A refused settle leaves the notice open and says so; the re-read is
 * the truth (the row may already be gone, or the session may have changed under the list).
 */
export async function settleInterruptedRewind(
  checkpointId: string,
  dismiss: (checkpointId: string) => Promise<void>,
  reload: () => Promise<void>,
): Promise<SettleOutcome> {
  try {
    await dismiss(checkpointId);
  } catch (error: unknown) {
    await reload();
    const host = failureMessage(error, "The notice could not be closed.");
    return { ok: false, message: `${host.replace(/\s+$/, "")} The notice is still open.` };
  }
  await reload();
  return { ok: true };
}
