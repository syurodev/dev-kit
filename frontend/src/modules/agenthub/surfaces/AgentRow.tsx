import * as React from "react";
import { Pin } from "lucide-react";

import { cn } from "@/lib/utils";
import { AgentState, type AgentStatus } from "./AgentState";
import { VendorIcon } from "./VendorIcon";
import type { AgentSession } from "@/bridge/agenthub";

/**
 * Extends the button's own props on purpose. A closed prop list here silently
 * broke the sidebar's context menu: Radix's ContextMenuTrigger with `asChild`
 * clones its child and hands it `onContextMenu`, `ref` and its data attributes,
 * and a component that neither spreads nor forwards them drops the lot — the
 * trigger bound to nothing and the webview's own Reload/Inspect menu answered
 * the right-click instead. Anything wrapping a row needs its props to arrive.
 */
export type AgentRowProps = React.ComponentProps<"button"> & {
  session: AgentSession;
  selected?: boolean;
  turnCount?: number;
  /** Sorted to the top of the tree; marked so the order is explicable. */
  pinned?: boolean;
};

/**
 * Agent / Row — Figma `906:35`, states `906:12` Rest, `906:23` Hover,
 * `906:34` Selected.
 *
 * Figma's note removes a whole axis: "Status lives on the nested Agent / State
 * instance, so a row does not need a variant per status." The three variants
 * are only backgrounds; everything a status changes is inside AgentState. A
 * local status switch here would be a second dot-and-colour table in a module
 * that already has one.
 *
 * The three fills, read off the rendered node because they are alphas rather
 * than named tokens on the frame:
 *
 *   rest      rgb(29,29,29) — the page's own ground, i.e. no fill at all
 *   hover     rgb(42,42,42) — white at 6% over it, state/soft
 *   selected  rgb(35,33,44) — surface/raised, plus a border/subtle hairline
 *
 * Two lines: the name with its vendor mark and live state, then the project and
 * the turn count in the quiet tier.
 */

/**
 * The bridge's SessionStatus and Figma's Agent / State are two vocabularies:
 * the backend says `awaiting_permission`, the design says `awaiting`. Mapping
 * them in one place beats five call sites each doing their own string compare.
 */
function toAgentStatus(status: string): AgentStatus {
  switch (status) {
    case "running":
      return "running";
    case "awaiting_permission":
      return "awaiting";
    case "error":
      return "error";
    case "closed":
      return "closed";
    default:
      return "idle";
  }
}

export function AgentRow({
  session,
  selected = false,
  turnCount,
  pinned = false,
  className,
  ...props
}: AgentRowProps) {
  const title = session.title || `Session ${session.id.slice(0, 8)}`;
  const project = session.projectPath?.split("/").filter(Boolean).pop() ?? "";

  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      data-testid="agent-row"
      data-selected={selected}
      data-pinned={pinned}
      className={cn(
        "flex w-full max-w-66 items-center gap-3 rounded-md px-3 py-2 text-left transition-colors cursor-pointer",
        selected
          ? "border border-border-subtle bg-surface-raised"
          : "border border-transparent bg-transparent hover:bg-(--state-soft)",
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex w-full items-center gap-2">
          <VendorIcon vendor={session.agentType} className="size-3 text-icon-secondary" />
          <span className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">
            {title}
          </span>
          <AgentState status={toAgentStatus(session.status)} />
        </div>
        <div className="flex w-full items-center gap-2 text-caption-11 text-text-tertiary">
          {pinned ? (
            <Pin
              aria-label="Pinned"
              data-testid="agent-row-pin"
              className="size-2.5 shrink-0"
            />
          ) : null}
          {project ? <span className="truncate">{project}</span> : null}
          {typeof turnCount === "number" ? (
            <span className="shrink-0">{`· ${turnCount} turns`}</span>
          ) : null}
        </div>
      </div>
    </button>
  );
}
