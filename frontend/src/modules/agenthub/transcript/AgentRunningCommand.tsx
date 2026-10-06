import * as React from "react";
import { Terminal } from "lucide-react";

import { cn } from "@/lib/utils";
import { Shimmer } from "@/components/ui/shimmer";
import { FramedCard, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import type { ToolCall, TranscriptItem } from "../types";
import { commandDescription, toolCommand } from "../turns";
import { toolActivityState } from "./AgentToolCall";

/** A command shows its card once it has run this long: a quick one never flashes. */
export const SLOW_COMMAND_MS = 2000;

/** A shell command the agent is running right now. */
export function isRunningCommand(item: TranscriptItem): boolean {
  const call = item.toolCall;
  return Boolean(call && call.kind?.toLowerCase() === "execute" && toolActivityState(call.status) === "running");
}

/** What a command card names: the command itself, else what the vendor titled it. */
export function commandText(call: ToolCall): string {
  return toolCommand(call) || call.title || "command";
}

/** "8s", "2m 05s", "1h 03m". */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  if (m > 0) return `${m}m ${String(s).padStart(2, "0")}s`;
  return `${s}s`;
}

// One clock for every running command on screen: a timer per card would be a
// timer per card. Started by the first subscriber, stopped with the last.
const listeners = new Set<() => void>();
let ticker: ReturnType<typeof setInterval> | null = null;
function subscribeClock(listener: () => void): () => void {
  listeners.add(listener);
  if (!ticker) ticker = setInterval(() => listeners.forEach((fn) => fn()), 1000);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && ticker) {
      clearInterval(ticker);
      ticker = null;
    }
  };
}

/**
 * Milliseconds since `since` (an ISO time), ticking once a second while
 * `running`. Stopped, it reads once and stays: a finished command's time does
 * not move, and nothing is left ticking for it.
 */
export function useElapsed(since: string | undefined, running: boolean): number {
  const start = React.useMemo(() => (since ? Date.parse(since) : NaN), [since]);
  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    if (!running) return;
    return subscribeClock(() => setNow(Date.now()));
  }, [running]);
  return Number.isNaN(start) ? 0 : Math.max(0, now - start);
}

/**
 * A shell command that is taking a while, kept on screen under its run while
 * it runs (AgentTurn's StepGroup): the command and how long it has been going.
 * The output is not here — no vendor sends it before the command ends — so
 * the clock is the one live thing there is to show, and the reason to look is
 * to decide whether to wait or stop the turn. Gone once the command finishes;
 * its row in the fold keeps the output.
 */
export function AgentRunningCommand({ item }: { item: TranscriptItem }) {
  const call = item.toolCall!;
  const running = isRunningCommand(item);
  const elapsed = useElapsed(item.createdAt, running);
  if (!running || elapsed < SLOW_COMMAND_MS) return null;
  const command = commandText(call);
  const description = commandDescription(call);
  return (
    <div data-testid="running-command" className={cn("w-full min-w-0 activity-enter")}>
      <FramedCard>
        <FramedCardHeader
          icon={Terminal}
          title={<Shimmer active className="text-label-12">{description || "Running command…"}</Shimmer>}
          meta={<span data-testid="running-command-elapsed">{formatElapsed(elapsed)}</span>}
        />
        <FramedCardPreview>
          <code className="min-w-0 truncate text-code-12 text-text-primary" title={command}>
            {command}
          </code>
        </FramedCardPreview>
      </FramedCard>
    </div>
  );
}
