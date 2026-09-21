import { FileDiff } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
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

/** A commit can touch many files; the row shows this many and counts the rest. */
const MAX_ROWS = 8;

/**
 * ToolResultView for `git.diff` (docs/dev-kit/tool-result-view.mdx). File
 * names and line counts, not the diff itself — AgentToolDiff already owns
 * showing a full diff inline. Only clickable for a call against a specific
 * revision (`rev` non-empty): the work tree diff has no view of its own to
 * reopen faithfully (see git.reveal in manifest.ts), so the row renders
 * plain, not a button, when `rev` is empty.
 */
export function GitDiffResultCard({ changes, rev }: { changes: DiffChange[]; rev: string }) {
  const added = changes.reduce((sum, c) => sum + Math.max(c.added, 0), 0);
  const removed = changes.reduce((sum, c) => sum + Math.max(c.removed, 0), 0);
  const shown = changes.slice(0, MAX_ROWS);

  return (
    <div className="flex w-full min-w-0 flex-col gap-1">
      <ToolResultRow
        icon={FileDiff}
        label={`${changes.length} file${changes.length === 1 ? "" : "s"} changed`}
        trailing={`+${added} -${removed}`}
        onClick={
          rev
            ? () =>
                void dispatchIntent(
                  "git.reveal",
                  { module: "git", kind: "commit", id: rev, label: rev },
                  { placement: "beside" },
                )
            : undefined
        }
      />
      <ul className="flex flex-col gap-0.5 pl-4.5">
        {shown.map((change) => (
          <li key={change.path} className="truncate text-code-12 text-text-tertiary">
            <span className="text-text-tertiary/70">{KIND_MARK[change.kind]}</span> {fileName(change.path)}
          </li>
        ))}
        {changes.length > MAX_ROWS ? (
          <li className="text-code-12 text-text-tertiary/70">+{changes.length - MAX_ROWS} more</li>
        ) : null}
      </ul>
    </div>
  );
}
