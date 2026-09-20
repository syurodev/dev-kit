import { useEffect, useState } from "react";
import { History } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { gitLog, type Commit } from "@/bridge/git";

/** The branch's history, linear. There is no branch-graph canvas in v1. */
export function GitHistoryView() {
  const [commits, setCommits] = useState<Commit[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    gitLog(100)
      .then(setCommits)
      .catch((e: Error) => setError(e.message));
  }, []);

  if (error) {
    return <EmptyState icon={History} title="No history" body={error} />;
  }
  if (!commits) {
    return null;
  }
  if (commits.length === 0) {
    return <EmptyState icon={History} title="No commits yet" body="This repository is empty." />;
  }
  return (
    <ul className="h-full overflow-auto">
      {commits.map((commit) => (
        <li key={commit.sha} className="border-b border-border-subtle px-3 py-2 last:border-b-0">
          <div className="flex items-baseline gap-2">
            <code className="text-xs text-fg-muted">{commit.short}</code>
            <span className="min-w-0 flex-1 truncate text-sm">{commit.subject}</span>
          </div>
          <div className="text-xs text-fg-muted">
            {commit.author} · {commit.date.slice(0, 10)}
          </div>
        </li>
      ))}
    </ul>
  );
}
