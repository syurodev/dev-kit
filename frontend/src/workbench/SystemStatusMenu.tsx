import { useCallback, useEffect, useState } from "react";
import { Activity, Bot, CloudCheck, Lock, LockOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { vaultStatus, type VaultState } from "../bridge/vault";
import { syncConflicts } from "../bridge/sync";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { requestVaultUnlock } from "./vaultUnlockRequest";

export type SystemStatus = {
  vault: VaultState | null;
  conflicts: number;
  agentsRunning: number;
};

export type Severity = "neutral" | "attention" | "active";

/**
 * The icon carries the worst thing currently true.
 *
 * A dropdown that hides everything behind a click trades clutter for
 * blindness: a locked vault or an unresolved conflict has to be visible
 * without opening anything. Only one state fits on one icon, so they are
 * ranked — a locked vault and a conflict both block work, an agent running
 * does not, which is why it reads as accent rather than warning.
 */
export function severityOf(status: SystemStatus): Severity {
  if (status.vault === "locked") return "attention";
  if (status.conflicts > 0) return "attention";
  if (status.agentsRunning > 0) return "active";
  return "neutral";
}

/** Colour is not a signal for everyone. The label says the state in words so
 *  a screen reader reaches the same conclusion the icon's tint offers. */
export function statusLabel(status: SystemStatus): string {
  const parts: string[] = [];
  if (status.vault === "locked") parts.push("vault locked");
  if (status.conflicts > 0) {
    parts.push(`${status.conflicts} sync conflict${status.conflicts === 1 ? "" : "s"}`);
  }
  if (status.agentsRunning > 0) {
    parts.push(`${status.agentsRunning} agent${status.agentsRunning === 1 ? "" : "s"} running`);
  }
  return parts.length === 0 ? "System status: all clear" : `System status: ${parts.join(", ")}`;
}

export function SystemStatusMenu({
  onResolveConflicts,
  onOpenAgents,
}: {
  onResolveConflicts: () => void;
  onOpenAgents: () => void;
}) {
  const sessions = useAgentSessionsStore((s) => s.sessions);
  const [vault, setVault] = useState<VaultState | null>(null);
  const [conflicts, setConflicts] = useState(0);

  const refresh = useCallback(async () => {
    const [state, list] = await Promise.all([
      vaultStatus().catch(() => null),
      syncConflicts().catch(() => []),
    ]);
    setVault(state);
    setConflicts(list.length);
  }, []);

  useEffect(() => {
    void refresh();
    // Neither the vault locking on idle nor a conflict arriving from another
    // device announces itself to the frontend, so this is a poll. Thirty
    // seconds is slow enough to be free and fast enough that a locked vault is
    // not a surprise at the next keystroke.
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const agentsRunning = sessions.filter(
    (s) => s.status === "running" || s.status === "awaiting_permission",
  ).length;
  const status: SystemStatus = { vault, conflicts, agentsRunning };
  const severity = severityOf(status);

  return (
    <DropdownMenu onOpenChange={(open) => open && void refresh()}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={statusLabel(status)}
              className={cn(
                "relative size-[26px] shrink-0 rounded-[7px] [--wails-draggable:no-drag]",
                severity === "attention" && "text-status-danger",
                severity === "active" && "text-action-primary",
              )}
            >
              <Activity className="size-3.5" />
              {severity !== "neutral" && (
                // The badge is the half of the signal that survives colour
                // blindness: presence, not hue.
                <span
                  className={cn(
                    "absolute right-1 top-1 size-1.5 rounded-full ring-2 ring-[var(--background-app)]",
                    severity === "attention" ? "bg-status-danger" : "bg-action-primary",
                  )}
                />
              )}
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{statusLabel(status)}</TooltipContent>
      </Tooltip>

      <DropdownMenuContent align="end" className="w-[260px]">
        <DropdownMenuItem
          disabled={vault !== "locked"}
          onSelect={() => requestVaultUnlock()}
        >
          {vault === "locked" ? (
            <Lock className="text-status-danger" />
          ) : (
            <LockOpen className="text-text-tertiary" />
          )}
          Vault
          <span className="ml-auto text-text-tertiary">
            {vault === "locked" ? "locked" : vault === "unlocked" ? "unlocked" : "—"}
          </span>
        </DropdownMenuItem>

        <DropdownMenuItem disabled={conflicts === 0} onSelect={onResolveConflicts}>
          <CloudCheck className={conflicts > 0 ? "text-status-danger" : "text-text-tertiary"} />
          Sync
          <span className="ml-auto text-text-tertiary">
            {conflicts > 0
              ? `${conflicts} conflict${conflicts === 1 ? "" : "s"}`
              : "in sync"}
          </span>
        </DropdownMenuItem>

        <DropdownMenuItem disabled={agentsRunning === 0} onSelect={onOpenAgents}>
          <Bot className={agentsRunning > 0 ? "text-action-primary" : "text-text-tertiary"} />
          Agent
          <span className="ml-auto text-text-tertiary">
            {agentsRunning > 0 ? `${agentsRunning} running` : "idle"}
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
