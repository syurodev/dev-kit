import { getModule, viewIcon } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

export const MODULE = "git";
export const DIFF_VIEW = "git.diff";
export const CHANGES_VIEW = "git.changes";

/**
 * Opens one commit's diff as a tab — GitSidebar's history row, and
 * `git.reveal` (docs/dev-kit/cross-module.mdx) for anywhere else. `rev` is
 * any revision git accepts, not just a full SHA; GitDiffView reads it back
 * out of `params.rev`.
 */
export function openCommitDiff(rev: string, title: string, options: { beside?: boolean } = {}): void {
  void getModule(MODULE)?.views.find((v) => v.id === DIFF_VIEW)?.preload?.().catch(() => undefined);
  useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: DIFF_VIEW,
    title,
    icon: viewIcon(MODULE, DIFF_VIEW),
    params: { rev },
    beside: options.beside,
  });
}

/**
 * Opens the working tree's Changes view — where an unstaged or staged diff
 * (an empty `rev`) is actually browsable; GitDiffView's own `git.diff` tab
 * only ever shows the unstaged work tree, so a `git.diff` tool call made
 * with `staged: true` is not literally reproducible there.
 */
export function openChanges(options: { beside?: boolean } = {}): void {
  void getModule(MODULE)?.views.find((v) => v.id === CHANGES_VIEW)?.preload?.().catch(() => undefined);
  useWorkbenchTabsStore.getState().openTab({
    moduleId: MODULE,
    viewId: CHANGES_VIEW,
    title: "Changes",
    icon: viewIcon(MODULE, CHANGES_VIEW),
    beside: options.beside,
  });
}
