import { Terminal as TerminalIcon } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentToolTerminalProps = {
  /** The command that ran. Figma puts it in the head — it is the identity. */
  command?: string | null;
  terminalId?: string | null;
  output?: string | null;
  status?: "running" | "exited" | "failed";
  exitCode?: number | null;
  className?: string;
};

/**
 * Agent / Tool Terminal — Figma `1029:2721`, state `1029:2712` Failed.
 *
 * The head names the command, not the terminal id: a terminal ToolCallContent
 * carries an opaque `term_01H…`, which says which handle failed rather than what
 * failed. The id stays as the fallback when no command reached us.
 *
 * The right-hand slot is the exit status in Figma's own shorthand, "exit 1" in
 * status/danger. There is no uppercase EXITED badge: a zero exit is the
 * expected case and does not need a label at all.
 */
export function AgentToolTerminal({
  command,
  terminalId,
  output,
  status = "exited",
  exitCode,
  className,
}: AgentToolTerminalProps) {
  const isRunning = status === "running";
  const failed = status === "failed" || (typeof exitCode === "number" && exitCode !== 0);

  const label = isRunning
    ? "running…"
    : failed
      ? `exit ${typeof exitCode === "number" ? exitCode : 1}`
      : "";

  return (
    <div
      data-testid="agent-tool-terminal"
      data-state={status}
      className={cn(
        "flex w-full max-w-full flex-col gap-2 rounded-control bg-surface-raised p-3",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <TerminalIcon className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-code-12 text-text-primary">
          {command || terminalId || "terminal"}
        </span>
        {label ? (
          <span
            className={cn(
              "shrink-0 text-caption-11 whitespace-nowrap",
              failed ? "text-status-danger" : "text-text-secondary",
            )}
          >
            {label}
          </span>
        ) : null}
      </div>

      <pre className="w-full overflow-x-auto rounded-sm bg-surface-base p-2 text-code-11 whitespace-pre text-text-secondary select-text">
        <code>{output || "(no output)"}</code>
      </pre>
    </div>
  );
}
