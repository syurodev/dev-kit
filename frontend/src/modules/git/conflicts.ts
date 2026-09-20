import type { Conflict } from "@/bridge/git";

/**
 * Reading a git conflict into the words and the panes the resolve panel shows.
 *
 * It is separate from the panel because every branch here is a real situation
 * a merge produces and each one is resolved differently — a delete/modify is
 * not a both-changed, and an add/add has no base to merge from. Getting one
 * wrong shows the person the wrong buttons, so they are tested.
 */

/**
 * A line git itself wrote to mark a conflict. `=======` has to be the whole
 * line: a row of equals signs underlining a heading is not a marker.
 */
const MARKER = /^(?:<<<<<<<|>>>>>>>|=======$)/;

/** How many marker lines are left in the merged text. */
export function markerCount(text: string): number {
  let count = 0;
  for (const line of text.split("\n")) {
    if (MARKER.test(line)) count++;
  }
  return count;
}

export type ConflictReading = {
  /** Whether there is text to line-merge at all. */
  mergeable: boolean;
  /** One sentence naming the situation and what resolves it. */
  situation: string;
  oursLabel: string;
  theirsLabel: string;
  oursText: string;
  theirsText: string;
};

export function readConflict(conflict: Conflict, branch: string): ConflictReading {
  const mergeable = !conflict.binary && conflict.hasOurs && conflict.hasTheirs;
  const oursLabel = branch ? `Ours · ${branch}` : "Ours · this branch";

  const situation = conflict.binary
    ? "Binary file — there is nothing to merge by hand. Pick a side and git copies it byte for byte."
    : !conflict.hasOurs
      ? "This branch deleted this file; the incoming branch changed it. Pick a side — a deletion is not line-merged."
      : !conflict.hasTheirs
        ? "The incoming branch deleted this file; this branch changed it. Pick a side — a deletion is not line-merged."
        : !conflict.hasBase
          ? "Both branches added this file, so there is no common version to merge from. Edit the merged pane, or take one side whole."
          : "Both branches changed this file. Edit the merged pane — it is git's own merge — or take one side whole.";

  return {
    mergeable,
    situation,
    oursLabel,
    theirsLabel: "Theirs · incoming",
    oursText: conflict.binary
      ? "(binary)"
      : conflict.hasOurs
        ? conflict.ours
        : "(deleted on this branch)",
    theirsText: conflict.binary
      ? "(binary)"
      : conflict.hasTheirs
        ? conflict.theirs
        : "(deleted on the incoming branch)",
  };
}
