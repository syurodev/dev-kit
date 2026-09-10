import {
  FileEdit,
  FileText,
  Search,
  Terminal,
  Wrench,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { ToolCall } from "../types";
import { AgentFileChip } from "./AgentFileChip";
import { AgentToolDiff } from "./AgentToolDiff";
import { AgentToolTerminal } from "./AgentToolTerminal";

export type AgentToolCallProps = {
  call: ToolCall;
  className?: string;
};

/**
 * Agent / Tool Call — Figma `947:2214`, states `947:2191` Running,
 * `947:2205` Done, `947:2213` Failed.
 *
 * Figma's note: "every session/update tool_call carries a status, so the
 * transcript shows the whole arc — queued, running, blocked on permission,
 * finished, failed. Failed keeps its reason inline; a red dot alone tells you
 * nothing to act on."
 *
 * One row: dot, kind icon, tool name, then the right-hand slot carrying
 * whatever the status has to say — "running…", the result, "exit 1". That slot
 * is the status, so there is no uppercase DONE/FAILED badge: it would say the
 * same thing twice and leave no room for the result.
 *
 * Failed is the only state that changes the ground (feedback/danger-soft) and
 * the only one that adds a second line — the reason.
 *
 * Dots, from the exported SVGs: running status/info, done
 * text/tertiary, failed status/danger. Done is deliberately grey: a
 * finished tool call is not good news, it is just finished.
 */
function kindIcon(kind?: string) {
  const k = kind?.toLowerCase() ?? "";
  if (k.includes("read") || k.includes("view") || k.includes("cat")) return FileText;
  if (k.includes("edit") || k.includes("write") || k.includes("patch")) return FileEdit;
  if (k.includes("exec") || k.includes("bash") || k.includes("run") || k.includes("command"))
    return Terminal;
  if (k.includes("search") || k.includes("grep") || k.includes("glob") || k.includes("find"))
    return Search;
  return Wrench;
}

type ToolState = "pending" | "running" | "awaiting" | "done" | "failed";

function toToolState(status?: string): ToolState {
  switch (status?.toLowerCase()) {
    case "pending":
      return "pending";
    case "in_progress":
    case "running":
      return "running";
    case "awaiting_permission":
    case "awaiting":
      return "awaiting";
    case "failed":
    case "error":
      return "failed";
    default:
      return "done";
  }
}

const DOT: Record<ToolState, string> = {
  pending: "border border-text-tertiary",
  running: "bg-status-info",
  awaiting: "bg-status-warning",
  done: "bg-text-tertiary",
  failed: "bg-status-danger",
};

/** What the right-hand slot says when the call has no result of its own. */
const FALLBACK: Record<ToolState, string> = {
  pending: "queued",
  running: "running…",
  awaiting: "awaiting permission",
  done: "",
  failed: "failed",
};

/**
 * Whether rawOutput can stand in the header's status slot.
 *
 * A JSON document never can, however short: an MCP tool answers with one, and
 * `{"result":[{"id":"caad65a3…"}]}` is both unreadable at 11px and unbounded —
 * it is the payload, not a summary of it. Length alone was the wrong test; a
 * 67-character document passed it.
 */
function isOneLineStatus(raw: string): boolean {
  const text = raw.trim();
  if (!text || text.includes("\n") || text.length > 60) return false;
  return !text.startsWith("{") && !text.startsWith("[");
}

export function AgentToolCall({ call, className }: AgentToolCallProps) {
  const KindIcon = kindIcon(call.kind);
  const state = toToolState(call.status);
  const title = call.title || call.kind || call.id || "Tool execution";

  const locations = call.locations ?? [];
  const content = call.content ?? [];
  // rawOutput is `unknown` off the wire — an object stringifies to
  // "[object Object]", which is worse than saying nothing.
  const raw = typeof call.rawOutput === "string" ? call.rawOutput : "";
  // Figma's right-hand slot holds a status or a one-line result ("running…",
  // "exit 1", "POST /refunds → RefundController.create"). An MCP tool returns
  // a whole JSON document there; putting it in a nowrap, no-shrink span made
  // the row wider than the module and gave the transcript a horizontal
  // scrollbar. Long or multi-line output belongs in the block below, which
  // scrolls inside itself.
  const summary = isOneLineStatus(raw) ? raw : "";
  const result = state === "failed" ? FALLBACK.failed : summary || FALLBACK[state];
  const reason = state === "failed" ? raw : "";

  return (
    <div
      data-testid="agent-tool-call"
      data-status={call.status ?? "completed"}
      data-state={state}
      className={cn(
        "flex w-full max-w-full flex-col gap-2 rounded-control p-3",
        state === "failed" ? "bg-(--feedback-danger-soft)" : "bg-(--state-soft)",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span
          aria-hidden="true"
          className={cn("size-1.5 shrink-0 rounded-full", DOT[state])}
        />
        <KindIcon className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="shrink-0 text-body-12 whitespace-nowrap text-text-secondary">
          {title}
        </span>
        <span className="min-w-0 flex-1" />
        {result ? (
          <span
            data-testid="tool-call-result"
            className={cn(
              "min-w-0 max-w-[50%] shrink truncate text-caption-11 whitespace-nowrap",
              state === "failed"
                ? "text-status-danger"
                : state === "done"
                  ? "text-text-tertiary"
                  : "text-text-secondary",
            )}
          >
            {result}
          </span>
        ) : null}
      </div>

      {locations.length > 0 ? (
        <div className="flex w-full flex-wrap items-center gap-1">
          {locations.map((loc, idx) => (
            <AgentFileChip
              key={`${loc.path}-${loc.line ?? idx}`}
              path={loc.path}
              line={loc.line}
            />
          ))}
        </div>
      ) : null}

      {/* Figma: failed keeps its reason inline. A red dot alone is not actionable. */}
      {reason ? (
        <p
          data-testid="tool-call-reason"
          className="w-full max-w-full text-caption-11 break-all whitespace-pre-wrap text-text-secondary"
        >
          {reason}
        </p>
      ) : null}

      {content.map((item, idx) => {
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
          return <AgentToolTerminal key={idx} terminalId={item.terminalId} />;
        }
        if (item.content?.text) {
          return (
            <pre
              key={idx}
              className="w-full overflow-x-auto rounded-sm bg-surface-base p-2 text-code-11 whitespace-pre text-text-secondary select-text"
            >
              <code>{item.content.text}</code>
            </pre>
          );
        }
        return null;
      })}
    </div>
  );
}
