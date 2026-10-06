import { GitCommitHorizontal, TriangleAlert } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { HistoryResult } from "./commitResult";
import { GitCommitResultCard } from "./GitCommitResultCard";

/** How many conflicted paths the card lists before folding the rest into a count. */
const SHOWN_PATHS = 5;

/**
 * `git.rebase`, `git.cherryPick`, `git.revert` — the branch's newest commit
 * once the operation finished, or, when it stopped, the paths waiting to be
 * resolved and a way to them. `repo` is the absolute folder the call named,
 * if it named one by path: Changes opens there rather than on whichever
 * repository the app has open.
 */
export function GitHistoryResultCard({ result, heading, repo }: { result: HistoryResult; heading?: string; repo?: string }) {
  if (!result.stopped && result.head) return <GitCommitResultCard commit={result.head} heading={heading} />;
  const shown = result.conflicted.slice(0, SHOWN_PATHS);
  const more = result.conflicted.length - shown.length;
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={result.stopped ? TriangleAlert : GitCommitHorizontal}
        title={`${heading ?? "History"} · stopped on ${result.conflicted.length || "a"} conflict${result.conflicted.length === 1 ? "" : "s"}`}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent("git.reveal", { module: "git", kind: "worktree", id: repo ?? "", label: "Changes" }, { placement: "beside" })
            }
          >
            Open changes
          </FramedCardAction>
        }
      />
      {shown.length > 0 ? (
        <FramedCardPreview>
          <ul className="flex min-w-0 flex-col gap-0.5">
            {shown.map((path) => (
              <li key={path} className="min-w-0 truncate text-code-11 text-text-secondary" title={path}>
                {path}
              </li>
            ))}
            {more > 0 ? <li className="text-caption-11 text-text-tertiary">{`and ${more} more`}</li> : null}
          </ul>
        </FramedCardPreview>
      ) : null}
    </FramedCard>
  );
}
