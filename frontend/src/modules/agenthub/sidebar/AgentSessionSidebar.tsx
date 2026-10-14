import * as React from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Eye, EyeOff, MessagesSquare, Plus } from "lucide-react";

import type { AgentSession } from "@/bridge/agenthub";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarInset } from "@/components/ui/sidebar-shell";
import { EASE_OUT_CSS, MOTION_BASE } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { sessionTitle } from "../documents";
import { heldChoice } from "../surfaces/AgentComposer";
import type { Subagent } from "../types";
import { SessionActionsContext, type SessionActions } from "./actions";
import { AgentSessionCard } from "./AgentSessionCard";
import { AgentSessionGroup } from "./AgentSessionGroup";
import { countDescendants, type SidebarWorkspace } from "./grouping";
import { useMinuteClock } from "./hooks";
import {
  groupCollapsed,
  groupSessions,
  drawnWhenHidingClosed,
  needsYou,
  rowFolder,
  sessionCount,
  type Attention,
  type SidebarGroup,
} from "./model";

// Stable defaults: a fresh [] / {} per render would defeat every memo below.
const NO_WORKSPACES: SidebarWorkspace[] = [];
const NO_STRINGS: string[] = [];
const NO_ATTENTION: Record<string, Attention> = {};
const NO_SUBAGENTS: Record<string, Subagent[]> = {};
const NO_ASKS: Record<string, string> = {};
const NO_SUBAGENT_LIST: readonly Subagent[] = [];

export type AgentSessionSidebarProps = {
  sessions: AgentSession[];
  /** Workspaces to group by, the current one first. Sessions outside all of them are grouped by folder. */
  workspaces?: SidebarWorkspace[];
  /** Where "no project" sessions get their folders: every session under it is one group. */
  scratchRoot?: string;
  /** The session the focused group shows. */
  activeSessionId?: string;
  /** What each session is waiting on the person for (permissions and questions both). */
  attention?: Record<string, Attention>;
  /** What each waiting session asks, in its own words (askBySession). */
  asks?: Record<string, string>;
  /**
   * The workspace chosen in the title bar. Its group opens by default; every
   * other group folds by default. Absent: every group follows `collapsedKeys`.
   */
  currentWorkspaceId?: string | null;
  /** The agents a session started inside itself, by session id. */
  subagents?: Record<string, Subagent[]>;
  pinnedIds?: string[];
  onTogglePin?: (sessionId: string) => void;
  /** Group keys the person folded shut. Absent means open — a new project shows. */
  collapsedKeys?: string[];
  onToggleCollapsed?: (key: string) => void;
  /** Groups outside the current workspace that the person opened. Absent means folded. */
  expandedKeys?: string[];
  onToggleExpanded?: (key: string) => void;
  /** Group keys whose stack of older sessions is open. Absent means folded. */
  olderOpenKeys?: string[];
  onToggleOlder?: (key: string) => void;
  hideClosed?: boolean;
  onToggleHideClosed?: () => void;
  onSelectSession: (sessionId: string) => void;
  /** A subagent's work, beside its session. */
  onSelectSubagent?: (session: AgentSession, subagent: Subagent) => void;
  onNewSession: () => void;
  /** Starts a session in a group's folder; a workspace also gets its folders, primary first. */
  onNewSessionIn?: (projectPath: string, workspaceId?: string, workspacePaths?: string[]) => void;
  onNewChildSession?: (parent: AgentSession) => void;
  onCancelTurn?: (session: AgentSession) => void;
  onCloseSession?: (session: AgentSession) => void;
  onEnvironment?: (session: AgentSession) => void;
  onRenameSession?: (session: AgentSession) => void;
  onCopyTranscript?: (session: AgentSession) => void;
  onDeleteSession?: (session: AgentSession, descendants: number) => void;
  onCopy?: (text: string) => void;
  className?: string;
};

/**
 * Agent Hub's rail: collapsible groups, and in each a short list of two-line
 * session cards.
 *
 * The shell owns the column — width, resize, toggle, clipping — and this is the
 * content of it (ModuleManifest.sidebar): no aside, no background, border or
 * shadow of its own. The header stays put; the groups scroll in one viewport.
 *
 * Presentational. AgentHubSidebar reads the stores and owns the dialogs; this
 * takes plain data and callbacks, so it renders and tests without either.
 */
