import { GitBranch } from "lucide-react";
import { lazy, Suspense, createElement } from "react";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { withGitFolder } from "./GitFolderGate";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parseDiffResult } from "./diffResult";
import { parseCommitResult } from "./commitResult";

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
 * `kind: "worktree"` is for `git.diff` called with no revision: the work
 * tree diff itself has no view of its own to reopen (GitDiffView's
 * `git.diff` tab only ever shows the unstaged work tree — a `staged: true`
 * call isn't reproducible there either way), so this opens the Changes view
 * instead, same place GitSidebar sends an ordinary "look at what changed"
 * click.
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
  return createElement(
    Suspense,
    { fallback: null },
    createElement(LazyGitDiffResultCard, { changes: parsed.changes, rev: parsed.rev }),
  );
});

/**
 * ToolResultView for `git.commit` (docs/dev-kit/tool-result-view.mdx).
 */
const LazyGitCommitResultCard = lazy(() =>
  import("./GitCommitResultCard").then(({ GitCommitResultCard }) => ({ default: GitCommitResultCard })),
);
registerToolResultView("git.commit", ({ rawOutput }) => {
  const commit = parseCommitResult(rawOutput);
  if (!commit) return null;
  return createElement(Suspense, { fallback: null }, createElement(LazyGitCommitResultCard, { commit }));
});
