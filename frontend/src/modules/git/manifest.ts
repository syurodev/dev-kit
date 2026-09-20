import { GitBranch } from "lucide-react";
import { lazy } from "react";
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
