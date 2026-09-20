import type { GitStatus, Guard, Remote } from "@/bridge/git";

/**
 * What the Changes view last read for a folder. A view is remounted when Git
 * moves to another repository, and without this it starts from nothing and
 * paints an empty frame before its first read lands — the flicker of choosing a
 * repository. With it, coming back to a repository the view has seen paints
 * that repository at once, and the fresh read replaces it a moment later.
 *
 * Keyed by folder, never shared between two: a snapshot shown under the wrong
 * repository would be worse than the blank frame it replaces. Not persisted and
 * not reactive — it is read once, when a view mounts.
 */
export type GitSnapshot = { status: GitStatus; guards: Guard[]; remotes: Remote[] | null };

const snapshots = new Map<string, GitSnapshot>();

export function recallSnapshot(folder: string): GitSnapshot | null {
  return snapshots.get(folder) ?? null;
}

export function rememberSnapshot(folder: string, snapshot: GitSnapshot) {
  if (folder) snapshots.set(folder, snapshot);
}
