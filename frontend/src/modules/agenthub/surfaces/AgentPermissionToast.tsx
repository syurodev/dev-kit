import { usePermissionDeadline } from "./usePermissionDeadline";
import { AgentState } from "./AgentState";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
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
  /** How many other requests are waiting behind this one. */
  queued?: number;
  className?: string;
};

/**
 * Agent / Permission Toast — Figma `924:152`, variants `924:124` Open and
 * `924:151` Locked.
 *
 * Figma's note: "Asks for permission when the rail is closed or you are in
 * another module — bottom-right, floating over the module. Carries the same
 * identity as the rail card (who, depth, cwd) plus the 60s deadline, because
 * the gate fails closed and a toast that quietly expired would deny the turn
 * with no trace. Deliberately has no allow-always: a grant that outlives the
 * turn should not be given from the surface with the least context."
 *
 * Two things the rail card lacks, both because this is the surface you are NOT
 * looking at: a queue pill counting what is waiting, and the deadline bar.
 *
 * The action row escalates left to right: "Open rail" for the full transcript,
 * then Deny, then Allow once. No allow-always, per the note.
 */
export function AgentPermissionToast({
  pending,
  session,
  onResolve,
  onOpenRail,
  busy = false,
  timeoutSeconds = 60,
  env: explicitEnv,
  queued = 0,
  className,
}: AgentPermissionToastProps) {
  const { secondsLeft, expired } = usePermissionDeadline(pending, timeoutSeconds);

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
  const agentTitle =
    session?.title || pending.title || pending.sessionId.slice(0, 8);
  const cwd = session?.projectPath || ".";
  const depth = pending.depth ?? session?.depth ?? 0;

  const command =
    typeof pending.params === "object" && pending.params !== null
      ? ((pending.params as Record<string, unknown>).command as string | undefined)
      : undefined;

  const allowOnceOption =
    options.find(
      (o) =>
        o.optionId.includes("once") ||
        o.name.toLowerCase().includes("once") ||
        o.kind.includes("once"),
    ) ?? options[0];

  // Elapsed, not remaining: Figma fills the track as the deadline is consumed.
  const elapsedPercent =
    timeoutSeconds > 0
      ? Math.min(100, Math.max(0, ((timeoutSeconds - secondsLeft) / timeoutSeconds) * 100))
      : 0;

  return (
    <div
      role="alert"
      data-testid="agent-permission-toast"
      data-env={env}
      className={cn(
        "flex w-full max-w-105 flex-col gap-3 rounded-control p-4",
        "border border-(--glass-border) bg-(--glass-float-bg) backdrop-blur-md",
        "shadow-[0px_12px_32px_-4px_rgba(0,0,0,0.45),inset_0px_1px_0px_0px_rgba(255,255,255,0.06)]",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        <AgentState status="awaiting" />
        <p className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">
          {agentTitle}
        </p>
        <p className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">
          {`depth ${depth}`}
        </p>
        {queued > 0 ? (
          <span
            data-testid="toast-queue"
            className="shrink-0 rounded-full bg-(--state-soft) px-2 py-1 text-caption-11-medium text-text-secondary whitespace-nowrap"
          >
            {`+${queued} waiting`}
          </span>
        ) : null}
      </div>

      <p className="w-full text-caption-11 text-text-tertiary">{cwd}</p>

      <div className="flex w-full items-center gap-2">
        <p className="min-w-0 flex-1 truncate text-label-12 text-text-secondary">
          {toolTitle}
        </p>
        <span
          data-testid={env === "locked" ? "toast-env-locked" : "toast-env-open"}
          className={cn(
            "shrink-0 rounded-full px-2 py-1 text-caption-11-medium",
            env === "locked"
              ? "bg-(--feedback-danger-soft) text-status-danger"
              : "bg-(--state-soft) text-text-secondary",
          )}
        >
          {env === "locked" ? "prod" : "dev"}
        </span>
      </div>

      {command ? (
        <p className="w-full text-body-12 text-text-primary break-all">{command}</p>
      ) : null}

      {env === "locked" ? (
        <p className="w-full text-caption-11 text-text-tertiary">
          prod requires confirmation every time
        </p>
      ) : null}

      <div className="flex w-full items-center gap-2">
        <Progress
          value={elapsedPercent}
          aria-label="Time left to answer"
          data-testid="toast-deadline"
          className="h-0.75 min-w-0 flex-1 bg-(--state-soft) [&_[data-slot=progress-indicator]]:bg-text-tertiary"
        />
        <span className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">
          {expired ? "expired" : `${secondsLeft}s left`}
        </span>
      </div>

      <div className="flex w-full items-center gap-2">
        <Button
          type="button"
          variant="link"
          data-testid="toast-open-rail"
          onClick={onOpenRail}
          className="h-auto min-w-0 flex-1 justify-start p-0 text-caption-11-medium text-text-tertiary no-underline hover:text-text-primary"
        >
          Open rail
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || expired}
          data-testid="toast-deny"
          onClick={() => void onResolve(null)}
          className="h-8 border-(--glass-border) px-3 text-text-secondary"
        >
          Deny
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={busy || expired}
          data-testid="toast-allow-once"
          onClick={() =>
            void onResolve(allowOnceOption ? allowOnceOption.optionId : "allow_once")
          }
          className="h-8 px-3"
        >
          Allow once
        </Button>
      </div>
    </div>
  );
}
