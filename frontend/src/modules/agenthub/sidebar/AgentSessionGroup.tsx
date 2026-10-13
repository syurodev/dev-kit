import * as React from "react";
import { ChevronRight, Copy, Folder, FolderOpen, FolderX, GalleryVerticalEnd, Layers, Plus } from "lucide-react";

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
import { cn } from "@/lib/utils";
import type { Subagent } from "../types";
import { sessionTitle } from "../documents";
import { useSessionActions } from "./actions";
import { AgentSessionCard } from "./AgentSessionCard";
import { useRisingEdge } from "./hooks";
import { partitionGroup, waitingCount, attentionKind, type Attention, type SidebarGroup } from "./model";

const NO_SUBAGENTS: readonly Subagent[] = [];

export type AgentSessionGroupProps = {
  group: SidebarGroup;
  collapsed: boolean;
  olderOpen: boolean;
  activeId?: string;
  pinnedIds: readonly string[];
  attention: Record<string, Attention>;
  subagents: Record<string, Subagent[]>;
  now: number;
  onToggleCollapsed: (key: string) => void;
  onToggleOlder: (key: string) => void;
  /** Starts a session in this group: the workspace as a whole, or the folder. */
  onNewSession?: (group: SidebarGroup) => void;
};

function GroupIcon({ group, collapsed }: { group: SidebarGroup; collapsed: boolean }) {
  const cls = "size-3.5 shrink-0 text-icon-secondary";
  if (group.kind === "workspace") return <Layers aria-hidden="true" className={cls} />;
  if (group.kind === "scratch") return <FolderX aria-hidden="true" className={cls} />;
  return collapsed ? <Folder aria-hidden="true" className={cls} /> : <FolderOpen aria-hidden="true" className={cls} />;
}

/**
 * How many sessions in this group are waiting, as a pill. It shows whether the
 * group is open or shut — shut is when it matters, since the cards that would
 * say so are not drawn — and it plays its beat once when the number grows.
 */
function WaitingBadge({ count }: { count: number }) {
  const pulse = useRisingEdge(count);
  if (count <= 0) return null;
  return (
    <span
      data-testid="agent-group-waiting"
      className={cn(
        "inline-flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-status-permission-soft px-1 text-caption-11-medium text-status-warning ring-1 ring-status-warning/40",
        pulse && "agent-attention-pulse",
      )}
    >
      <span aria-hidden="true" className="tabular-nums">
        {count}
      </span>
      <span className="sr-only">{count === 1 ? "1 session needs your response" : `${count} sessions need your response`}</span>
    </span>
  );
}

/**
 * The row that folds a group's older sessions: layered-cards glyph, how many,
 * and a plain label. Closed, two thin layers peek out under it to say "more
 * behind this"; open, they go and the row becomes the way back.
 */
function StackRow({ count, open, onToggle, controlsId }: { count: number; open: boolean; onToggle: () => void; controlsId: string }) {
  const label = open ? "Hide older sessions" : count === 1 ? "1 older session" : `${count} older sessions`;
  return (
    <div className="relative mt-0.5 mb-1.5">
      <button
        type="button"
        data-sidebar-nav=""
        data-testid="agent-stack-toggle"
        aria-expanded={open}
        aria-controls={controlsId}
        onClick={onToggle}
        className="relative z-10 flex h-8 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-3 text-left text-body-13 text-text-tertiary outline-none hover:bg-state-soft hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-status-info"
      >
        <GalleryVerticalEnd aria-hidden="true" className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
            open ? "-rotate-90" : "rotate-90",
          )}
        />
      </button>
      {open ? null : (
        <>
          <span aria-hidden="true" className="absolute inset-x-2 top-full h-1 rounded-b-md bg-surface-raised" />
          <span aria-hidden="true" className="absolute inset-x-4 top-full mt-1 h-1 rounded-b-md bg-surface-raised/50" />
        </>
      )}
    </div>
  );
}

/**
 * One workspace / folder: a header that folds it, then its cards — the five
 * most recent plus the ones that must not hide — and the stack of the rest.
 */
