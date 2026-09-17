import {
  RestoreMode,
  type RestoreConflict,
  type RestorePreview,
} from "../../../bindings/devkit/internal/agenthub/models";

/**
 * What the confirmation dialog says.
 *
 * A restore is the one thing in Agent Hub that destroys work, so the dialog has
 * to be readable before it is agreed to: how many files change, which will not,
 * and why. All of that is decided here rather than in JSX, because "is this
 * safe to press" is a rule, not a layout.
 */

export type RestoreKind = "undo" | "toBefore" | "undoRestore";

/** The dialog's title and the verb on its confirm button. */
export function restoreTitle(mode: RestoreMode | string): { title: string; confirm: string } {
  switch (mode) {
    case RestoreMode.RestoreToBeforeTurn:
      return { title: "Restore files to before this message", confirm: "Restore files" };
    case RestoreMode.RestoreUndoRestore:
      return { title: "Undo this restore", confirm: "Undo restore" };
    default:
      return { title: "Undo this turn's file changes", confirm: "Undo changes" };
  }
}

export type RestoreCounts = {
  written: number;
  deleted: number;
  /** Conflicts the user can force past. */
  overridable: number;
  /** Conflicts nobody can force: no tree holds the content, or the path is unsafe. */
  blocked: number;
};

export function restoreCounts(preview: RestorePreview | null | undefined): RestoreCounts {
  const counts: RestoreCounts = { written: 0, deleted: 0, overridable: 0, blocked: 0 };
  for (const action of preview?.actions ?? []) {
    if (action.kind === "delete") counts.deleted += 1;
    else counts.written += 1;
  }
  for (const conflict of preview?.conflicts ?? []) {
    if (conflict.overridable) counts.overridable += 1;
    else counts.blocked += 1;
  }
  return counts;
}

/**
 * The one line above the file list: what pressing the button does.
 *
 * Deletions are named separately from writes. "5 files restored" reads as five
 * files coming back; some of them are files going away, and that is the half
 * worth being explicit about.
 */
export function restoreSummary(preview: RestorePreview | null | undefined): string {
  const { written, deleted } = restoreCounts(preview);
  if (written === 0 && deleted === 0) return "Nothing to change — these files already match.";
  const parts: string[] = [];
  if (written > 0) parts.push(`${written} ${written === 1 ? "file" : "files"} restored`);
  if (deleted > 0) parts.push(`${deleted} ${deleted === 1 ? "file" : "files"} deleted`);
  return parts.join(", ");
}

/** Why one path is being left out, in the user's words. */
export function conflictReason(conflict: RestoreConflict): string {
  switch (conflict.reason) {
    case "changed_since":
      return "Changed since this turn — restoring would overwrite that edit.";
    case "protected":
      return "Not in any snapshot, so DevKit does not know what to put back.";
    case "unsafe_path":
      return "A folder on the way to this file is now a link, so it will not be written through.";
    default:
      return "Left out of this restore.";
  }
}

/**
 * Whether the confirm button does anything.
 *
 * A plan with no actions and no forced conflicts would write nothing, and a
 * button that runs a no-op still takes a safety snapshot and still tells the
 * user something happened.
 */
export function canApplyRestore(
  preview: RestorePreview | null | undefined,
  forced: ReadonlySet<string>,
): boolean {
  if (!preview) return false;
  if ((preview.actions ?? []).length > 0) return true;
  return (preview.conflicts ?? []).some((conflict) => conflict.overridable && forced.has(conflict.path));
}

/** Toggling one conflict's override, as a new set. */
export function toggleForced(forced: ReadonlySet<string>, path: string): Set<string> {
  const next = new Set(forced);
  if (next.has(path)) next.delete(path);
  else next.add(path);
  return next;
}

/** Only the overridable ones are worth sending; the backend ignores the rest. */
export function forcedPaths(preview: RestorePreview | null | undefined, forced: ReadonlySet<string>): string[] {
  return (preview?.conflicts ?? [])
    .filter((conflict) => conflict.overridable && forced.has(conflict.path))
    .map((conflict) => conflict.path);
}
