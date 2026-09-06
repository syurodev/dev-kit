import { useMemo } from "react";
import { FileCode } from "lucide-react";
import { cn } from "@/lib/utils";
import { lineDiff, type DiffLine } from "@/modules/notes/noteDiff";

export type AgentToolDiffProps = {
  /**
   * Diff viewer for ToolCallContent of type "diff".
   * Figma node: 1029:2667 (Agent / Tool Diff)
   */
  path?: string | null;
  oldText?: string | null;
  newText?: string | null;
  className?: string;
};

export function AgentToolDiff({
  path,
  oldText = "",
  newText = "",
  className,
}: AgentToolDiffProps) {
  const safeOldText = oldText ?? "";
  const safeNewText = newText ?? "";

  const lines: DiffLine[] = useMemo(() => {
    if (!safeOldText && !safeNewText) {
      return [];
    }
    return lineDiff(safeOldText, safeNewText);
  }, [safeOldText, safeNewText]);

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
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base overflow-hidden",
        className,
      )}
      data-testid="agent-tool-diff"
    >
      <div className="flex h-(--control-height-sm) shrink-0 items-center justify-between gap-2 border-b border-border-subtle bg-surface-raised/50 px-2.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <FileCode className="size-(--icon-12) text-text-tertiary shrink-0" />
          <span className="truncate text-code-12-medium text-text-secondary">
            {path ?? "diff"}
          </span>
        </div>

        <div className="flex items-center gap-1.5 shrink-0 text-caption-11">
          {stats.added > 0 && (
            <span className="text-status-success">{`+${stats.added}`}</span>
          )}
          {stats.removed > 0 && (
            <span className="text-status-danger">{`-${stats.removed}`}</span>
          )}
          {stats.added === 0 && stats.removed === 0 && (
            <span className="text-text-tertiary">unchanged</span>
          )}
        </div>
      </div>

      {lines.length > 0 ? (
        <div className="overflow-x-auto py-1 text-code-11 select-text">
          {lines.map((line, idx) => (
            <div
              key={idx}
              className={cn(
                "flex items-stretch min-w-max px-2.5 py-0.5",
                line.kind === "added" && "bg-status-success/10 text-status-success",
                line.kind === "removed" && "bg-status-danger/10 text-status-danger",
                line.kind === "same" && "text-text-secondary",
              )}
            >
              <span className="w-5 shrink-0 select-none text-text-tertiary">
                {line.kind === "added" ? "+" : line.kind === "removed" ? "-" : " "}
              </span>
              <span className="whitespace-pre">{line.text}</span>
            </div>
          ))}
        </div>
      ) : (
        <div className="p-3 text-caption-11 text-text-tertiary">
          (empty diff)
        </div>
      )}
    </div>
  );
}
