import * as React from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession } from "@/bridge/agenthub";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/** Matches AgentState's dot colours; the strip has room for a dot, not a word. */
const DOT_CLASS: Record<string, string> = {
  running: "bg-status-info",
  awaiting_permission: "bg-status-warning",
  error: "bg-status-danger",
  closed: "bg-text-tertiary/50",
};

export type AgentSwarmStripProps = {
  sessions: AgentSession[];
  activeSessionId?: string;
  onSelectSession?: (id: string) => void;
  className?: string;
};

function sessionLabel(session: AgentSession): string {
  return session.title || `Session ${session.id.slice(0, 8)}`;
}

/**
 * Which sessions get a tab: every root session still open, and the selected
 * one whatever it is. A subagent is reached through its parent (or the menu),
 * and a closed session is history — a strip of every session that ever ran
 * fills a 320px rail with tabs nobody is going to read.
 */
export function railTabSessions(sessions: AgentSession[], activeSessionId?: string): AgentSession[] {
  return sessions.filter(
    (session) => session.id === activeSessionId || (!session.parentSessionId && session.status !== "closed"),
  );
}

/** The next session asking for a permission after the selected one, wrapping. */
function nextWaitingId(sessions: AgentSession[], activeSessionId?: string): string | undefined {
  const waiting = sessions.filter((s) => s.status === "awaiting_permission").map((s) => s.id);
  if (waiting.length === 0) return undefined;
  const at = activeSessionId ? waiting.indexOf(activeSessionId) : -1;
  return waiting[(at + 1) % waiting.length];
}

function Dot({ status }: { status: string }) {
  return (
    <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", DOT_CLASS[status] ?? "bg-text-tertiary")} />
  );
}

/**
 * The session tabs at the top of Agent / Rail — Figma `913:13`.
 *
 * The rail travels across modules, so it needs its own way to change agent
 * without the Agent Hub sidebar being on screen. One row: the open sessions as
 * tabs, scrolling sideways (edges fading) when they do not fit, then a jump to
 * the next waiting session when a
 * permission is pending, then a menu holding every session — subagents and
 * closed ones included — so nothing is out of reach. It used to show the first
 * two sessions of the whole list and a "+N" that did nothing: the selected
 * session was often not a tab at all, and the rest could not be reached.
 *
 * Tabs truncate at a fixed width rather than wrapping or pushing the row wider
 * than the rail; the full title is on hover and in the menu.
 */
