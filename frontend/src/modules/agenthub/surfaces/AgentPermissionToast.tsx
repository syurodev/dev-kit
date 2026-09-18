import { usePermissionDeadline } from "./usePermissionDeadline";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { parsePermissionOptions } from "@/bridge/agenthub";
import {
  PermissionAsk,
  PermissionIdentity,
  PermissionLockedNote,
  URGENT_SECONDS,
  permissionChoices,
  permissionEnv,
  type PermissionEnv,
} from "./permissionCard";

export type ToastEnv = PermissionEnv;

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
 * Identity and ask are the inline card's, from permissionCard. What is only
 * here belongs to the surface you are NOT looking at: a queue pill counting
 * what is behind this one, a deadline hairline along the bottom edge that fills
 * as the time is spent, and Open rail for the transcript this came out of.
 *
 * Material is the shell's float tier (`glass-float`), the same as every other
 * toast in the app.
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
  const { secondsLeft, expired } = usePermissionDeadline(pending);
  const env = permissionEnv(pending.params, explicitEnv);
  const { once } = permissionChoices(parsePermissionOptions(pending.params));

  // Elapsed, not remaining: Figma fills the track as the deadline is consumed.
  // Nothing to fill when nothing expires, which is the ordinary case — the
  // gate waits for the user (see usePermissionDeadline).
  const elapsedPercent =
    secondsLeft !== undefined && timeoutSeconds > 0
      ? Math.min(100, Math.max(0, ((timeoutSeconds - secondsLeft) / timeoutSeconds) * 100))
      : 0;
  const urgent = !expired && secondsLeft !== undefined && secondsLeft <= URGENT_SECONDS;

  return (
    <div
      role="alert"
      data-testid="agent-permission-toast"
      data-env={env}
      className={cn(
        "glass-float relative flex w-full max-w-105 flex-col gap-3 overflow-hidden rounded-float p-4",
        className,
      )}
    >
      <PermissionIdentity
        pending={pending}
        session={session}
        env={env}
        secondsLeft={secondsLeft}
        expired={expired}
        testIdPrefix="toast"
        trailing={
          queued > 0 ? (
            <span
              data-testid="toast-queue"
              className="shrink-0 rounded-full bg-state-soft px-2 py-0.5 text-caption-11-medium text-text-secondary whitespace-nowrap"
            >
              {`+${queued} waiting`}
            </span>
          ) : undefined
        }
      />

      <PermissionAsk pending={pending} />
      <PermissionLockedNote env={env} />

      {/* The action row escalates left to right: Open rail for the full
          transcript, then Deny, then Allow once. No allow-always, per the note. */}
      <div className="flex w-full items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          data-testid="toast-open-rail"
          onClick={onOpenRail}
          className="-ml-2 min-w-0 flex-1 justify-start px-2 text-text-tertiary hover:text-text-primary"
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
          className="border-border-subtle text-text-secondary"
        >
          Deny
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={busy || expired}
          data-testid="toast-allow-once"
          onClick={() => void onResolve(once ? once.optionId : "allow_once")}
        >
          Allow once
        </Button>
      </div>

      {/* Only when something is running out. A rail at 0% under a toast that
          never expires is a progress bar for a process with no end. */}
      {secondsLeft === undefined ? null : (
      <Progress
        value={elapsedPercent}
        aria-label="Time left to answer"
        data-testid="toast-deadline"
        className={cn(
          "absolute inset-x-0 bottom-0 h-0.5 rounded-none bg-transparent",
          urgent
            ? "[&_[data-slot=progress-indicator]]:bg-status-warning"
            : "[&_[data-slot=progress-indicator]]:bg-text-tertiary/40",
        )}
      />
      )}
    </div>
  );
}
