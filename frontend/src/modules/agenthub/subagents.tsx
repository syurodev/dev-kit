import * as React from "react";
import { create } from "zustand";
import { Bot, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { isSessionQuiet } from "./sessionActivity";
import { runningSubagents, type LiveSubagent } from "./turns";
import type { Subagent, TranscriptItem } from "./types";
import { AgentState, type AgentStatus } from "./surfaces/AgentState";
import { SubagentActivity } from "./surfaces/AgentTurn";
import { EarlierTurnProvider } from "./transcript/toolSettle";

/** A subagent with no end state yet is still working (the spawn carries none). */
export function isSubagentRunning(state: string | undefined): boolean {
  return !state || state === "running";
}

function toStatus(state: string | undefined): AgentStatus {
  if (isSubagentRunning(state)) return "running";
  return state === "failed" ? "error" : "closed";
}

/**
 * The subagents of a session still working, each with what it has done so far.
 *
 * Whether one is running is the tracked state's to say (the store's
 * `subagents`, fed by every spawn and state update, killed ones dropped — what
 * the sidebar shows), not the turn's: a lead agent that sends subagents off in
 * the background and ends its turn to wait for them used to make them vanish
 * from the composer tray while the sidebar still showed them running. The
 * transcript adds what was spawned before this app started tracking — only
 * while a turn runs, since a subagent the transcript never saw end is
 * otherwise as likely stale as running.
 */
export function sessionRunningSubagents(
  items: readonly TranscriptItem[],
  tracked: readonly Subagent[] | undefined,
  live: boolean,
): LiveSubagent[] {
  // Nothing tracked and no turn running: the transcript alone is trusted for
  // nothing, so there is no reason to walk it.
  if (!live && !tracked?.length) return [];
  const { running, activity } = runningSubagents(items);
  const out = new Map<string, LiveSubagent>();
  const ended = new Set<string>();
  for (const subagent of tracked ?? []) {
    if (!isSubagentRunning(subagent.state)) {
      ended.add(subagent.id);
      continue;
    }
    const seen = running.find((r) => r.subagent.id === subagent.id)?.subagent;
    out.set(subagent.id, { subagent: { ...seen, ...subagent }, activity: activity[subagent.id] ?? [] });
  }
  if (live) {
    for (const r of running) if (!out.has(r.subagent.id) && !ended.has(r.subagent.id)) out.set(r.subagent.id, r);
  }
  return [...out.values()];
}

/** Which subagent Agent Hub's right-hand pane is showing, if any. */
export const useSubagentPanel = create<{
  open: { sessionId: string; subagentId: string } | null;
  show: (sessionId: string, subagentId: string) => void;
  close: () => void;
}>((set) => ({
  open: null,
  show: (sessionId, subagentId) => set({ open: { sessionId, subagentId } }),
  close: () => set({ open: null }),
}));

/** Every row a subagent appears in, folded into one: its name, task and latest state. */
function findSubagent(items: readonly TranscriptItem[], id: string): Subagent | null {
  let found: Subagent | null = null;
  for (const item of items) {
    const row = item.subagent;
    if (row && row.id === id) found = { ...(found ?? {}), ...row };
  }
  return found;
}

/**
 * One subagent's work, in Agent Hub's right-hand pane: the brief it was given,
 * its steps and what it said, its report — live while it runs. Opened from the
 * subagent's row in the sidebar, which used to only select the session it
 * belongs to and leave the person to find the subagent's row in a folded turn.
 *
 * Read from the owning session's transcript, so it is the pages loaded for
 * that session: a subagent from an older page shows once that page is loaded.
 */
export function SubagentPanel({ sessionId, subagentId, onClose }: { sessionId: string; subagentId: string; onClose: () => void }) {
  const items = useAgentSessionsStore((s) => s.transcriptsById[sessionId]?.items ?? EMPTY_ARRAY);
  const tracked = useAgentSessionsStore((s) => s.subagents[sessionId]?.find((a) => a.id === subagentId));
  const activityById = React.useMemo(() => runningSubagents(items).activity, [items]);
  const fromTranscript = React.useMemo(() => findSubagent(items, subagentId), [items, subagentId]);
  const subagent: Subagent = { id: subagentId, ...fromTranscript, ...tracked };
  const activity = activityById[subagentId] ?? EMPTY_ARRAY;
  const running = isSubagentRunning(subagent.state);
  // Outside a turn, so the pane says what the transcript's scope would: nothing
  // works on the session, so a call its subagent is not tracked as running for
  // cannot finish (transcript/toolSettle.tsx).
  const quiet = useAgentSessionsStore((s) => isSessionQuiet(s.sessionsById[sessionId], Boolean(s.turnInFlightBySession[sessionId])));

  return (
    <section data-testid="subagent-panel" aria-label="Subagent" className="flex h-full min-h-0 w-full min-w-0 flex-col">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
        <Bot className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-label-12 text-text-secondary" title={subagent.task || undefined}>
          {subagent.name || "Subagent"}
        </span>
        <AgentState status={toStatus(subagent.state)} />
        <Button variant="ghost" size="icon-xs" aria-label="Close" title="Close" onClick={onClose}>
          <X />
        </Button>
      </header>
      {activity.length === 0 ? (
        <EmptyState
          size="rail"
          className="flex-1"
          title={running ? "Starting…" : "Nothing to show yet"}
          body={running ? "Its steps show here as it takes them." : "Its work is in an older part of the conversation that has not loaded."}
        />
      ) : (
        <div data-testid="subagent-panel-activity" className="min-h-0 flex-1 overflow-y-auto p-2">
          <EarlierTurnProvider earlier={false} quiet={quiet} sessionId={sessionId}>
            <SubagentActivity subagent={subagent} activity={activity} subagents={activityById} live={running} flat />
          </EarlierTurnProvider>
        </div>
      )}
    </section>
  );
}
