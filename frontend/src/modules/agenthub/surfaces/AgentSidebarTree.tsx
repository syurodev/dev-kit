import React from "react";
import { Plus, Users } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentSession } from "@/bridge/agenthub";
import { AgentRow } from "./AgentRow";

export type AgentSidebarTreeProps = {
  sessions: AgentSession[];
  activeSessionId?: string;
  pendingCounts?: Record<string, number>;
  onSelectSession: (sessionId: string) => void;
  onNewSession: () => void;
  className?: string;
};

/**
 * Agent Hub / Sidebar Content (Figma 264×836)
 * Swarm tree view rendering hierarchy of parent->child agent sessions.
 */
export function AgentSidebarTree({
  sessions,
  activeSessionId,
  pendingCounts = {},
  onSelectSession,
  onNewSession,
  className,
}: AgentSidebarTreeProps) {
  // Sort sessions preserving parent->child ordering where possible
  const orderedSessions = React.useMemo(() => {
    const rootSessions = sessions.filter((s) => !s.parentSessionId);
    const childMap = new Map<string, AgentSession[]>();
    for (const s of sessions) {
      if (s.parentSessionId) {
        const list = childMap.get(s.parentSessionId) ?? [];
        list.push(s);
        childMap.set(s.parentSessionId, list);
      }
    }

    const result: AgentSession[] = [];
    function appendTree(node: AgentSession) {
      result.push(node);
      const children = childMap.get(node.id) ?? [];
      for (const child of children) {
        appendTree(child);
      }
    }

    for (const root of rootSessions) {
      appendTree(root);
    }

    // Add any orphan sessions that weren't captured
    for (const s of sessions) {
      if (!result.includes(s)) {
        result.push(s);
      }
    }

    return result;
  }, [sessions]);

  return (
    <aside
      role="navigation"
      aria-label="Agent swarm sessions"
      data-testid="agent-sidebar-tree"
      className={cn(
        "flex w-64 flex-col border-r border-border-subtle bg-surface-base select-none",
        className,
      )}
    >
      {/* Sidebar Header */}
      <div className="flex h-10 shrink-0 items-center justify-between border-b border-border-subtle px-3">
        <div className="flex items-center gap-1.5">
          <Users className="size-3.5 text-text-tertiary" aria-hidden="true" />
          <span className="text-label-11-caps text-text-tertiary">
            {`Swarm (${sessions.length})`}
          </span>
        </div>

        <button
          type="button"
          data-testid="sidebar-new-session-button"
          onClick={onNewSession}
          aria-label="New Session"
          className="inline-flex h-6 items-center gap-1 rounded-control border border-border-subtle bg-surface-raised px-2 text-caption-11 text-text-primary transition-colors hover:bg-surface-tinted cursor-pointer"
        >
          <Plus className="size-3 shrink-0" aria-hidden="true" />
          <span>New</span>
        </button>
      </div>

      {/* Sessions List */}
      <div className="flex-1 overflow-y-auto">
        {orderedSessions.length === 0 ? (
          <div className="flex h-40 flex-col items-center justify-center gap-1.5 p-4 text-center">
            <p className="text-body-13 text-text-secondary">No agents in swarm</p>
            <p className="text-caption-11 text-text-tertiary">
              Launch a session to begin running agent turns.
            </p>
          </div>
        ) : (
          <div className="flex flex-col">
            {orderedSessions.map((session) => (
              <AgentRow
                key={session.id}
                session={session}
                selected={session.id === activeSessionId}
                pendingCount={pendingCounts[session.id] ?? 0}
                onClick={() => onSelectSession(session.id)}
              />
            ))}
          </div>
        )}
      </div>
    </aside>
  );
}