export function AgentSessionSidebar({
  sessions: allSessions,
  workspaces = NO_WORKSPACES,
  scratchRoot = "",
  activeSessionId,
  attention = NO_ATTENTION,
  asks = NO_ASKS,
  currentWorkspaceId = null,
  subagents = NO_SUBAGENTS,
  pinnedIds = NO_STRINGS,
  collapsedKeys = NO_STRINGS,
  expandedKeys = NO_STRINGS,
  olderOpenKeys = NO_STRINGS,
  hideClosed = false,
  onToggleHideClosed,
  onToggleCollapsed,
  onToggleExpanded,
  onToggleOlder,
  onSelectSession,
  onNewSession,
  className,
  ...handlers
}: AgentSessionSidebarProps) {
  const props = {
    onSelectSession,
    onNewSession,
    onToggleCollapsed,
    onToggleExpanded,
    onToggleOlder,
    currentWorkspaceId,
    ...handlers,
  };

  // The latest handlers, read through a stable object: a card is memoised on
  // its own data, so the owner rerendering with new closures must not rerender
  // every card.
  const descendants = React.useMemo(() => countDescendants(allSessions), [allSessions]);
  const latest = React.useRef(props);
  const descendantsRef = React.useRef(descendants);
  React.useLayoutEffect(() => {
    latest.current = props;
    descendantsRef.current = descendants;
  });

  const [held] = React.useState(heldChoice);
  const has = {
    newChild: Boolean(handlers.onNewChildSession),
    pin: Boolean(handlers.onTogglePin),
    environment: Boolean(handlers.onEnvironment),
    rename: Boolean(handlers.onRenameSession),
    transcript: Boolean(handlers.onCopyTranscript),
    copy: Boolean(handlers.onCopy),
    cancel: Boolean(handlers.onCancelTurn),
    close: Boolean(handlers.onCloseSession),
    del: Boolean(handlers.onDeleteSession),
  };
  const actions = React.useMemo<SessionActions>(
    () => ({
      open: (session) => latest.current.onSelectSession(session.id),
      // Without a handler a subagent opens the session that owns it.
      openSubagent: (session, subagent) =>
        latest.current.onSelectSubagent
          ? latest.current.onSelectSubagent(session, subagent)
          : latest.current.onSelectSession(session.id),
      newChild: has.newChild ? (parent) => latest.current.onNewChildSession?.(parent) : undefined,
      togglePin: has.pin ? (id) => latest.current.onTogglePin?.(id) : undefined,
      environment: has.environment ? (session) => latest.current.onEnvironment?.(session) : undefined,
      rename: has.rename ? (session) => latest.current.onRenameSession?.(session) : undefined,
      copyTranscript: has.transcript ? (session) => latest.current.onCopyTranscript?.(session) : undefined,
      copy: has.copy ? (text) => latest.current.onCopy?.(text) : undefined,
      cancelTurn: has.cancel ? (session) => latest.current.onCancelTurn?.(session) : undefined,
      closeSession: has.close ? (session) => latest.current.onCloseSession?.(session) : undefined,
      deleteSession: has.del ? (session, n) => latest.current.onDeleteSession?.(session, n) : undefined,
      descendantCount: (id) => descendantsRef.current[id] ?? 0,
      hold: held.hold,
      release: held.release,
    }),
    [held, has.environment, has.newChild, has.pin, has.rename, has.transcript, has.copy, has.cancel, has.close, has.del],
  );

  // What waits on the person, from every workspace, is drawn once, on top.
  const waiting = React.useMemo(() => needsYou(allSessions, attention), [allSessions, attention]);
  // Keyed on who is in it, not on the list: a queue change that leaves the same
  // sessions waiting must not regroup (and rerender) every group.
  const waitingKey = waiting.map((x) => x.id).join("\u0000");
  const waitingIds = React.useMemo(() => new Set(waitingKey ? waitingKey.split("\u0000") : []), [waitingKey]);

  // Membership is decided on every session, then the ones drawn in Needs you
  // and (when hidden) closed ones are left out of what is drawn — see
  // groupSessions' `keep`. Filtering first would move a waiting parent's
  // children into whatever group their own folder says.
  // groupSessions orders by name; the workspace chosen in the title bar goes
  // first (a stable sort keeps the rest as they were).
  const groups = React.useMemo(() => {
    const closed = drawnWhenHidingClosed(hideClosed, activeSessionId);
    const named = groupSessions(allSessions, scratchRoot, workspaces, (s) => !waitingIds.has(s.id) && (closed?.(s) ?? true));
    if (!currentWorkspaceId) return named;
    const first = `workspace:${currentWorkspaceId}`;
    return [...named].sort((a, b) => Number(b.key === first) - Number(a.key === first));
  }, [allSessions, scratchRoot, workspaces, hideClosed, activeSessionId, waitingIds, currentWorkspaceId]);
  const hiddenCount = allSessions.length - waiting.length - groups.reduce((n, g) => n + g.sessions.length, 0);
  const titles = React.useMemo(() => new Map(allSessions.map((s) => [s.id, sessionTitle(s)])), [allSessions]);
  const byId = React.useMemo(() => new Map(allSessions.map((s) => [s.id, s])), [allSessions]);
  const needsLabelId = React.useId();
  const collapsed = React.useMemo(() => new Set(collapsedKeys), [collapsedKeys]);
  const expanded = React.useMemo(() => new Set(expandedKeys), [expandedKeys]);
  // While the section folds away its last rows stay drawn, so it shrinks from
  // its own height instead of jumping to an empty label first.
  const lastWaiting = React.useRef(waiting);
  if (waiting.length > 0) lastWaiting.current = waiting;
  const drawnWaiting = waiting.length > 0 ? waiting : lastWaiting.current;
  const [needsRef] = useAutoAnimate<HTMLDivElement>({ duration: MOTION_BASE * 1000, easing: EASE_OUT_CSS });
  const olderOpen = React.useMemo(() => new Set(olderOpenKeys), [olderOpenKeys]);

  const viewportRef = React.useRef<HTMLDivElement>(null);

  // A card that moves lists — into Needs you, back into its group, out of a
  // stack when pinned — is remounted by React, and keyboard focus goes with
  // the old element. If the card the person had focused moved and focus fell
  // to the page, give it back to the card where it now is. Watched here, over
  // every list, because a move into Needs you leaves the group altogether.
  const focusedCard = React.useRef<{ id: string; list: Element | null } | null>(null);
  React.useLayoutEffect(() => {
    const focused = focusedCard.current;
    if (!focused || typeof document === "undefined") return;
    const lost = !document.activeElement || document.activeElement === document.body;
    if (!lost) return;
    const moved = Array.from(viewportRef.current?.querySelectorAll<HTMLElement>("[data-session-id]") ?? []).find(
      (el) => el.dataset.sessionId === focused.id && el.closest('[role="list"]') !== focused.list,
    );
    if (!moved) return;
    focused.list = moved.closest('[role="list"]');
    moved.querySelector<HTMLElement>('[data-testid="agent-card-open"]')?.focus();
  });  const now = useMinuteClock();

  const newSessionIn = React.useCallback(
    (group: SidebarGroup) => latest.current.onNewSessionIn?.(group.path, group.workspace?.id, group.workspace?.paths),
    [],
  );
  // The same branch groupCollapsed reads: the current workspace (or every group,
  // with none chosen) remembers what was folded; the others, what was opened.
  const toggleCollapsed = React.useCallback((key: string) => {
    const { currentWorkspaceId: current, onToggleCollapsed: fold, onToggleExpanded: open } = latest.current;
    if (!current || key === `workspace:${current}`) fold?.(key);
    else open?.(key);
  }, []);
  const isCurrent = (group: SidebarGroup) => !currentWorkspaceId || group.key === `workspace:${currentWorkspaceId}`;
  // The session on screen is never folded away inside another workspace: its
  // group draws open while it holds it.
  const holdsActive = (group: SidebarGroup) =>
    !isCurrent(group) && Boolean(activeSessionId) && group.sessions.some((s) => s.id === activeSessionId);
  const toggleOlder = React.useCallback((key: string) => latest.current.onToggleOlder?.(key), []);

  // Up/Down/Home/End walk the rail's own stops (group headers, cards, stack
  // rows) without tabbing through every avatar and menu in between. Only when
  // the key came from one of those: Radix menus and popovers bubble their keys
  // through React's tree, and arrows inside them are theirs.
  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    const target = event.target as HTMLElement;
    if (!target.hasAttribute("data-sidebar-nav")) return;
    const stops = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("[data-sidebar-nav]"));
    const at = stops.indexOf(target);
    if (at === -1) return;
    const to =
      event.key === "Home" ? 0 : event.key === "End" ? stops.length - 1 : at + (event.key === "ArrowDown" ? 1 : -1);
    const next = stops[Math.max(0, Math.min(stops.length - 1, to))];
    if (next && next !== target) {
      event.preventDefault();
      next.focus();
    }
  }

  return (
    <SessionActionsContext.Provider value={actions}>
      <div data-testid="agent-session-sidebar" className={cn("flex h-full min-h-0 w-full flex-col", className)}>
        {/* The rail's header, outside the scroller so it stays put. */}
        <SidebarInset className="shrink-0">
          <div className="flex items-center gap-2 px-2 pt-1.5 pb-3">
            <h2 className="min-w-0 flex-1 truncate text-title-14 text-text-primary">Agents</h2>
            {onToggleHideClosed ? (
              <ActionTooltip
                label={
                  hideClosed ? `Show closed sessions${hiddenCount ? ` (${hiddenCount} hidden)` : ""}` : "Hide closed sessions"
                }
              >
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  data-testid="agent-tree-toggle-closed"
                  aria-pressed={hideClosed}
                  onClick={onToggleHideClosed}
                  className="text-text-tertiary"
                >
                  {hideClosed ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
                </Button>
              </ActionTooltip>
            ) : null}
            <Button
              type="button"
              size="xs"
              data-testid="agent-tree-new"
              aria-label="New session"
              onClick={onNewSession}
              className="rounded-full"
            >
              <Plus aria-hidden="true" />
              New
            </Button>
          </div>
        </SidebarInset>

        {/* The one scroll viewport. Groups and cards sit inside it with their own
            insets, so the scrollbar touches the rail's edge. */}
        <div
          ref={viewportRef}
          data-testid="agent-session-viewport"
          onKeyDown={onKeyDown}
          onFocus={(event) => {
            const card = (event.target as HTMLElement).closest<HTMLElement>("[data-session-id]");
            const id = card?.dataset.sessionId;
            focusedCard.current = id ? { id, list: card.closest('[role="list"]') } : null;
          }}
          className="min-h-0 flex-1 overflow-y-auto pb-2"
        >
          {/* Eases in when the first session starts waiting and out when the
              last one is answered, instead of pushing the rail down a step. */}
          <AnimatedCollapse open={waiting.length > 0}>
            <SidebarInset>
              <section data-testid="agent-needs-you" aria-labelledby={needsLabelId} className="flex flex-col">
                {/* The same row as a group's header, without a chevron: it does not fold. */}
                <div id={needsLabelId} className="flex h-8 items-center gap-1.5 px-2">
                  <span className="min-w-0 flex-1 truncate text-body-13 font-medium text-text-primary">Needs you</span>
                  <span aria-hidden="true" className="shrink-0 text-caption-11 tabular-nums text-text-tertiary">
                    {drawnWaiting.length}
                  </span>
                  <span className="sr-only">{sessionCount(drawnWaiting.length)}</span>
                </div>
                <div ref={needsRef} role="list" aria-label="Sessions that need you" className="flex flex-col gap-0.5">
                  {drawnWaiting.map((session) => (
                    <AgentSessionCard
                      key={session.id}
                      session={session}
                      selected={session.id === activeSessionId}
                      attention={attention[session.id]}
                      ask={asks[session.id] ?? null}
                      folder={rowFolder(session, byId, scratchRoot, workspaces)}
                      pinned={pinnedIds.includes(session.id)}
                      subagents={subagents[session.id] ?? NO_SUBAGENT_LIST}
                      parentTitle={session.parentSessionId ? titles.get(session.parentSessionId) : undefined}
                      now={now}
                    />
                  ))}
                </div>
              </section>
            </SidebarInset>
          </AnimatedCollapse>
          {groups.length === 0 && waiting.length === 0 ? (
            <EmptyState
              size="rail"
              icon={MessagesSquare}
              title={hideClosed && allSessions.length > 0 ? "Only closed sessions" : "No sessions yet"}
              body={
                hideClosed && allSessions.length > 0
                  ? "Show closed sessions to see them again."
                  : "Start one, and it shows here with the folder it works in."
              }
              action={{ label: "New session", icon: Plus, onClick: onNewSession }}
            />
          ) : (
            groups.map((group) => (
              <AgentSessionGroup
                key={group.key}
                group={group}
                collapsed={groupCollapsed(group.key, currentWorkspaceId, collapsed, expanded) && !holdsActive(group)}
                current={isCurrent(group)}
                olderOpen={olderOpen.has(group.key)}
                activeId={activeSessionId}
                pinnedIds={pinnedIds}
                subagents={subagents}
                now={now}
                onToggleCollapsed={toggleCollapsed}
                onToggleOlder={toggleOlder}
                onNewSession={handlers.onNewSessionIn ? newSessionIn : undefined}
              />
            ))
          )}
        </div>
      </div>
    </SessionActionsContext.Provider>
  );
}
