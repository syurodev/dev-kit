import {
  CheckCircle2,
  Circle,
  FileEdit,
  FileText,
  Loader2,
  Search,
  ShieldAlert,
  Terminal,
  Wrench,
  XCircle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { ToolCall } from "../types";
import { AgentFileChip } from "./AgentFileChip";
import { AgentToolDiff } from "./AgentToolDiff";
import { AgentToolTerminal } from "./AgentToolTerminal";

export type AgentToolCallProps = {
  /**
   * Tool execution row.
   * Figma node: 947:2214 (Agent / Tool Call)
   * Variants: Pending / Running / Awaiting / Done / Failed, + kind icon + optional locations row
   */
  call: ToolCall;
  className?: string;
};

function getKindIcon(kind?: string) {
  const k = kind?.toLowerCase() ?? "";
  if (k.includes("read") || k.includes("view") || k.includes("cat")) {
    return FileText;
  }
  if (k.includes("edit") || k.includes("write") || k.includes("patch")) {
    return FileEdit;
  }
  if (k.includes("exec") || k.includes("bash") || k.includes("run") || k.includes("command")) {
    return Terminal;
  }
  if (k.includes("search") || k.includes("grep") || k.includes("glob") || k.includes("find")) {
    return Search;
  }
  return Wrench;
}

function getStatusDetails(status?: string) {
  const s = status?.toLowerCase() ?? "completed";
  switch (s) {
    case "pending":
      return {
        label: "PENDING",
        icon: Circle,
        iconClass: "text-text-tertiary",
        badgeClass: "text-text-tertiary",
      };
    case "in_progress":
    case "running":
      return {
        label: "RUNNING",
        icon: Loader2,
        iconClass: "text-status-info animate-spin",
        badgeClass: "text-status-info",
      };
    case "awaiting_permission":
    case "awaiting":
      return {
        label: "AWAITING",
        icon: ShieldAlert,
        iconClass: "text-status-warning",
        badgeClass: "text-status-warning",
      };
    case "failed":
    case "error":
      return {
        label: "FAILED",
        icon: XCircle,
        iconClass: "text-status-danger",
        badgeClass: "text-status-danger",
      };
    case "completed":
    case "done":
    default:
      return {
        label: "DONE",
        icon: CheckCircle2,
        iconClass: "text-status-success",
        badgeClass: "text-status-success",
      };
  }
}

export function AgentToolCall({ call, className }: AgentToolCallProps) {
  const KindIcon = getKindIcon(call.kind);
  const statusInfo = getStatusDetails(call.status);
  const StatusIcon = statusInfo.icon;

  const title = call.title || call.kind || call.id || "Tool execution";
  const hasLocations = Boolean(call.locations && call.locations.length > 0);
  const hasContent = Boolean(call.content && call.content.length > 0);

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base p-2.5 gap-2",
        className,
      )}
      data-testid="agent-tool-call"
      data-status={call.status ?? "completed"}
    >
      <div className="flex items-center justify-between gap-2 min-w-0">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <KindIcon className="size-(--icon-14) text-text-tertiary shrink-0" />
          <span className="truncate text-body-13-medium text-text-primary">
            {title}
          </span>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <StatusIcon className={cn("size-(--icon-14) shrink-0", statusInfo.iconClass)} />
          <span className={cn("text-label-11-caps", statusInfo.badgeClass)}>
            {statusInfo.label}
          </span>
        </div>
      </div>

      {hasLocations && (
        <div className="flex flex-wrap gap-1.5 pt-1 border-t border-border-subtle/50">
          {call.locations!.map((loc, idx) => (
            <AgentFileChip
              key={`${loc.path}-${loc.line ?? idx}`}
              path={loc.path}
              line={loc.line}
            />
          ))}
        </div>
      )}

      {hasContent && (
        <div className="flex flex-col gap-2 pt-1 border-t border-border-subtle/50">
          {call.content!.map((item, idx) => {
            if (item.type === "diff") {
              return (
                <AgentToolDiff
                  key={idx}
                  path={item.path}
                  oldText={item.oldText}
                  newText={item.newText}
                />
              );
            }
            if (item.type === "terminal") {
              return (
                <AgentToolTerminal
                  key={idx}
                  terminalId={item.terminalId}
                />
              );
            }
            if (item.content?.text) {
              return (
                <pre
                  key={idx}
                  className="overflow-x-auto rounded-sm bg-surface-raised/60 p-2 text-code-12 text-text-secondary select-text whitespace-pre"
                >
                  <code>{item.content.text}</code>
                </pre>
              );
            }
            return null;
          })}
        </div>
      )}
    </div>
  );
}
