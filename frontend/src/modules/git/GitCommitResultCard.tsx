import { GitCommitHorizontal } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { CommitResult } from "./commitResult";

/** `git.commit` — the commit made: its subject, its SHA, and a way to its diff. */
export function GitCommitResultCard({ commit }: { commit: CommitResult }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={GitCommitHorizontal}
        title="Commit"
        meta={<span className="text-code-11">{commit.short}</span>}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "git.reveal",
                { module: "git", kind: "commit", id: commit.sha, label: commit.short },
                { placement: "beside" },
              )
            }
          >
            View diff
          </FramedCardAction>
        }
      />
      <FramedCardPreview>
        <p className="min-w-0 truncate text-body-13 text-text-primary" title={commit.subject}>
          {commit.subject}
        </p>
      </FramedCardPreview>
    </FramedCard>
  );
}