export function AgentSwarmStrip({ sessions, activeSessionId, onSelectSession, className }: AgentSwarmStripProps) {
  const tabs = railTabSessions(sessions, activeSessionId);
  const open = sessions.filter((s) => s.status !== "closed");
  const closed = sessions.filter((s) => s.status === "closed");
  const waiting = sessions.filter((s) => s.status === "awaiting_permission").length;
  const listRef = React.useRef<HTMLDivElement>(null);
  // Which ends have tabs scrolled out of sight: those ends fade, so a tab cut
  // in half at the edge reads as "more this way" rather than as a glitch.
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
  React.useEffect(measure, [measure, tabs.length]);

  // Keep the selected tab on screen when it changes from outside the strip
  // (the Hub sidebar, the menu, the waiting jump).
  React.useEffect(() => {
    const tab = listRef.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    tab?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [activeSessionId, tabs.length]);

  // A mouse wheel only scrolls vertically; the row only scrolls sideways.
  function onWheel(event: React.WheelEvent<HTMLDivElement>) {
    const el = listRef.current;
    if (!el || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
    el.scrollLeft += event.deltaY;
  }

  const fade = hidden.start || hidden.end
    ? `linear-gradient(to right, ${hidden.start ? "transparent, black 24px" : "black"}, ${hidden.end ? "black calc(100% - 24px), transparent" : "black"})`
    : undefined;

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight" && event.key !== "Home" && event.key !== "End") return;
    const buttons = Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? []);
    if (buttons.length === 0) return;
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? buttons.length - 1
          : (Math.max(at, 0) + (event.key === "ArrowRight" ? 1 : -1) + buttons.length) % buttons.length;
    event.preventDefault();
    buttons[next]?.focus();
    buttons[next]?.click();
  }

  const nextWaiting = nextWaitingId(sessions, activeSessionId);

  return (
    <div
      data-testid="agent-swarm-strip"
      className={cn("flex w-full min-w-0 shrink-0 items-center gap-1 py-2", className)}
    >
      <div
        ref={listRef}
        role="tablist"
        aria-label="Agent sessions"
        onKeyDown={onKeyDown}
        onScroll={measure}
        onWheel={onWheel}
        style={fade ? { maskImage: fade, WebkitMaskImage: fade } : undefined}
        className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {tabs.map((session) => {
          const selected = session.id === activeSessionId;
          const label = sessionLabel(session);
          return (
            <button
              key={session.id}
              type="button"
              role="tab"
              aria-selected={selected}
              tabIndex={selected ? 0 : -1}
              title={label}
              data-testid="agent-swarm-chip"
              data-selected={selected}
              onClick={() => onSelectSession?.(session.id)}
              className={cn(
                "flex h-6 max-w-40 min-w-0 shrink-0 cursor-pointer items-center gap-1.5 rounded-full border px-2 outline-none",
                "focus-visible:ring-2 focus-visible:ring-ring/50",
                selected
                  ? "border-border-subtle bg-surface-raised text-caption-11-medium text-text-primary"
                  : "border-transparent text-caption-11 text-text-secondary hover:bg-(--state-soft) hover:text-text-primary",
              )}
            >
              <Dot status={session.status} />
              <span className="min-w-0 truncate">{label}</span>
            </button>
          );
        })}
      </div>

      {nextWaiting ? (
        <button
          type="button"
          data-testid="agent-swarm-waiting"
          aria-label={`${waiting} waiting for you — go to the next`}
          title={`${waiting} waiting for you — go to the next`}
          onClick={() => onSelectSession?.(nextWaiting)}
          className="flex h-6 shrink-0 cursor-pointer items-center gap-1 rounded-full px-1.5 text-caption-11-medium text-status-warning hover:bg-(--state-soft)"
        >
          <Dot status="awaiting_permission" />
          {waiting}
        </button>
      ) : null}

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            data-testid="agent-swarm-more"
            aria-label="All agent sessions"
            title="All agent sessions"
            className="flex h-6 shrink-0 cursor-pointer items-center gap-1 rounded-full px-1.5 text-caption-11 text-text-tertiary outline-none hover:bg-(--state-soft) hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            {sessions.length}
            <ChevronDown className="size-3.5" aria-hidden="true" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuLabel>{`Open · ${open.length}`}</DropdownMenuLabel>
          {open.map((session) => (
            <SessionMenuItem
              key={session.id}
              session={session}
              selected={session.id === activeSessionId}
              onSelect={onSelectSession}
            />
          ))}
          {closed.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>{`Closed · ${closed.length}`}</DropdownMenuLabel>
              {closed.map((session) => (
                <SessionMenuItem
                  key={session.id}
                  session={session}
                  selected={session.id === activeSessionId}
                  onSelect={onSelectSession}
                />
              ))}
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function SessionMenuItem({
  session,
  selected,
  onSelect,
}: {
  session: AgentSession;
  selected: boolean;
  onSelect?: (id: string) => void;
}) {
  return (
    <DropdownMenuItem
      data-testid="agent-swarm-menu-item"
      icon={<Dot status={session.status} />}
      detail={selected ? "current" : session.parentSessionId ? "subagent" : undefined}
      onSelect={() => onSelect?.(session.id)}
      title={sessionLabel(session)}
    >
      <span className={session.parentSessionId ? "pl-3" : undefined}>{sessionLabel(session)}</span>
    </DropdownMenuItem>
  );
}
