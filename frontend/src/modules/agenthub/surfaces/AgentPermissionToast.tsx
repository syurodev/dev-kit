import { Clock, Lock, PanelRight, ShieldAlert, Unlock } from "lucide-react";
import { usePermissionDeadline } from "./usePermissionDeadline";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions, permissionToolTitle } from "@/bridge/agenthub";

export type ToastEnv = "open" | "locked";

export type AgentPermissionToastProps = {
  pending: PendingPermission;
  session?: AgentSession | null;
  onResolve: (optionId: string | null) => void | Promise<void>;
  onOpenRail: () => void;
  busy?: boolean;
  timeoutSeconds?: number;
  env?: ToastEnv;
  className?: string;
};

/**
 * Agent / Permission Toast (Figma 924:152)
 * Floating toast card (420×220–248) positioned bottom-right over any module.
 *
 * Safety semantics:
 * - Shows the 60s fail-closed deadline clearly (never silently dismisses).
 * - Deliberately has NO "Allow always" option: long-lived grants require
 *   the context of the full transcript in the rail.
 * - "Allow once" is the primary button.
 * - "Open rail" allows viewing the full transcript and granting broader permissions.
 * - Names the asking agent, depth, and working directory.
 * - Non-modal: blocks the agent, not the person.
 */
