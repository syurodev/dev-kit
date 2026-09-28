import { Terminal as TerminalIcon } from "lucide-react";

import { cn } from "@/lib/utils";
import { FramedCard, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";

export type AgentToolTerminalProps = {
  /** The command that ran. Figma puts it in the head — it is the identity. */
  command?: string | null;
  terminalId?: string | null;
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
      : undefined;

  // A Framed card like every other block in a tool call: the header says it is
  // a terminal and how it went, the preview is what ran.
  return (
    <FramedCard data-testid="agent-tool-terminal" data-state={status} className={cn("max-w-full", className)}>
      <FramedCardHeader
        icon={TerminalIcon}
        title="Terminal"
        meta={label ? <span className={failed ? "text-status-danger" : undefined}>{label}</span> : undefined}
      />
      <FramedCardPreview>
        <p className="min-w-0 break-all whitespace-pre-wrap text-code-12 text-text-primary">
          {command || terminalId || "terminal"}
        </p>
      </FramedCardPreview>
    </FramedCard>
  );
}
