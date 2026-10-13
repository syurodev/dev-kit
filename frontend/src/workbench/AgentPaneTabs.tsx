import * as React from "react";
import { ChevronDown, Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { AgentSession } from "@/bridge/agenthub";
import { cn } from "@/lib/utils";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import {
  addAgentSessionTabInBackground,
  agentPanelSessionId,
  agentSessionTabs,
  closeAgentSessionTabs,
  openNewAgentSession,
  sessionTitle,
  tabSessionId,
} from "@/modules/agenthub/documents";

/** Matches AgentState's dot colours; a tab has room for a dot, not a word. */
const DOT_CLASS: Record<string, string> = {
  running: "bg-(--status-running)",
  awaiting_permission: "bg-status-warning",
  error: "bg-status-danger",
  closed: "bg-text-tertiary/50",
};

function Dot({ status }: { status: string }) {
  return <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", DOT_CLASS[status] ?? "bg-text-tertiary")} />;
}

/** The Agent panel's sessions: Agent Hub's open session tabs, in its order. */
export function useAgentPaneSessionIds(): string[] {
  return useWorkbenchTabsStore(
    React.useCallback((state) => agentSessionTabs(state).map(tabSessionId).join("\n"), []),
  )
    .split("\n")
    .filter(Boolean);
}

/** See documents.ts agentPanelSessionId: one rule for the panel and for "on screen". */
export const agentPaneSessionId = agentPanelSessionId;

/**
 * The Agent panel's tabs, drawn in its own header (components/ui/pane-slots)
 * the way the Terminal pane draws its terminals: a pane header and a strip
 * stacked read as two title bars.
 *
 * They are Agent Hub's tabs, not a list of their own: the same sessions in the
 * same order, so opening, closing or reordering a session in either place shows
 * in both. Closing a tab here closes that tab there and never the session —
 * the agent keeps running and the session stays in the sidebar. Picking a tab
 * only changes what the panel shows; the main area stays where it is.
 */
export function AgentPaneTabs() {
  const tabIds = useAgentPaneSessionIds();
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);
  const byId = React.useMemo(() => new Map(sessions.map((s) => [s.id, s])), [sessions]);
  const value = agentPaneSessionId(tabIds, activeSessionId);
  const listRef = React.useRef<HTMLDivElement>(null);
  // Which ends have tabs scrolled out of sight: those ends fade, so a tab cut
  // at the edge reads as "more this way" rather than as a glitch.
  const [hidden, setHidden] = React.useState({ start: false, end: false });
  const measure = React.useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    const start = el.scrollLeft > 1;
    const end = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
    setHidden((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
  }, []);
  React.useEffect(() => {
    const el = listRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [measure]);
  React.useEffect(measure, [measure, tabIds.length, value]);
  const fade =
    hidden.start || hidden.end
      ? `linear-gradient(to right, ${hidden.start ? "transparent, black 20px" : "black"}, ${hidden.end ? "black calc(100% - 20px), transparent" : "black"})`
      : undefined;

  // Keep the selected tab in view when it changes from elsewhere (a
  // notification, the menu below, the tab on screen in Agent Hub).
  React.useEffect(() => {
    const tab = listRef.current?.querySelector<HTMLElement>('[role="tab"][data-state="active"]');
    tab?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [value, tabIds.length]);

  const notOpen = sessions.filter((s) => !tabIds.includes(s.id));
  const liveNotOpen = notOpen.filter((s) => s.status !== "closed");
  const closedNotOpen = notOpen.filter((s) => s.status === "closed");

  function openInPanel(session: AgentSession) {
    addAgentSessionTabInBackground(session);
    setActiveSessionId(session.id);
  }

  return (
    <div className="flex min-w-0 flex-1 items-center gap-1">
      {tabIds.length === 0 ? <span className="min-w-0 flex-1 truncate text-label-12">Agent</span> : null}
      <Tabs value={value} onValueChange={setActiveSessionId} className={cn("min-w-0 gap-0", tabIds.length > 0 && "flex-1")}>
        <TabsList
          ref={listRef}
          aria-label="Agent sessions"
          onScroll={measure}
          style={fade ? { maskImage: fade, WebkitMaskImage: fade } : undefined}
          // A vertical wheel scrolls the strip sideways, as the document strip does.
          onWheel={(event) => {
            const el = listRef.current;
            if (el && Math.abs(event.deltaY) > Math.abs(event.deltaX)) el.scrollLeft += event.deltaY;
          }}
          className="h-6 w-full justify-start gap-1 overflow-x-auto bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        >
          {tabIds.map((id) => {
            const session = byId.get(id);
            const title = session ? sessionTitle(session) : `Session ${id.slice(0, 8)}`;
            return (
              <TabsTrigger
                key={id}
                value={id}
                title={title}
                data-testid="agent-pane-tab"
                // Tabs share the strip, shrinking down to min-w-16 before it
                // scrolls — a 320px panel would otherwise show one tab and a
                // sliver. The trigger's own inner wrapper must shrink too.
                className={cn(
                  "group/agent-tab h-6 max-w-40 shrink justify-start gap-1 rounded-sm px-2 pr-1 text-label-12 [&>span:last-child]:w-full [&>span:last-child]:min-w-0",
                  // The one on screen keeps enough of its title to read.
                  id === value ? "min-w-24" : "min-w-16",
                  "data-[state=active]:bg-surface-raised data-[state=active]:text-text-primary data-[state=inactive]:bg-state-soft data-[state=inactive]:text-text-secondary",
                )}
              >
                <Dot status={session?.status ?? ""} />
                <span className="min-w-0 flex-1 truncate text-left">{title}</span>
                <span
                  role="button"
                  tabIndex={-1}
                  aria-label={`Close ${title}`}
                  title="Close tab (the session keeps running)"
                  onPointerDown={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    closeAgentSessionTabs([id]);
                  }}
                  className={cn(
                    "shrink-0 rounded p-0.5 opacity-60 hover:bg-foreground/10 hover:opacity-100",
                    // Close marks on every tab ate a third of the strip: only
                    // the shown tab keeps one, the others show it on hover.
                    id !== value && "hidden group-hover/agent-tab:inline-flex",
                  )}
                >
                  <X className="size-3" />
                </span>
              </TabsTrigger>
            );
          })}
        </TabsList>
      </Tabs>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon-xs" variant="titlebar" aria-label="Agent sessions" title="Agent sessions" data-testid="agent-pane-sessions">
            <ChevronDown className="size-3.25" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuItem icon={<Plus className="size-3.5" />} onSelect={() => openNewAgentSession()} detail="Agent Hub">
            New session
          </DropdownMenuItem>
          {tabIds.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              {/* Every tab, readable in full: in a narrow panel most of the
                  strip is scrolled away or truncated. */}
              <DropdownMenuLabel>Open tabs</DropdownMenuLabel>
              {tabIds.map((id) => {
                const session = byId.get(id);
                return (
                  <DropdownMenuItem
                    key={id}
                    icon={<Dot status={session?.status ?? ""} />}
                    detail={id === value ? "current" : undefined}
                    onSelect={() => setActiveSessionId(id)}
                  >
                    {session ? sessionTitle(session) : `Session ${id.slice(0, 8)}`}
                  </DropdownMenuItem>
                );
              })}
            </>
          ) : null}
          {liveNotOpen.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>Not open</DropdownMenuLabel>
              {liveNotOpen.map((session) => (
                <DropdownMenuItem
                  key={session.id}
                  icon={<Dot status={session.status} />}
                  detail={session.parentSessionId ? "subagent" : undefined}
                  onSelect={() => openInPanel(session)}
                >
                  {sessionTitle(session)}
                </DropdownMenuItem>
              ))}
            </>
          ) : null}
          {closedNotOpen.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>Closed</DropdownMenuLabel>
              {closedNotOpen.map((session) => (
                <DropdownMenuItem key={session.id} icon={<Dot status={session.status} />} onSelect={() => openInPanel(session)}>
                  {sessionTitle(session)}
                </DropdownMenuItem>
              ))}
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
