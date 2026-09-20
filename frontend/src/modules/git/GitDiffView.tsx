import { useEffect, useState } from "react";
import { FileDiff } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { gitDiff, type Change } from "@/bridge/git";

const KIND_MARK: Record<Change["kind"], string> = {
  added: "A",
  modified: "M",
  deleted: "D",
  typechange: "T",
};

/** Per-file change counts. A binary file reports itself rather than showing 0/0. */
export function GitDiffView({ params }: { params?: Record<string, unknown> }) {
  const rev = typeof params?.rev === "string" ? params.rev : "";
  const [changes, setChanges] = useState<Change[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    gitDiff(rev, false)
      .then(setChanges)
      .catch((e: Error) => setError(e.message));
  }, [rev]);

  if (error) {
    return <EmptyState icon={FileDiff} title="No diff" body={error} />;
  }
  if (!changes) {
    return null;
  }
  if (changes.length === 0) {
    return <EmptyState icon={FileDiff} title="Nothing changed" body="There is no diff to show." />;
  }
  return (
    <ul className="h-full overflow-auto">
      {changes.map((change) => (
        <li
          key={change.path}
          className="flex items-center gap-3 border-b border-border-subtle px-3 py-1.5 last:border-b-0"
        >
          <span className="w-4 text-center text-xs text-fg-muted">{KIND_MARK[change.kind]}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs">{change.path}</span>
          {change.added < 0 ? (
            <span className="text-xs text-fg-muted">binary</span>
          ) : (
            <span className="text-xs">
              <span className="text-emerald-500">+{change.added}</span>{" "}
              <span className="text-red-500">-{change.removed}</span>
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}
