import { GitBranch } from "lucide-react";
import { lazy, createElement } from "react";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { withGitFolder } from "./GitFolderGate";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { registerGitActivities } from "./toolActivities";
import { parseDiffResult } from "./diffResult";
import { parseCommitResult, parseHistoryResult } from "./commitResult";
import { stringField } from "@/workbench/toolActivity";

export const gitModule: ModuleManifest = {
  id: "git",
  title: "Git",
  version: "1.0.0",
  apiVersion: "1",
  icon: GitBranch,
  padded: false,
  tabs: true,
  // Changes is the whole point of arriving here, and it follows the folder the
  // app has open, so there is nothing to choose before showing it.
  opensHome: true,
  // The rail is the module's, not a view's: repositories, changes and history
  // are there with no tab open, which is when Git is first brought on screen.
  sidebar: lazy(() => import("./GitSidebar").then(({ GitSidebar }) => ({ default: GitSidebar }))),
  // git:push is separate from git:write because it leaves the machine.
  // There is no git:push:force, here or in the backend.
  capabilities: ["git:read", "git:write", "git:push"],
  commands: [
    { id: "git.commit", title: "Commit staged changes" },
    { id: "git.push", title: "Push" },
  ],
  views: [
    {
      id: "git.changes",
      title: "Changes",
      ...lazyModuleView(() =>
        import("./GitChangesView").then(({ GitChangesView }) => ({ default: withGitFolder(GitChangesView, { bar: false }) })),
      ),
    },
    {
      id: "git.diff",
      title: "Diff",
      ...lazyModuleView(() =>
        import("./GitDiffView").then(({ GitDiffView }) => ({ default: withGitFolder(GitDiffView, { bar: false }) })),
      ),
    },
    {
      id: "git.remotes",
      title: "Remotes",
      ...lazyModuleView(() =>
        import("./GitRemotesView").then(({ GitRemotesView }) => ({ default: withGitFolder(GitRemotesView) })),
      ),
    },
    {
      id: "git.history",
      title: "History",
      ...lazyModuleView(() =>
        import("./GitHistoryView").then(({ GitHistoryView }) => ({ default: withGitFolder(GitHistoryView) })),
      ),
    },
  ],
};

/**
 * `git.reveal` (docs/dev-kit/cross-module.mdx): open a diff from anywhere —
 * a ToolResultView card for `git.commit`, or `git.diff` when the tool was
 * called against a specific revision — without the caller importing this
 * module's documents helper. `kind: "commit"` matches
 * internal/agenthub/attachedref.go's own ref kind for a git commit.
 *
 * `kind: "worktree"` opens Changes and its composer. Staged and unstaged
 * files can then be reviewed in the diff viewer. With an `id` it is that
 * repository's path: Git is pinned to it first (the work folder's pin, the
 * same one Git's own folder picker sets), so a workspace of several
 * repositories can be opened at the one asked about.
 */
registerIntent(
  "git.reveal",
  async (ref, { placement }) => {
    const beside = placement === "beside";
    if (ref.kind === "commit") {
      const { openCommitDiff } = await import("./documents");
      openCommitDiff(ref.id, ref.label || ref.id, { beside });
      return;
    }
    if (ref.kind === "worktree") {
      if (ref.id) {
        const { pinWorkFolder } = await import("@/bridge/workfolder");
        await pinWorkFolder("git", ref.id).catch(() => undefined);
      }
      const { openChanges } = await import("./documents");
      openChanges({ beside });
    }
  },
  "beside",
);

/**
 * ToolResultView for `git.diff` (docs/dev-kit/tool-result-view.mdx). Shape
 * checked synchronously before the card component loads.
 */
const LazyGitDiffResultCard = lazy(() =>
  import("./GitDiffResultCard").then(({ GitDiffResultCard }) => ({ default: GitDiffResultCard })),
);
registerToolResultView("git.diff", ({ rawInput, rawOutput }) => {
  const parsed = parseDiffResult(rawInput, rawOutput);
  if (!parsed) return null;
  return createElement(LazyGitDiffResultCard, { changes: parsed.changes, rev: parsed.rev });
});

/**
 * ToolResultView for `git.commit` (docs/dev-kit/tool-result-view.mdx).
 */
const LazyGitCommitResultCard = lazy(() =>
  import("./GitCommitResultCard").then(({ GitCommitResultCard }) => ({ default: GitCommitResultCard })),
);
for (const [tool, title] of [
  ["git.commit", undefined],
  ["git.amend", "Amended commit"],
  ["git.squash", "Squashed commit"],
] as const) {
  registerToolResultView(tool, ({ rawOutput, heading }) => {
    const commit = parseCommitResult(rawOutput);
    if (!commit) return null;
    return createElement(LazyGitCommitResultCard, { commit, heading: heading ?? title });
  });
}

/**
 * ToolResultView for the history tools that can stop on a conflict: the new
 * head when they finished, the conflicted paths when they did not.
 */
const LazyGitHistoryResultCard = lazy(() =>
  import("./GitHistoryResultCard").then(({ GitHistoryResultCard }) => ({ default: GitHistoryResultCard })),
);
for (const [tool, title] of [
  ["git.rebase", "Rebase"],
  ["git.cherryPick", "Cherry-pick"],
  ["git.revert", "Revert"],
] as const) {
  registerToolResultView(tool, ({ rawInput, rawOutput, heading }) => {
    const result = parseHistoryResult(rawOutput);
    if (!result) return null;
    const repo = stringField(rawInput, "repo");
    return createElement(LazyGitHistoryResultCard, { result, heading: heading ?? title, repo: repo.startsWith("/") ? repo : undefined });
  });
}

// What this module's tools that change something do, for the transcript's
// always-visible cards (workbench/toolActivity.ts).
registerGitActivities();
