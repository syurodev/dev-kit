import { FileDiff } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardMore, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
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

const MAX_ROWS = 8;

/** Added and removed line counts, each in its own colour; a binary file counts -1 and shows none. */
function LineCounts({ added, removed }: { added: number; removed: number }) {
  return (
    <>
      {added > 0 ? <span className="text-status-success">+{added}</span> : null}
      {added > 0 && removed > 0 ? " " : null}
      {removed > 0 ? <span className="text-status-danger">−{removed}</span> : null}
    </>
  );
}

/**
 * `git.diff` — which files changed and by how much, not the diff itself
 * (AgentToolDiff is for that). View diff only when the tool diffed a revision:
 * a work-tree diff has no view that shows again what the tool saw.
 */
export function GitDiffResultCard({ changes, rev }: { changes: DiffChange[]; rev: string }) {
  const added = changes.reduce((sum, c) => sum + Math.max(c.added, 0), 0);
  const removed = changes.reduce((sum, c) => sum + Math.max(c.removed, 0), 0);
  const shown = changes.slice(0, MAX_ROWS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={FileDiff}
        title={`${changes.length} file${changes.length === 1 ? "" : "s"} changed`}
        meta={<LineCounts added={added} removed={removed} />}
        action={
          rev ? (
            <FramedCardAction
              onClick={() =>
                void dispatchIntent(
                  "git.reveal",
                  { module: "git", kind: "commit", id: rev, label: rev },
                  { placement: "beside" },
                )
              }
            >
              View diff
            </FramedCardAction>
          ) : undefined
        }
      />
      <FramedCardPreview flush>
        {shown.map((change) => (
          <FramedCardRow
            key={change.path}
            label={
              <span className="text-code-12" title={change.path}>
                <span className="text-text-tertiary">{KIND_MARK[change.kind]}</span> {fileName(change.path)}
              </span>
            }
            trailing={<LineCounts added={change.added} removed={change.removed} />}
          />
        ))}
        <FramedCardMore count={changes.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
