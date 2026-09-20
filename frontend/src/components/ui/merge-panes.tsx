import { cn } from "@/lib/utils";

/**
 * The three-pane merge layout: one side, an editable middle, the other side.
 *
 * It is shared rather than copied because a second three-pane merge UI is a
 * second three-pane merge UI — two places to fix a wrapping bug, two places a
 * designer has to keep in step. Sync resolves a record conflict with it
 * (Local / Merged / Remote) and Git resolves a merge conflict (Ours / Merged /
 * Theirs); the labels and the words are the caller's, the layout is not.
 *
 * It knows nothing about either conflict: no bridge call, no strategy, no
 * notion of what a side means. Dropping the middle pane is how a caller says
 * there is nothing to line-merge, only a side to pick.
 *
 * Geometry from Figma `Sync / Resolve — Merge` (781:216).
 */

const PANE = "flex min-h-0 flex-1 flex-col rounded-control border bg-surface-well pb-3";
const PANE_HEAD = "px-4 pt-3 pb-2.5 text-label-11-caps text-text-tertiary";
const PANE_BODY =
  "min-h-0 flex-1 overflow-auto px-4 text-code-12 whitespace-pre-wrap text-text-secondary";

export type MergePane = {
  /** The caps label above the pane, e.g. "Local · this device". */
  label: string;
  text: string;
};

export function MergePanes({
  left,
  right,
  merged,
  disabled,
}: {
  left: MergePane;
  right: MergePane;
  /** Absent when there is nothing to line-merge: only a side to pick. */
  merged?: { label: string; value: string; onChange: (value: string) => void };
  disabled?: boolean;
}) {
  return (
    <div className="flex min-h-0 flex-1 gap-3">
      <section className={cn(PANE, "border-border-subtle")}>
        <span className={PANE_HEAD}>{left.label}</span>
        <pre className={PANE_BODY}>{left.text}</pre>
      </section>
      {merged ? (
        <section className={cn(PANE, "border-status-info")}>
          <span className={PANE_HEAD}>{merged.label}</span>
          <textarea
            value={merged.value}
            onChange={(event) => merged.onChange(event.target.value)}
            disabled={disabled}
            spellCheck={false}
            aria-label="Merged result"
            className="min-h-0 flex-1 resize-none bg-transparent px-4 text-code-12 text-text-primary outline-none"
          />
        </section>
      ) : null}
      <section className={cn(PANE, "border-border-subtle")}>
        <span className={PANE_HEAD}>{right.label}</span>
        <pre className={PANE_BODY}>{right.text}</pre>
      </section>
    </div>
  );
}
