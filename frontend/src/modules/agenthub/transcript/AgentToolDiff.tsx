import { useMemo } from "react";
import { FileDiff } from "lucide-react";

import { cn } from "@/lib/utils";
import { lineDiff, type DiffLine } from "@/modules/notes/noteDiff";

export type AgentToolDiffProps = {
  path?: string | null;
  oldText?: string | null;
  newText?: string | null;
  className?: string;
};

/**
 * Agent / Tool Diff — Figma `1029:2667`.
 *
 * Additions are drawn in brand/lime, not status/success. That is deliberate in
 * the design and worth not "fixing": green would pair added with the success
 * tone and removed with the danger tone, which reads as good-change and
 * bad-change. A diff has no opinion — lime is the accent DevKit already uses
 * for "this is the new thing", and only the deletion carries a warning colour
 * because a deletion is the half you can lose.
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
  className,
}: AgentToolDiffProps) {
  const safeOld = oldText ?? "";
  const safeNew = newText ?? "";

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
        {stats.added > 0 ? (
          <span className="shrink-0 text-caption-11 whitespace-nowrap text-status-success">
            {`+${stats.added}`}
          </span>
        ) : null}
        {stats.removed > 0 ? (
          <span className="shrink-0 text-caption-11 whitespace-nowrap text-status-danger">
            {`−${stats.removed}`}
          </span>
        ) : null}
        {stats.added === 0 && stats.removed === 0 ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary">unchanged</span>
        ) : null}
      </div>

      {lines.length > 0 ? (
        <div className="w-full overflow-x-auto rounded-[6px] bg-surface-base text-code-12 select-text">
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
