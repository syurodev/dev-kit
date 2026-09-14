import { useCallback, useEffect, useState } from "react";
import { Activity, Bot, CloudCheck, Lock, LockOpen } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { vaultStatus, type VaultState } from "../bridge/vault";
import { syncConflicts } from "../bridge/sync";
import { useAgentSessionList } from "../stores/agentSessions";
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
  const sessions = useAgentSessionList();
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
            {severity === "neutral" ? (
              <Button
                type="button"
                variant="titlebar"
                size="icon-sm"
                aria-label={statusLabel(status)}
              >
                <Activity className="size-4" />
              </Button>
            ) : severity === "attention" ? (
              <button
                type="button"
                aria-label={statusLabel(status)}
                className="size-7 shrink-0 rounded-none p-0 flex items-center justify-center transition-colors cursor-pointer text-status-danger hover:opacity-90 [--wails-draggable:no-drag]"
              >
                {/* Full-bleed 28x28 attention graphic without radius wrapper */}
                <svg
                  width="28"
                  height="28"
                  viewBox="0 0 28 28"
                  fill="none"
                  xmlns="http://www.w3.org/2000/svg"
                  className="size-7"
                >
                  <path
                    d="M5 14H8.5L11 8.5L14.5 19.5L17 14H23"
                    stroke="currentColor"
                    strokeWidth="1.75"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                  <circle cx="21" cy="7" r="3" fill="currentColor" />
                </svg>
              </button>
            ) : (
              <button
                type="button"
                aria-label={statusLabel(status)}
                className="size-7 shrink-0 rounded-none p-0 flex items-center justify-center transition-colors cursor-pointer text-action-primary hover:opacity-90 [--wails-draggable:no-drag]"
              >
                {/* Full-bleed 28x28 active graphic without radius wrapper */}
                <svg
                  width="28"
                  height="28"
                  viewBox="0 0 28 28"
                  fill="none"
                  xmlns="http://www.w3.org/2000/svg"
                  className="size-7"
                >
                  <path
                    d="M5 14H8.5L11 8.5L14.5 19.5L17 14H23"
                    stroke="currentColor"
                    strokeWidth="1.75"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                  <circle cx="21" cy="7" r="2.5" fill="currentColor" />
                </svg>
              </button>
            )}
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{statusLabel(status)}</TooltipContent>
      </Tooltip>

      <DropdownMenuContent align="end" className="w-[260px]">
        <DropdownMenuItem
          disabled={vault !== "locked"}
          onSelect={() => requestVaultUnlock()}
          icon={
            vault === "locked" ? (
              <Lock className="text-status-danger" />
            ) : (
              <LockOpen className="text-text-tertiary" />
            )
          }
          detail={vault === "locked" ? "locked" : vault === "unlocked" ? "unlocked" : "—"}
        >
          Vault
        </DropdownMenuItem>

        <DropdownMenuItem
          disabled={conflicts === 0}
          onSelect={onResolveConflicts}
          icon={<CloudCheck className={conflicts > 0 ? "text-status-danger" : "text-text-tertiary"} />}
          detail={
            conflicts > 0
              ? `${conflicts} conflict${conflicts === 1 ? "" : "s"}`
              : "in sync"
          }
        >
          Sync
        </DropdownMenuItem>

        <DropdownMenuItem
          disabled={agentsRunning === 0}
          onSelect={onOpenAgents}
          icon={<Bot className={agentsRunning > 0 ? "text-action-primary" : "text-text-tertiary"} />}
          detail={agentsRunning > 0 ? `${agentsRunning} running` : "idle"}
        >
          Agent
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
