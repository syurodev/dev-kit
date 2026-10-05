import { useQuery } from "@tanstack/react-query";
import { History, RefreshCw, GitCommitHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { ScrollArea } from "@/components/ui/scroll-area";
import { gitLog } from "@/bridge/git";
import { errorMessage } from "@/bridge/errors";
import { useModuleFolder } from "@/stores/workFolder";
import { openCommitDiff } from "./documents";
import { useGitRevision } from "./gitRevision";
import { filterCommits, gitUI, useGitDraft } from "./gitUI";

/** The log is navigable, bounded and searchable; commit tabs belong to the shell. */
export function GitHistoryView() {
  const { folder } = useModuleFolder("git");
  const revision = useGitRevision((s) => s.revision);
  const { historySearch, historyLimit } = useGitDraft(folder);
  const query = useQuery({
    queryKey: ["git", "history", folder, historyLimit, revision],
    queryFn: () => gitLog(historyLimit),
    placeholderData: (previous) => previous,
  });
  const commits = query.data ?? [];
  const matches = filterCommits(commits, historySearch);
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 px-4 py-3">
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        <History className="size-4 text-text-secondary" />
        <h1 className="mr-auto text-title-16">History</h1>
        <Badge variant="secondary">{commits.length} loaded</Badge>
        <Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
          <RefreshCw className="size-3.5" />
          Refresh
        </Button>
      </div>
      <Input
        aria-label="Search commits"
        placeholder="Search message, author or SHA…"
        value={historySearch}
        onChange={(e) => gitUI.setDraft(folder, { historySearch: e.target.value })}
      />
      {query.isFetching && <PendingStatus>Reading commits…</PendingStatus>}
      {query.error && (
        <FeedbackBanner
          tone="error"
          title="Could not read history"
          action={
            <Button variant="outline" onClick={() => void query.refetch()}>
              Retry
            </Button>
          }
        >
          {errorMessage(query.error)}
        </FeedbackBanner>
      )}
      {query.isPending ? null : matches.length === 0 ? (
        <EmptyState
          icon={History}
          title={historySearch ? "No matching commits" : query.error ? "History unavailable" : "No commits yet"}
          body={historySearch ? "Search applies to the loaded commits." : undefined}
          action={
            historySearch
              ? {
                  label: "Clear search",
                  onClick: () => gitUI.setDraft(folder, { historySearch: "" }),
                }
              : undefined
          }
        />
      ) : (
        <ScrollArea className="min-h-0 flex-1">
          <ul className="flex flex-col gap-1">
            {matches.map((commit) => (
              <li key={commit.sha}>
                <Button
                  variant="ghost"
                  className="h-auto min-h-15 w-full justify-start gap-3 px-3 py-2.5 text-left"
                  onClick={() => openCommitDiff(commit.sha, commit.short)}
                  aria-label={`Open commit ${commit.short}: ${commit.subject}`}
                >
                  <GitCommitHorizontal className="size-4 shrink-0 text-text-tertiary" />
                  <span className="flex min-w-0 flex-1 flex-col gap-1">
                    <span className="truncate text-body-13-medium">{commit.subject}</span>
                    <span className="truncate text-code-11 text-text-tertiary">
                      {commit.short} · {commit.author} · {commit.date.slice(0, 10)}
                    </span>
                  </span>
                </Button>
              </li>
            ))}
          </ul>
        </ScrollArea>
      )}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 text-body-12 text-text-tertiary">
        <span>
          {matches.length} shown · newest first
          {historyLimit === 500 ? " · first 500 commits" : ""}
        </span>
        {commits.length >= historyLimit && historyLimit < 500 && (
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() =>
              gitUI.setDraft(folder, {
                historyLimit: Math.min(500, historyLimit + 100),
              })
            }
          >
            Load 100 more
          </Button>
        )}
      </div>
    </div>
  );
}