function AgentSessionGroupView({
  group,
  collapsed,
  olderOpen,
  activeId,
  pinnedIds,
  attention,
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
        waiting: (s) => attentionKind(s, attention[s.id]) !== null,
      }),
    [group.sessions, activeId, olderOpen, pinnedIds, attention],
  );

  // A card that still has to move lists (a pin, a session that starts waiting)
  // is remounted by React. If it held focus, give it back.
  const sectionRef = React.useRef<HTMLElement>(null);
  const focusedCard = React.useRef<string | null>(null);
  const placed = React.useRef(new Map<string, "shown" | "stacked">());
  React.useLayoutEffect(() => {
    const next = new Map<string, "shown" | "stacked">();
    for (const s of shown) next.set(s.id, "shown");
    for (const s of stacked) next.set(s.id, "stacked");
    const id = focusedCard.current;
    const lost = typeof document !== "undefined" && (!document.activeElement || document.activeElement === document.body);
    if (id && lost && placed.current.has(id) && placed.current.get(id) !== next.get(id)) {
      const button = Array.from(sectionRef.current?.querySelectorAll<HTMLElement>("[data-session-id]") ?? [])
        .find((el) => el.dataset.sessionId === id)
        ?.querySelector<HTMLElement>('[data-testid="agent-card-open"]');
      button?.focus();
    }
    placed.current = next;
  });
  const waiting = React.useMemo(() => waitingCount(group.sessions, attention), [group.sessions, attention]);
  const titles = React.useMemo(() => new Map(group.sessions.map((s) => [s.id, sessionTitle(s)])), [group.sessions]);

  // Cards that appear in an open group after it was already drawn — a session
  // pulled out of the stack, a new one — fade in. Cards drawn because the group
  // or the stack just opened do not: those already have the collapse's motion.
  const previous = React.useRef<{ ids: Set<string>; open: boolean } | null>(null);
  const entering = new Set<string>();
  if (previous.current?.open && !collapsed) {
    for (const s of shown) if (!previous.current.ids.has(s.id)) entering.add(s.id);
  }
  React.useEffect(() => {
    previous.current = { ids: new Set(shown.map((s) => s.id)), open: !collapsed };
  });

  const containsActive = Boolean(activeId) && group.sessions.some((s) => s.id === activeId);
  const newLabel = group.kind === "scratch" ? "New session without a project" : `New session in ${group.label}`;
  const hint = group.kind === "scratch" ? undefined : group.workspace ? group.workspace.paths.join("\n") : group.path || undefined;

  const renderCard = (session: (typeof shown)[number], enter = false) => (
    <AgentSessionCard
      key={session.id}
      session={session}
      selected={session.id === activeId}
      attention={attention[session.id]}
      ask={null}
      folder={null}
      pinned={pinnedIds.includes(session.id)}
      subagents={subagents[session.id] ?? NO_SUBAGENTS}
      parentTitle={session.parentSessionId ? titles.get(session.parentSessionId) : undefined}
      now={now}
      enter={enter}
    />
  );

  return (
    <section
      ref={sectionRef}
      onFocus={(event) => {
        focusedCard.current = (event.target as HTMLElement).closest<HTMLElement>("[data-session-id]")?.dataset.sessionId ?? null;
      }}
      data-testid="agent-session-group"
      data-group-key={group.key}
      data-collapsed={collapsed || undefined}
      aria-label={group.label}
      className="flex flex-col"
    >
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div className="group/header flex items-center gap-1 px-1.5 pt-2">
            <button
              type="button"
              data-sidebar-nav=""
              data-testid="agent-group-toggle"
              data-contains-active={containsActive || undefined}
              aria-expanded={!collapsed}
              aria-controls={bodyId}
              title={hint}
              onClick={() => onToggleCollapsed(group.key)}
              className="flex h-7 min-w-0 flex-1 cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-left outline-none hover:bg-state-soft focus-visible:ring-2 focus-visible:ring-status-info"
            >
              <ChevronRight
                aria-hidden="true"
                className={cn(
                  "size-3 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
                  !collapsed && "rotate-90",
                )}
              />
              <GroupIcon group={group} collapsed={collapsed} />
              <span
                data-testid="agent-group-name"
                className={cn(
                  "min-w-0 truncate text-label-12",
                  containsActive ? "text-text-primary" : "text-text-secondary",
                )}
              >
                {group.label}
              </span>
              {/* Light: the count is context, the badge beside it is the signal. */}
              <span data-testid="agent-group-count" className="shrink-0 text-caption-11 text-text-tertiary/70 tabular-nums">
                {group.sessions.length}
              </span>
              <WaitingBadge count={waiting} />
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
          <div role="list" aria-label={`${group.label} sessions`} className="flex flex-col gap-0.5">
            {shown.map((session) => renderCard(session, entering.has(session.id)))}
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
                <div id={stackId} role="list" aria-label={`${group.label} older sessions`} className="flex flex-col gap-0.5 pb-1">
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
