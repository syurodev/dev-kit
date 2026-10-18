import * as React from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { ChevronDown, ChevronRight, Copy, Plus } from "lucide-react";

import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { SidebarInset } from "@/components/ui/sidebar-shell";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { EASE_OUT_CSS, MOTION_BASE } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { Subagent } from "../types";
import { sessionTitle } from "../documents";
import { useSessionActions } from "./actions";
import type { DismissedFailures } from "./dismissed";
import { sessionCount, type SidebarGroup } from "./model";
import { SessionBranch, type BranchContext } from "./SessionBranch";
import { buildForest, heldBranches, partitionForest, type BranchSignals } from "./tree";

export type AgentSessionGroupProps = {
  group: SidebarGroup;
  collapsed: boolean;
  /**
   * The workspace chosen in the title bar (or there is none). Any other group
   * says, while folded, how many of its sessions are running.
   */
  current: boolean;
  olderOpen: boolean;
  activeId?: string;
  pinnedIds: readonly string[];
  subagents: Record<string, Subagent[]>;
  peerUnread: Record<string, number>;
  /** What the sessions below each session are doing, over every session (tree.ts branchSignals). */
  signals: Record<string, BranchSignals>;
  /** Failures the person dismissed: those rows draw quietly. */
  dismissed?: DismissedFailures;
  /** Sessions whose children the person folded shut. */
  folded: ReadonlySet<string>;
  now: number;
  onToggleCollapsed: (key: string) => void;
  onToggleOlder: (key: string) => void;
  /** Starts a session in this group: the workspace as a whole, or the folder. */
  onNewSession?: (group: SidebarGroup) => void;
};

/**
 * The row that folds a group's older sessions: how many, a hairline to the
 * edge, and a chevron. Open, it becomes the way back. A Collapsible trigger:
 * Radix gives it aria-expanded / aria-controls and the click.
 */
function StackRow({ count, open }: { count: number; open: boolean }) {
  const label = open ? "Hide older sessions" : count === 1 ? "1 older session" : `${count} older sessions`;
  return (
    <CollapsibleTrigger asChild>
      <button
        type="button"
        data-sidebar-nav=""
        data-testid="agent-stack-toggle"
        className="mt-0.5 flex h-8 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-body-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50"
      >
        <span className="shrink-0">{label}</span>
        <span aria-hidden="true" data-testid="agent-stack-rule" className="h-px min-w-4 flex-1 bg-border-subtle" />
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
            open && "rotate-180",
          )}
        />
      </button>
    </CollapsibleTrigger>
  );
}

/**
 * One workspace / folder: a header that folds it, then its cards — the five
 * most recent plus the ones that must not hide — and the stack of the rest.
 */
