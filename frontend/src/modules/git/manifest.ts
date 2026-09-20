import { GitBranch } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { withGitFolder } from "./GitFolderGate";

export const gitModule: ModuleManifest = {
  id: "git",
  title: "Git",
  version: "1.0.0",
  apiVersion: "1",
  icon: GitBranch,
  padded: false,
  tabs: true,
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
        import("./GitChangesView").then(({ GitChangesView }) => ({ default: withGitFolder(GitChangesView) })),
      ),
    },
    {
      id: "git.diff",
      title: "Diff",
      ...lazyModuleView(() =>
        import("./GitDiffView").then(({ GitDiffView }) => ({ default: withGitFolder(GitDiffView) })),
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
