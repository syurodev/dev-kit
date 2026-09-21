import { GitCommitHorizontal } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { CommitResult } from "./commitResult";

/**
 * ToolResultView for `git.commit` (docs/dev-kit/tool-result-view.mdx).
 * "View diff" goes through the existing `git.reveal` intent, by the full
 * SHA (not the short form — precise, and CommitView reads any revision
 * string git accepts).
 */
export function GitCommitResultCard({ commit }: { commit: CommitResult }) {
  return (
    <ToolResultCard
      icon={GitCommitHorizontal}
      title={`${commit.short} ${commit.subject}`}
      actionLabel="View diff"
      onAction={() =>
        void dispatchIntent(
          "git.reveal",
          { module: "git", kind: "commit", id: commit.sha, label: commit.short },
          { placement: "beside" },
        )
      }
    />
  );
}