function AgentSessionGroupView({
  group,
  collapsed,
  current,
  olderOpen,
  activeId,
  pinnedIds,
  subagents,
  peerUnread,
  signals,
  dismissed,
  folded,
  now,
  onToggleCollapsed,
  onToggleOlder,
  onNewSession,
}: AgentSessionGroupProps) {
  const actions = useSessionActions();

  // The group is a forest: a session with children draws them under it, and a
  // whole tree counts as one of the five recent. While the stack is open every
  // card is on screen, so the session being viewed needs no exception: pulling
  // its tree out of the stack on selection would move it between the two lists,
  // and React would remount the card — losing the keyboard focus the person
  // just used to choose it. It is drawn as an exception again the moment the
  // stack folds.
  const forest = React.useMemo(() => buildForest(group.sessions), [group.sessions]);
  const { shown, stacked } = React.useMemo(
    () => partitionForest(forest, { activeId: olderOpen ? undefined : activeId, pinnedIds }),
    [forest, activeId, olderOpen, pinnedIds],
  );
  // A branch stays open over the session on screen and a pinned one.
  const held = React.useMemo(() => heldBranches(forest, activeId, pinnedIds), [forest, activeId, pinnedIds]);
  const pinnedSet = React.useMemo(() => new Set(pinnedIds), [pinnedIds]);

  const running = React.useMemo(() => group.sessions.filter((s) => s.status === "running").length, [group.sessions]);
  const titles = React.useMemo(() => new Map(group.sessions.map((s) => [s.id, sessionTitle(s)])), [group.sessions]);

  // Rows that come and go — pulled out of the stack, a new session, one that
  // leaves for or comes back from Needs you — slide instead of cutting. A list drawn because
  // the group or the stack just opened is not animated: auto-animate only
  // reacts to changes after it attaches, and the collapse already moves.
  const listMotion = { duration: MOTION_BASE * 1000, easing: EASE_OUT_CSS };
  const [shownRef] = useAutoAnimate<HTMLDivElement>(listMotion);
  const [stackedRef] = useAutoAnimate<HTMLDivElement>(listMotion);

  // Folded and not the workspace in front of you: one compact line.
  const compact = collapsed && !current;
  const containsActive = Boolean(activeId) && group.sessions.some((s) => s.id === activeId);
  const newLabel = group.kind === "scratch" ? "New session without a project" : `New session in ${group.label}`;
  const hint = group.kind === "scratch" ? undefined : group.workspace ? group.workspace.paths.join("\n") : group.path || undefined;

  // A workspace of several folders says which one each row works in.

  const ctx: BranchContext = {
    activeId,
    pinned: pinnedSet,
    folded,
    held,
    subagents,
    peerUnread,
    signals,
    dismissed,
    titles,
    // Anything waiting on the person is drawn in Needs you, not here.
    now,
  };
  const renderTree = (nodes: typeof shown) =>
    nodes.map((node) => <SessionBranch key={node.session.id} node={node} first={false} last={false} guided={false} ctx={ctx} />);

  // Folded with something running, that is the news; otherwise the total.
  const showRunning = compact && running > 0;

  return (
    <Collapsible asChild open={!collapsed} onOpenChange={() => onToggleCollapsed(group.key)}>
      <section
        data-testid="agent-session-group"
        data-group-key={group.key}
        data-collapsed={collapsed || undefined}
        aria-label={group.label}
        className="flex flex-col pt-1"
      >
        <ContextMenu>
          <ContextMenuTrigger asChild>
            {/* Inset like the cards, so the header's hover ground lines up with theirs. */}
            <SidebarInset className="group/header relative">
              <CollapsibleTrigger asChild>
                <button
                  type="button"
                  data-sidebar-nav=""
                  data-testid="agent-group-toggle"
                  data-contains-active={containsActive || undefined}
                  title={hint}
                  className="flex h-8 w-full min-w-0 cursor-pointer items-center gap-1.5 rounded-md px-2 text-left outline-none transition-colors duration-(--motion-fast) group-hover/header:bg-state-soft hover:bg-state-soft focus-visible:ring-2 focus-visible:ring-border-focus/50"
                >
                  <ChevronRight
                    aria-hidden="true"
                    data-testid="agent-group-chevron"
                    className={cn(
                      "size-3.5 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
                      !collapsed && "rotate-90",
                    )}
                  />
                  <span
                    data-testid="agent-group-name"
                    className={cn(
                      "min-w-0 flex-1 truncate text-body-13 font-medium",
                      containsActive ? "text-text-primary" : "text-text-secondary",
                    )}
                  >
                    {group.label}
                  </span>
                  {/* Said in words to a reader; drawn as a number. */}
                  <span className="sr-only">
                    {sessionCount(group.sessions.length)}
                    {showRunning ? `, ${running} running` : ""}
                  </span>
                  {/* The + appears in this slot on hover / focus: the slot is at
                      least as wide as the +, so the name never moves. */}
                  <span
                    aria-hidden="true"
                    className={cn(
                      "flex shrink-0 items-center justify-end transition-opacity duration-(--motion-fast)",
                      onNewSession &&
                        "min-w-(--control-height-xs) group-hover/header:opacity-0 group-focus-within/header:opacity-0",
                    )}
                  >
                    {showRunning ? (
                      <span
                        data-testid="agent-group-running"
                        className="flex items-center gap-1.5 text-caption-11 text-text-secondary"
                      >
                        <span className="size-1.5 rounded-full bg-text-primary" />
                        {`${running} running`}
                      </span>
                    ) : (
                      <span
                        data-testid="agent-group-count"
                        aria-hidden="true"
                        className="text-caption-11 tabular-nums text-text-tertiary"
                      >
                        {group.sessions.length}
                      </span>
                    )}
                  </span>
                </button>
              </CollapsibleTrigger>
              {onNewSession ? (
                <ActionTooltip label={newLabel}>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    data-testid="agent-tree-project-new"
                    onClick={() => onNewSession(group)}
                    // Over the count's slot, hidden until the header is hovered
                    // or something in it has focus — and still a tab stop, so
                    // the keyboard finds it.
                    className="absolute top-1 right-2 text-text-tertiary opacity-0 transition-opacity duration-(--motion-fast) group-hover/header:opacity-100 group-focus-within/header:opacity-100 focus-visible:opacity-100"
                  >
                    <Plus aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              ) : null}
            </SidebarInset>
          </ContextMenuTrigger>
          <ContextMenuContent className="w-56">
            <ContextMenuLabel className="truncate">{group.label}</ContextMenuLabel>
            <ContextMenuSeparator />
            {onNewSession ? (
              <ContextMenuItem icon={<Plus />} onSelect={() => onNewSession(group)}>
                New session
              </ContextMenuItem>
            ) : null}
            {actions.copy && group.kind !== "scratch" ? (
              <ContextMenuItem icon={<Copy />} onSelect={() => actions.copy?.(group.path)}>
                Copy path
              </ContextMenuItem>
            ) : null}
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => onToggleCollapsed(group.key)}>{collapsed ? "Expand" : "Collapse"}</ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>

        <CollapsibleContent>
          <SidebarInset className="pt-0.5 pb-2">
            <div ref={shownRef} role="list" aria-label={`${group.label} sessions`} className="flex flex-col gap-0.5">
              {renderTree(shown)}
            </div>
            {stacked.length > 0 ? (
              <Collapsible open={olderOpen} onOpenChange={() => onToggleOlder(group.key)}>
                <StackRow count={stacked.length} open={olderOpen} />
                <CollapsibleContent>
                  <div
                    ref={stackedRef}
                    role="list"
                    aria-label={`${group.label} older sessions`}
                    className="flex flex-col gap-0.5 pb-1"
                  >
                    {renderTree(stacked)}
                  </div>
                </CollapsibleContent>
              </Collapsible>
            ) : null}
          </SidebarInset>
        </CollapsibleContent>
      </section>
    </Collapsible>
  );
}

export const AgentSessionGroup = React.memo(AgentSessionGroupView);
