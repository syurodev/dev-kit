import { GitCommitHorizontal } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { CommitResult } from "./commitResult";

/**
 * ToolResultView for `git.commit` (docs/dev-kit/tool-result-view.mdx). The
 * whole row opens the diff through the existing `git.reveal` intent, by the
 * full SHA (not the short form — precise, and CommitView reads any
 * revision string git accepts).
 */
export function GitCommitResultCard({ commit }: { commit: CommitResult }) {
  return (
    <ToolResultRow
      icon={GitCommitHorizontal}
      label={commit.subject}
      trailing={<span className="text-code-11">{commit.short}</span>}
      onClick={() =>
        void dispatchIntent(
          "git.reveal",
          { module: "git", kind: "commit", id: commit.sha, label: commit.short },
          { placement: "beside" },
        )
      }
    />
  );
}