export function AgentPermissionToast({
  pending,
  session,
  onResolve,
  onOpenRail,
  busy = false,
  timeoutSeconds = 60,
  env: explicitEnv,
  className,
}: AgentPermissionToastProps) {
  const { secondsLeft, expired } = usePermissionDeadline(pending, timeoutSeconds);

  // Derive environment: explicit prop or inspect params for locked/prod tags
  const isLocked =
    explicitEnv === "locked" ||
    (typeof pending.params === "object" &&
      pending.params !== null &&
      Boolean(
        (pending.params as Record<string, unknown>).locked ||
          (pending.params as Record<string, unknown>).env === "prod" ||
          (pending.params as Record<string, unknown>).environment === "prod",
      ));
  const env: ToastEnv = isLocked ? "locked" : "open";


  const toolTitle = permissionToolTitle(pending.params);
  const options = parsePermissionOptions(pending.params);
  const agentTitle = session?.title || pending.title || pending.sessionId.slice(0, 8);
  const cwd = session?.projectPath || ".";

  // Find allow once from options (or first available option)
  const allowOnceOption = options.find(
    (o) =>
      o.optionId.includes("once") ||
      o.name.toLowerCase().includes("once") ||
      o.kind.includes("once"),
  ) ?? options[0];

  // Tool parameter or command summary
  let paramSummary = "";
  if (pending.params && typeof pending.params === "object") {
    const p = pending.params as Record<string, unknown>;
    if (typeof p.command === "string") {
      paramSummary = p.command;
    } else if (typeof p.path === "string") {
      paramSummary = p.path;
    } else if (p.toolCall && typeof p.toolCall === "object") {
      const tc = p.toolCall as Record<string, unknown>;
      if (typeof tc.rawInput === "string") {
        paramSummary = tc.rawInput;
      }
    }
  }

  return (
    <aside
      role="alert"
      aria-label="Agent permission request"
      data-testid="agent-permission-toast"
      data-env={env}
      className={cn(
        "fixed bottom-4 right-4 z-50 flex w-[420px] flex-col rounded-xl border bg-surface-raised p-4 shadow-2xl backdrop-blur-sm select-none animate-in fade-in slide-in-from-bottom-3 duration-200",
        env === "locked"
          ? "border-status-warning/60 shadow-status-warning/5"
          : "border-border-subtle",
        className,
      )}
    >
      {/* 1. Header: Shield, Tool title, Env badge, and Deadline countdown */}
      <div className="flex items-center justify-between gap-2 pb-2.5 border-b border-border-subtle">
        <div className="flex items-center gap-2 min-w-0">
          <ShieldAlert
            className={cn(
              "size-4 shrink-0",
              env === "locked" ? "text-status-warning" : "text-text-primary",
            )}
            aria-hidden="true"
          />
          <h4 className="truncate text-title-16 text-text-primary">
            {toolTitle}
          </h4>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {env === "locked" ? (
            <span
              data-testid="toast-env-locked"
              className="inline-flex items-center gap-1 rounded-sm bg-status-warning/15 px-1.5 py-0.5 text-code-11 text-status-warning"
              title="Locked environment (requires confirmation)"
            >
              <Lock className="size-3 shrink-0" aria-hidden="true" />
              <span>LOCKED</span>
            </span>
          ) : (
            <span
              data-testid="toast-env-open"
              className="inline-flex items-center gap-1 rounded-sm bg-surface-base px-1.5 py-0.5 text-code-11 text-text-tertiary"
            >
              <Unlock className="size-3 shrink-0" aria-hidden="true" />
              <span>OPEN</span>
            </span>
          )}

          <div
            data-testid="toast-deadline"
            className={cn(
              "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-code-11",
              secondsLeft <= 10
                ? "bg-status-danger/15 text-status-danger"
                : "bg-surface-base text-text-secondary",
            )}
            title="Auto fail-closed deadline (gate closes after 60s)"
          >
            <Clock className="size-3 shrink-0" aria-hidden="true" />
            <span>{`${secondsLeft}s`}</span>
          </div>
        </div>
      </div>

      {/* 2. Identity Section: Agent Name, Depth, and Working Directory */}
      <div className="flex flex-col gap-1 py-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="text-body-13-medium text-text-primary truncate">
              {agentTitle}
            </span>
            <span className="rounded-sm bg-surface-base px-1 py-0.2 text-code-11 text-text-tertiary shrink-0">
              {`depth ${pending.depth}`}
            </span>
          </div>
        </div>

        <div className="flex items-center text-code-11 text-text-tertiary truncate">
          <span className="truncate">{cwd}</span>
        </div>

        {paramSummary ? (
          <div className="mt-1 rounded-sm border border-border-subtle bg-surface-base p-2 text-code-11 text-text-secondary overflow-x-auto max-h-16">
            <pre className="whitespace-pre">{paramSummary}</pre>
          </div>
        ) : null}
      </div>

      {/* 3. Actions: Allow once (primary), Open rail, Deny */}
      <div className="flex items-center justify-between gap-2 pt-2.5 border-t border-border-subtle">
        <button
          type="button"
          data-testid="toast-open-rail"
          onClick={onOpenRail}
          className="inline-flex h-8 items-center gap-1.5 rounded-control border border-border-subtle bg-surface-base px-3 text-label-12 text-text-primary transition-colors hover:bg-surface-tinted cursor-pointer"
        >
          <PanelRight className="size-3.5 shrink-0 text-text-tertiary" aria-hidden="true" />
          <span>Open rail</span>
        </button>

        <div className="flex items-center gap-1.5">
          <button
            type="button"
            data-testid="toast-deny"
            disabled={busy || expired}
            onClick={() => void onResolve(null)}
            className="h-8 rounded-control border border-border-subtle bg-surface-raised px-3 text-label-12 text-text-secondary transition-colors hover:text-text-primary hover:bg-surface-tinted disabled:opacity-50 cursor-pointer"
          >
            Deny
          </button>

          <button
            type="button"
            data-testid="toast-allow-once"
            disabled={busy || expired}
            onClick={() => void onResolve(allowOnceOption ? allowOnceOption.optionId : "allow_once")}
            className="inline-flex h-8 items-center justify-center rounded-control bg-action-primary px-3.5 text-label-12 text-action-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-50 cursor-pointer"
          >
            {allowOnceOption?.name || "Allow once"}
          </button>
        </div>
      </div>
    </aside>
  );
}
