import { FileDiffPane } from "../surfaces/FileDiffPane";

export type AgentToolDiffProps = {
  path?: string | null;
  oldText?: string | null;
  newText?: string | null;
  /** The page read dropped this diff's texts because it was too large to
   * preview; the tool call's "Show full output" loads it whole. */
  truncated?: boolean;
  className?: string;
};

/**
 * A file a tool changed, in the transcript — the same 08 Diff pane the
 * permission card asks with (FileDiffPane), so the edit you approved and the
 * edit that happened are read in one language: the same tints, the same
 * folding, the same syntax colours and line numbers, and the same expansion to
 * two panes for the whole file.
 *
 * It used to be its own renderer: a header, a full unfolded diff, and no
 * colours beyond the row tints. What it kept is the vocabulary — additions are
 * brand/lime rather than status/success, deliberately, because green would
 * pair added with success and removed with danger, which reads as good-change
 * and bad-change, and a diff has no opinion. Lime is DevKit's accent for "the
 * new thing"; only deletion carries a warning colour, because deletion is the
 * half you can lose.
 */
export function AgentToolDiff({
  path,
  oldText = "",
  newText = "",
  truncated = false,
  className,
}: AgentToolDiffProps) {
  return (
    <div data-testid="agent-tool-diff" className={className}>
      <FileDiffPane
        diff={{
          path: path ?? "",
          before: oldText ?? "",
          after: newText ?? "",
          truncated,
        }}
        labels={{ before: "Before", after: "After" }}
        testIdPrefix="tool-diff"
      />
    </div>
  );
}
