import * as React from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { ChevronDown, ChevronRight, Copy, Plus } from "lucide-react";

import { ActionTooltip } from "@/components/ui/action-tooltip";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Button } from "@/components/ui/button";
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
import { AgentSessionCard } from "./AgentSessionCard";
import { projectLabel } from "./grouping";
import { partitionGroup, sessionCount, type SidebarGroup } from "./model";

const NO_SUBAGENTS: readonly Subagent[] = [];

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
  now: number;
  onToggleCollapsed: (key: string) => void;
  onToggleOlder: (key: string) => void;
  /** Starts a session in this group: the workspace as a whole, or the folder. */
  onNewSession?: (group: SidebarGroup) => void;
};

/**
 * The row that folds a group's older sessions: how many, a hairline to the
 * edge, and a chevron. Open, it becomes the way back.
 */
function StackRow({ count, open, onToggle, controlsId }: { count: number; open: boolean; onToggle: () => void; controlsId: string }) {
  const label = open ? "Hide older sessions" : count === 1 ? "1 older session" : `${count} older sessions`;
  return (
    <button
      type="button"
      data-sidebar-nav=""
      data-testid="agent-stack-toggle"
      aria-expanded={open}
      aria-controls={controlsId}
      onClick={onToggle}
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
  now,
  onToggleCollapsed,
  onToggleOlder,
  onNewSession,
}: AgentSessionGroupProps) {
  const actions = useSessionActions();
  const bodyId = React.useId();
  const stackId = React.useId();

  // While the stack is open every card is on screen, so the session being
  // viewed needs no exception: pulling it out of the stack on selection would
  // move its card between the two lists, and React would remount it — losing
  // the keyboard focus the person just used to choose it. It is drawn as an
  // exception again the moment the stack folds.
  const { shown, stacked } = React.useMemo(
    () =>
      partitionGroup(group.sessions, {
        activeId: olderOpen ? undefined : activeId,
        pinnedIds,
      }),
    [group.sessions, activeId, olderOpen, pinnedIds],
  );

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
  const severalFolders = Boolean(group.workspace && group.workspace.paths.length > 1);

  const renderCard = (session: (typeof shown)[number]) => (
    <AgentSessionCard
      key={session.id}
      session={session}
      selected={session.id === activeId}
      // Anything waiting on the person is drawn in Needs you, not here.
      ask={null}
      folder={severalFolders ? projectLabel(session.projectPath) : null}
      pinned={pinnedIds.includes(session.id)}
      subagents={subagents[session.id] ?? NO_SUBAGENTS}
      parentTitle={session.parentSessionId ? titles.get(session.parentSessionId) : undefined}
      now={now}
    />
  );

  return (
    <section
      data-testid="agent-session-group"
      data-group-key={group.key}
      data-collapsed={collapsed || undefined}
      aria-label={group.label}
      className="flex flex-col"
    >
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div className={cn("group/header flex items-center gap-1 px-2", compact ? "h-8" : "pt-4 pb-1.5")}>
            <button
              type="button"
              data-sidebar-nav=""
              data-testid="agent-group-toggle"
              data-contains-active={containsActive || undefined}
              aria-expanded={!collapsed}
              aria-controls={bodyId}
              title={hint}
              onClick={() => onToggleCollapsed(group.key)}
              className="flex h-7 min-w-0 flex-1 cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50"
            >
              {/* Inline, so the count sits as a superscript after the name and
                  both truncate together. */}
              <span className="min-w-0 truncate">
                <span
                  data-testid="agent-group-name"
                  className={cn("text-caption-11", containsActive ? "text-text-secondary" : "text-text-tertiary")}
                >
                  {group.label}
                </span>
                {/* Light: the count is context. Said in words to a reader. */}
                <sup
                  data-testid="agent-group-count"
                  aria-hidden="true"
                  className="ml-0.5 text-caption-11 text-text-tertiary/70 tabular-nums"
                >
                  {group.sessions.length}
                </sup>
                <span className="sr-only">{sessionCount(group.sessions.length)}</span>
              </span>
              <ChevronRight
                aria-hidden="true"
                className={cn(
                  "size-3 shrink-0 text-text-tertiary opacity-0 transition duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
                  "group-hover/header:opacity-100 group-focus-within/header:opacity-100",
                  !collapsed && "rotate-90",
                )}
              />
              {compact && running > 0 ? (
                <span
                  data-testid="agent-group-running"
                  className="ml-auto flex shrink-0 items-center gap-1 text-caption-11 text-text-tertiary"
                >
                  <span aria-hidden="true" className="size-1.5 rounded-full bg-text-primary" />
                  {`${running} running`}
                </span>
              ) : null}
            </button>
            {onNewSession ? (
              <ActionTooltip label={newLabel}>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  data-testid="agent-tree-project-new"
                  onClick={() => onNewSession(group)}
                  // Hidden until the header is hovered or something in it has
                  // focus — and still a tab stop, so the keyboard finds it.
                  className="shrink-0 text-text-tertiary opacity-0 transition-opacity duration-(--motion-fast) group-hover/header:opacity-100 group-focus-within/header:opacity-100 focus-visible:opacity-100"
                >
                  <Plus aria-hidden="true" />
                </Button>
              </ActionTooltip>
            ) : null}
          </div>
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

      <AnimatedCollapse open={!collapsed}>
        <SidebarInset id={bodyId} className="pt-0.5 pb-1">
          <div ref={shownRef} role="list" aria-label={`${group.label} sessions`} className="flex flex-col gap-0.5">
            {shown.map((session) => renderCard(session))}
          </div>
          {stacked.length > 0 ? (
            <>
              <StackRow
                count={stacked.length}
                open={olderOpen}
                onToggle={() => onToggleOlder(group.key)}
                controlsId={stackId}
              />
              <AnimatedCollapse open={olderOpen}>
                <div
                  ref={stackedRef}
                  id={stackId}
                  role="list"
                  aria-label={`${group.label} older sessions`}
                  className="flex flex-col gap-0.5 pb-1"
                >
                  {stacked.map((session) => renderCard(session))}
                </div>
              </AnimatedCollapse>
            </>
          ) : null}
        </SidebarInset>
      </AnimatedCollapse>
    </section>
  );
}

export const AgentSessionGroup = React.memo(AgentSessionGroupView);
