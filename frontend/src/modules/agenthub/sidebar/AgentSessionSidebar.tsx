import * as React from "react";
import { Eye, EyeOff, MessagesSquare, Plus } from "lucide-react";

import type { AgentSession } from "@/bridge/agenthub";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarInset, SidebarSection } from "@/components/ui/sidebar-shell";
import { cn } from "@/lib/utils";
import { heldChoice } from "../surfaces/AgentComposer";
import type { Subagent } from "../types";
import { SessionActionsContext, type SessionActions } from "./actions";
import { AgentSessionGroup } from "./AgentSessionGroup";
import { countDescendants, type SidebarWorkspace } from "./grouping";
import { useMinuteClock, useNarrowRail } from "./hooks";
import {
  attentionKind,
  groupSessions,
  nextWaiting,
  drawnWhenHidingClosed,
  type Attention,
  type SidebarGroup,
} from "./model";

// Stable defaults: a fresh [] / {} per render would defeat every memo below.
const NO_WORKSPACES: SidebarWorkspace[] = [];
const NO_STRINGS: string[] = [];
const NO_ATTENTION: Record<string, Attention> = {};
const NO_SUBAGENTS: Record<string, Subagent[]> = {};

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
  /** The agents a session started inside itself, by session id. */
  subagents?: Record<string, Subagent[]>;
  pinnedIds?: string[];
  onTogglePin?: (sessionId: string) => void;
  /** Group keys the person folded shut. Absent means open — a new project shows. */
  collapsedKeys?: string[];
  onToggleCollapsed?: (key: string) => void;
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
  subagents = NO_SUBAGENTS,
  pinnedIds = NO_STRINGS,
  collapsedKeys = NO_STRINGS,
  olderOpenKeys = NO_STRINGS,
  hideClosed = false,
  onToggleHideClosed,
  onToggleCollapsed,
  onToggleOlder,
  onSelectSession,
  onNewSession,
  className,
  ...handlers
}: AgentSessionSidebarProps) {
  const props = { onSelectSession, onNewSession, onToggleCollapsed, onToggleOlder, ...handlers };

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
    [held, has.newChild, has.pin, has.rename, has.transcript, has.copy, has.cancel, has.close, has.del],
  );

  // Membership is decided on every session, then closed ones are left out of
  // what is drawn (see groupSessions' `keep`).
  const groups = React.useMemo(
    () => groupSessions(allSessions, scratchRoot, workspaces, drawnWhenHidingClosed(hideClosed, activeSessionId)),
    [allSessions, scratchRoot, workspaces, hideClosed, activeSessionId],
  );
  const hiddenCount = allSessions.length - groups.reduce((n, g) => n + g.sessions.length, 0);
  const collapsed = React.useMemo(() => new Set(collapsedKeys), [collapsedKeys]);
  const olderOpen = React.useMemo(() => new Set(olderOpenKeys), [olderOpenKeys]);

  const rootRef = React.useRef<HTMLDivElement>(null);
  const viewportRef = React.useRef<HTMLDivElement>(null);
  const narrow = useNarrowRail(viewportRef);
  const now = useMinuteClock();

  // Over every session, not the drawn ones: a hidden agent can still be
  // waiting, and the footer is how you would find out.
  const waitingIds = React.useMemo(
    () => allSessions.filter((s) => attentionKind(s, attention[s.id]) !== null).map((s) => s.id),
    [allSessions, attention],
  );

  const newSessionIn = React.useCallback(
    (group: SidebarGroup) => latest.current.onNewSessionIn?.(group.path, group.workspace?.id, group.workspace?.paths),
    [],
  );
  const toggleCollapsed = React.useCallback((key: string) => latest.current.onToggleCollapsed?.(key), []);
  const toggleOlder = React.useCallback((key: string) => latest.current.onToggleOlder?.(key), []);

  function goToNextWaiting() {
    const next = nextWaiting(waitingIds, activeSessionId);
    if (!next) return;
    // Open its group first: a session the person was just sent to must not be
    // behind a fold.
    const group = groups.find((g) => g.sessions.some((s) => s.id === next));
    if (group && collapsed.has(group.key)) onToggleCollapsed?.(group.key);
    onSelectSession(next);
  }

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

  const waiting = waitingIds.length;

  return (
    <SessionActionsContext.Provider value={actions}>
      <div ref={rootRef} data-testid="agent-session-sidebar" className={cn("flex h-full min-h-0 w-full flex-col", className)}>
        {/* The shared rail header, outside the scroller so it stays put. */}
        <SidebarInset className="shrink-0">
          <SidebarSection
            first
            label="Sessions"
            actions={
              <>
                {onToggleHideClosed ? (
                  <ActionTooltip
                    label={
                      hideClosed
                        ? `Show closed sessions${hiddenCount ? ` (${hiddenCount} hidden)` : ""}`
                        : "Hide closed sessions"
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
                <ActionTooltip label="New session">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    data-testid="agent-tree-new"
                    aria-label="New session"
                    onClick={onNewSession}
                  >
                    <Plus aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              </>
            }
          />
        </SidebarInset>

        {/* The one scroll viewport. Groups and cards sit inside it with their own
            insets, so the scrollbar touches the rail's edge. */}
        <div
          ref={viewportRef}
          data-testid="agent-session-viewport"
          onKeyDown={onKeyDown}
          className="min-h-0 flex-1 overflow-y-auto pb-2 scrollbar-gutter-stable"
        >
          {groups.length === 0 ? (
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
                collapsed={collapsed.has(group.key)}
                olderOpen={olderOpen.has(group.key)}
                activeId={activeSessionId}
                pinnedIds={pinnedIds}
                attention={attention}
                subagents={subagents}
                now={now}
                narrow={narrow}
                onToggleCollapsed={toggleCollapsed}
                onToggleOlder={toggleOlder}
                onNewSession={handlers.onNewSessionIn ? newSessionIn : undefined}
              />
            ))
          )}
        </div>

        {waiting > 0 ? (
          // A button, not a caption: the count says something needs you, and
          // this is the shortest way to it. Each press walks to the next one.
          <button
            type="button"
            data-testid="agent-tree-attention"
            onClick={goToNextWaiting}
            className="flex w-full shrink-0 cursor-pointer items-center gap-2 bg-(--state-soft) px-3 py-3 text-left outline-none hover:bg-surface-raised focus-visible:ring-2 focus-visible:ring-status-info"
          >
            <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-status-warning" />
            <span className="min-w-0 flex-1 text-caption-11-medium text-text-primary">
              {`${waiting} agent${waiting === 1 ? "" : "s"} waiting on you`}
            </span>
            <span className="shrink-0 text-caption-11 text-text-tertiary">{waiting === 1 ? "Go" : "Next"}</span>
          </button>
        ) : null}
      </div>
    </SessionActionsContext.Provider>
  );
}
