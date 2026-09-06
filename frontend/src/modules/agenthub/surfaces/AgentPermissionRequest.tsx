import { Lock, ShieldAlert, Unlock } from "lucide-react";
import { usePermissionDeadline } from "./usePermissionDeadline";
import { cn } from "@/lib/utils";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions, permissionToolTitle } from "@/bridge/agenthub";

export type PermissionEnv = "open" | "locked";

export type AgentPermissionRequestProps = {
  pending: PendingPermission;
  session?: AgentSession | null;
  onResolve: (optionId: string | null) => void | Promise<void>;
  busy?: boolean;
  timeoutSeconds?: number;
  env?: PermissionEnv;
  className?: string;
};

/**
 * Agent / Permission Request (Figma 909:49)
 * Inline permission card inside AgentRail transcript (272×180–208).
 * Variants: Env Open / Locked.
 * Includes countdown deadline, caller identity, and full choice set (Allow once, Allow always, Deny).
 */
export function AgentPermissionRequest({
  pending,
  session,
  onResolve,
  busy = false,
  timeoutSeconds = 60,
  env: explicitEnv,
  className,
}: AgentPermissionRequestProps) {
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
  const env: PermissionEnv = isLocked ? "locked" : "open";


  const toolTitle = permissionToolTitle(pending.params);
  const options = parsePermissionOptions(pending.params);
  const agentTitle = session?.title || pending.title || pending.sessionId.slice(0, 8);
  const cwd = session?.projectPath || ".";

  // Find allow once and allow always from options, or fallback
  const allowOnceOption = options.find(
    (o) =>
      o.optionId.includes("once") ||
      o.name.toLowerCase().includes("once") ||
      o.kind.includes("once"),
  ) ?? options[0];

  const allowAlwaysOption = options.find(
    (o) =>
      o.optionId.includes("always") ||
      o.name.toLowerCase().includes("always") ||
      o.kind.includes("always"),
  );

  return (
    <div
      role="alert"
      data-testid="agent-permission-request"
      data-env={env}
      className={cn(
        "flex w-full max-w-[272px] flex-col rounded-lg border bg-surface-tinted p-3 shadow-sm select-none",
        env === "locked"
          ? "border-status-warning/60"
          : "border-border-subtle",
        className,
      )}
    >
      {/* Header: Tool title, Env tag, Countdown deadline */}
      <div className="flex items-center justify-between gap-1 pb-1.5 border-b border-border-subtle/50">
        <div className="flex items-center gap-1.5 min-w-0">
          <ShieldAlert
            className={cn(
              "size-3.5 shrink-0",
              env === "locked" ? "text-status-warning" : "text-text-secondary",
            )}
            aria-hidden="true"
          />
          <span className="truncate text-caption-11-medium text-text-primary">
            {toolTitle}
          </span>
        </div>

        <div className="flex items-center gap-1 shrink-0">
          {env === "locked" ? (
            <span
              data-testid="permission-env-locked"
              className="inline-flex items-center gap-0.5 rounded-sm bg-status-warning/15 px-1 py-0.2 text-code-11 text-status-warning"
              title="Locked environment (requires confirmation)"
            >
              <Lock className="size-2.5 shrink-0" aria-hidden="true" />
              <span>LOCKED</span>
            </span>
          ) : (
            <span
              data-testid="permission-env-open"
              className="inline-flex items-center gap-0.5 rounded-sm bg-surface-base px-1 py-0.2 text-code-11 text-text-tertiary"
            >
              <Unlock className="size-2.5 shrink-0" aria-hidden="true" />
              <span>OPEN</span>
            </span>
          )}

          <span
            data-testid="permission-deadline"
            className={cn(
              "rounded-sm px-1 py-0.2 text-code-11",
              secondsLeft <= 10
                ? "bg-status-danger/15 text-status-danger"
                : "bg-surface-base text-text-tertiary",
            )}
            title="Auto fail-closed deadline"
          >
            {`${secondsLeft}s`}
          </span>
        </div>
      </div>

      {/* Caller Identity */}
      <div className="flex flex-col gap-0.5 py-2">
        <div className="flex items-center justify-between text-caption-11">
          <span className="text-text-secondary truncate">
            {agentTitle}
          </span>
          <span className="text-text-tertiary shrink-0">
            {`depth ${pending.depth}`}
          </span>
        </div>
        <div className="flex items-center text-code-11 text-text-tertiary truncate">
          <span className="truncate">{cwd}</span>
        </div>
      </div>

      {/* Action Buttons: Allow once (primary), Allow always, Deny */}
      <div className="flex flex-col gap-1.5 pt-1.5 border-t border-border-subtle/50">
        <div className="flex items-center gap-1.5">
          {/* Allow once is always primary */}
          <button
            type="button"
            data-testid="permission-allow-once"
            disabled={busy || expired}
            onClick={() => void onResolve(allowOnceOption ? allowOnceOption.optionId : "allow_once")}
            className="flex-1 h-7 rounded-control bg-action-primary px-2 text-label-12 text-action-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-50 cursor-pointer text-center"
          >
            {allowOnceOption?.name || "Allow once"}
          </button>

          {/* Deny */}
          <button
            type="button"
            data-testid="permission-deny"
            disabled={busy || expired}
            onClick={() => void onResolve(null)}
            className="h-7 rounded-control border border-border-subtle bg-surface-raised px-2.5 text-label-12 text-text-secondary transition-colors hover:text-text-primary hover:bg-surface-tinted disabled:opacity-50 cursor-pointer"
          >
            Deny
          </button>
        </div>

        {/* Allow always (only available in rail where transcript is visible) */}
        {allowAlwaysOption ? (
          <button
            type="button"
            data-testid="permission-allow-always"
            disabled={busy || expired}
            onClick={() => void onResolve(allowAlwaysOption.optionId)}
            className="w-full h-6 rounded-control border border-border-subtle bg-surface-base px-2 text-caption-11 text-text-tertiary transition-colors hover:text-text-secondary hover:bg-surface-tinted disabled:opacity-50 cursor-pointer text-center"
          >
            {allowAlwaysOption.name || "Allow always"}
          </button>
        ) : null}
      </div>
    </div>
  );
}
