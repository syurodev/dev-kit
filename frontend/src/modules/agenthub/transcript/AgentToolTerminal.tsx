import { Terminal as TerminalIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentToolTerminalProps = {
  /**
   * Terminal output viewer for ToolCallContent of type "terminal".
   * Figma node: 1029:2721 (Agent / Tool Terminal)
   * Variants: Running / Exited / Failed
   */
  terminalId?: string | null;
  output?: string | null;
  status?: "running" | "exited" | "failed";
  exitCode?: number | null;
  className?: string;
};

export function AgentToolTerminal({
  terminalId,
  output,
  status = "exited",
  exitCode,
  className,
}: AgentToolTerminalProps) {
  const isRunning = status === "running";
  const isFailed = status === "failed" || (typeof exitCode === "number" && exitCode !== 0);

  const statusLabel = isRunning
    ? "RUNNING"
    : isFailed
      ? `FAILED${typeof exitCode === "number" ? ` (${exitCode})` : ""}`
      : `EXITED${typeof exitCode === "number" ? ` (${exitCode})` : ""}`;

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base overflow-hidden",
        className,
      )}
      data-testid="agent-tool-terminal"
      data-state={status}
    >
      <div className="flex h-(--control-height-sm) shrink-0 items-center justify-between gap-2 border-b border-border-subtle bg-surface-raised/50 px-2.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <TerminalIcon className="size-(--icon-12) text-text-tertiary shrink-0" />
          <span className="truncate text-code-12-medium text-text-secondary">
            {terminalId ?? "terminal"}
          </span>
        </div>

        <span
          className={cn(
            "text-label-11-caps shrink-0",
            isRunning && "text-status-info",
            isFailed && "text-status-danger",
            !isRunning && !isFailed && "text-text-tertiary",
          )}
        >
          {statusLabel}
        </span>
      </div>

      <pre className="overflow-x-auto p-2.5 text-code-12 text-text-primary select-text whitespace-pre">
        <code>{output || "(no output)"}</code>
      </pre>
    </div>
  );
}
