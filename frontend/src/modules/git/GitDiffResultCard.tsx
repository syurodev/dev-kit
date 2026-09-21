import { FileDiff } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { DiffChange } from "./diffResult";

const KIND_MARK: Record<DiffChange["kind"], string> = {
  added: "A",
  modified: "M",
  deleted: "D",
  typechange: "T",
};

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

/**
 * ToolResultView for `git.diff` (docs/dev-kit/tool-result-view.mdx). File
 * names and line counts, not the diff itself — AgentToolDiff already owns
 * showing a full diff inline. "View diff" only appears for a call against a
 * specific revision (`rev` non-empty): the work tree diff has no view of
 * its own to reopen faithfully (see git.reveal in manifest.ts).
 */
export function GitDiffResultCard({ changes, rev }: { changes: DiffChange[]; rev: string }) {
  const added = changes.reduce((sum, c) => sum + Math.max(c.added, 0), 0);
  const removed = changes.reduce((sum, c) => sum + Math.max(c.removed, 0), 0);

  return (
    <ToolResultCard
      icon={FileDiff}
      title={`${changes.length} file${changes.length === 1 ? "" : "s"} changed`}
      subtitle={`+${added} -${removed}`}
      actionLabel={rev ? "View diff" : undefined}
      onAction={
        rev
          ? () =>
              void dispatchIntent(
                "git.reveal",
                { module: "git", kind: "commit", id: rev, label: rev },
                { placement: "beside" },
              )
          : undefined
      }
    >
      <ul className="flex flex-col gap-0.5">
        {changes.slice(0, 8).map((change) => (
          <li key={change.path} className="truncate text-code-12 text-text-tertiary">
            <span className="text-text-tertiary/70">{KIND_MARK[change.kind]}</span> {fileName(change.path)}
          </li>
        ))}
        {changes.length > 8 ? (
          <li className="text-code-12 text-text-tertiary/70">+{changes.length - 8} more</li>
        ) : null}
      </ul>
    </ToolResultCard>
  );
}
