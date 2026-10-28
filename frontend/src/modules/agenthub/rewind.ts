import {
  RewindMode,
  type CheckpointSummary,
  type RewindPreview,
  type RewindResult,
} from "../../../bindings/devkit/internal/agenthub/models";

/**
 * Choosing and confirming a rewind.
 *
 * A rewind is two operations the user can ask for together or apart, and the
 * difference between them is not cosmetic: the file half can be undone from a
 * safety snapshot, and the conversation half cannot — the old vendor session is
 * deleted. Everything here exists so that asymmetry is on screen before the
 * button is pressed rather than discovered after.
 */

export type RewindOption = {
  mode: RewindMode;
  label: string;
  hint: string;
};

/**
 * Which rewinds this turn can be offered, in menu order.
 *
 * "Both" is the default when there are files to put back, because a turn that
 * changed files and is being taken out of the conversation has left those
 * changes with nothing that explains them. With no checkpoint there is nothing
 * to restore, so the choice collapses to the conversation alone rather than
 * offering an option that would do nothing.
 */
export function rewindOptions(checkpoint: CheckpointSummary | null | undefined): RewindOption[] {
  const files = restorableFiles(checkpoint);
  const conversation: RewindOption = {
    mode: RewindMode.RewindConversation,
    label: "Conversation only",
    hint: "The agent forgets this message onwards; files stay as they are",
  };
  if (!files) return [conversation];
  return [
    {
      mode: RewindMode.RewindBoth,
      label: "Conversation and files",
      hint: "Takes back what the agent remembers and what it changed on disk",
    },
    conversation,
    {
      mode: RewindMode.RewindCode,
      label: "Files only",
      hint: "Puts the files back; the conversation is left alone",
    },
  ];
}

/** The mode a menu opens on. */
export function defaultRewindMode(checkpoint: CheckpointSummary | null | undefined): RewindMode {
  return restorableFiles(checkpoint) ? RewindMode.RewindBoth : RewindMode.RewindConversation;
}

/**
 * Whether this turn's files can be put back at all.
 *
 * Both halves are needed: something to restore, and an opening snapshot to
 * restore to. A turn whose opening snapshot never finished has changes on
 * record and no way back to before them.
 */
export function restorableFiles(checkpoint: CheckpointSummary | null | undefined): boolean {
  return Boolean(checkpoint?.beforeTree) && (checkpoint?.changed?.length ?? 0) > 0;
}

/** The dialog's title and the verb on its confirm button, per mode. */
export function rewindTitle(mode: RewindMode | string): { title: string; confirm: string } {
  switch (mode) {
    case RewindMode.RewindConversation:
      return { title: "Rewind the conversation to here", confirm: "Rewind conversation" };
    case RewindMode.RewindCode:
      return { title: "Put this turn's files back", confirm: "Restore files" };
    default:
      return { title: "Rewind to here", confirm: "Rewind" };
  }
}

/**
 * The line under the title: what will be gone afterwards.
 *
 * Counts, not adjectives. "This cannot be undone" is true of the conversation
 * half and false of the file half, and a single sentence covering both would
 * have to be wrong about one of them.
 */
export function rewindSummary(preview: RewindPreview | null | undefined): string {
  if (!preview) return "Working out what would change…";
  const parts: string[] = [];
  if (preview.mode !== RewindMode.RewindCode) {
    parts.push(
      preview.startsFresh
        ? "The agent starts a new conversation"
        : "The agent forgets this message and everything after it",
    );
  }
  const files = preview.files;
  if (files) {
    const changing = (files.actions ?? []).length;
    parts.push(changing === 0 ? "no files change" : `${changing} ${changing === 1 ? "file" : "files"} change`);
  }
  return parts.length > 0 ? parts.join(", ") + "." : "Nothing to change.";
}

/**
 * Whether a rewind is worth carrying out.
 *
 * A conversation rewind always is — cutting messages is the whole point, and
 * there is no "nothing to cut" once a message has been named. A files-only
 * rewind is a restore, so it follows the restore's rule: nothing to write means
 * nothing to press.
 */
export function canApplyRewind(preview: RewindPreview | null | undefined, forced: ReadonlySet<string>): boolean {
  if (!preview) return false;
  if (preview.mode !== RewindMode.RewindCode) return true;
  const files = preview.files;
  if (!files) return false;
  if ((files.actions ?? []).length > 0) return true;
  return (files.conflicts ?? []).some((conflict) => conflict.overridable && forced.has(conflict.path));
}

/**
 * A queued prompt and a rewind cannot both be right.
 *
 * The queue holds messages meant for a conversation that is about to stop
 * existing; sending them afterwards would put them in a session that never
 * heard what they were answering. The user decides, so this only reports.
 */
export function queueBlocksRewind(queued: number): string | null {
  if (queued <= 0) return null;
  return queued === 1
    ? "A queued message is waiting to be sent. Rewinding would send it into a conversation it was not written for."
    : `${queued} queued messages are waiting to be sent. Rewinding would send them into a conversation they were not written for.`;
}

/** The banner text for a rewind that never finished. */
export function interruptedRewindNotice(record: CheckpointSummary | null | undefined): string | null {
  if (!record || record.kind !== "rewind_safety" || record.status !== "pending") return null;
  return "A rewind was interrupted. The files in this project may be half-restored.";
}

/** The public codes of a rewind that changed the files and did not finish (internal/bridge). */
export const REWIND_FILES_RESTORED = "agenthub.rewindFilesRestored";
export const REWIND_OUTCOME_UNKNOWN = "agenthub.rewindOutcomeUnknown";

/**
 * What a rewind that did not succeed was.
 *
 * - `partial`: the files were put back and the conversation was proven not rewound.
 * - `unknown`: the files were put back (or not) and what happened to the conversation
 *   could not be confirmed.
 * - `refused`: anything else. Its sentence is the host's own and claims nothing about
 *   the project folder.
 *
 * The first two are not "nothing changed" and not a success: the plan's token is spent,
 * the files may differ from what the dialog showed, and repeating the apply is a new
 * decision that needs a new plan.
 */
export type RewindFailure = { kind: "partial" | "unknown" | "refused"; message: string };

export function rewindFailure(error: unknown): RewindFailure {
  const code = typeof error === "object" && error !== null && "code" in error ? (error as { code?: unknown }).code : undefined;
  // Never a sentence of our own that says nothing happened: the host's message, or one
  // that only says the rewind did not finish.
  const message = error instanceof Error && error.message ? error.message : "The rewind did not finish.";
  if (code === REWIND_FILES_RESTORED) return { kind: "partial", message };
  if (code === REWIND_OUTCOME_UNKNOWN) return { kind: "unknown", message };
  return { kind: "refused", message };
}

/** The result of one attempt: success, or why not. A failure never carries a result. */
export type RewindAttempt = { done: true; result: RewindResult } | { done: false; failure: RewindFailure };

/** Runs the apply and sorts its outcome; only a resolved call is `done`. */
export async function attemptRewind(apply: () => Promise<RewindResult>): Promise<RewindAttempt> {
  try {
    return { done: true, result: await apply() };
  } catch (error: unknown) {
    return { done: false, failure: rewindFailure(error) };
  }
}

/** Whether a failure leaves the dialog's plan unusable: the files already moved. */
export function rewindFailureSpendsThePlan(failure: RewindFailure | null | undefined): boolean {
  return failure?.kind === "partial" || failure?.kind === "unknown";
}
