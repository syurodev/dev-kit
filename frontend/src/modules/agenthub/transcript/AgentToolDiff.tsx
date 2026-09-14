import { useMemo } from "react";
import { FileDiff } from "lucide-react";

import { cn } from "@/lib/utils";
import { lineDiff, type DiffLine } from "@/modules/notes/noteDiff";

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
 * Agent / Tool Diff — Figma `1029:2667`.
 *
 * Additions are brand/lime, not status/success — deliberate, and worth not
 * "fixing". Green would pair added with success and removed with danger, which
 * reads as good-change and bad-change; a diff has no opinion. Lime is DevKit's
 * accent for "the new thing", and only deletion carries a warning colour
 * because deletion is the half you can lose.
 *
 * The counts are +N and −N (a real minus sign, U+2212, as Figma has it — a
 * hyphen next to a lime plus looks like a dash).
 *
 * Not to be confused with the 08 Diff module's ribbon; this is the inline
 * transcript card and shares none of that spec.
 */
export function AgentToolDiff({
  path,
  oldText = "",
  newText = "",
  truncated = false,
  className,
}: AgentToolDiffProps) {
  const safeOld = truncated ? "" : (oldText ?? "");
  const safeNew = truncated ? "" : (newText ?? "");

  const lines: DiffLine[] = useMemo(
    () => (!safeOld && !safeNew ? [] : lineDiff(safeOld, safeNew)),
    [safeOld, safeNew],
  );

  const stats = useMemo(() => {
    let added = 0;
    let removed = 0;
    for (const l of lines) {
      if (l.kind === "added") added++;
      if (l.kind === "removed") removed++;
    }
    return { added, removed };
  }, [lines]);

  return (
    <div
      data-testid="agent-tool-diff"
      className={cn(
        "flex w-full max-w-full flex-col gap-2 rounded-control bg-surface-raised p-3",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <FileDiff className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">
          {path ?? "diff"}
        </span>
        {/* No counts and no "unchanged": the texts are not here to count, and
            saying nothing changed about a diff too big to show would be a lie. */}
        {truncated ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary">too large to preview</span>
        ) : null}
        {!truncated && stats.added > 0 ? (
          <span className="shrink-0 text-caption-11 whitespace-nowrap text-status-success">
            {`+${stats.added}`}
          </span>
        ) : null}
        {!truncated && stats.removed > 0 ? (
          <span className="shrink-0 text-caption-11 whitespace-nowrap text-status-danger">
            {`−${stats.removed}`}
          </span>
        ) : null}
        {!truncated && stats.added === 0 && stats.removed === 0 ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary">unchanged</span>
        ) : null}
      </div>

      {truncated ? null : lines.length > 0 ? (
        <div className="w-full overflow-x-auto rounded-sm bg-surface-base text-code-12 select-text">
          {lines.map((line, idx) => (
            <div
              key={idx}
              data-kind={line.kind}
              className={cn(
                "flex min-w-max items-center py-1 pr-3 pl-4",
                line.kind === "added" && "bg-(--brand-text)/10 text-(--brand-text)",
                line.kind === "removed" && "bg-status-danger/10 text-status-danger",
                line.kind === "same" && "text-text-secondary",
              )}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "shrink-0 whitespace-pre select-none",
                  line.kind === "same" && "text-text-tertiary",
                )}
              >
                {line.kind === "added" ? "+ " : line.kind === "removed" ? "- " : "  "}
              </span>
              <span className="whitespace-pre">{line.text}</span>
            </div>
          ))}
        </div>
      ) : (
        <p className="w-full text-caption-11 text-text-tertiary">No textual change</p>
      )}
    </div>
  );
}
