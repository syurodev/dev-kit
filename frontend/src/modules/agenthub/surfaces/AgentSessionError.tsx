import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

/**
 * notInstalled and requirement are not crashes: the agent never started, and
 * the way out is Settings → Agents, not a restart that fails the same way.
 */
export type SessionErrorCause = "crashed" | "denied" | "notInstalled" | "requirement" | "authRequired";

const COPY: Record<SessionErrorCause, { title: string; retry: string; reason: string }> = {
  crashed: {
    title: "stopped",
    retry: "Restart",
    reason: "agenthub.processFailed — the agent exited before the turn finished.",
  },
  denied: {
    title: "turn failed",
    retry: "Retry turn",
    reason:
      "agenthub.permissionDenied — the permission request went unanswered, so the gate failed closed.",
  },
  notInstalled: {
    title: "can't start",
    retry: "Retry",
    reason: "agent_not_installed — Claude Agent is not installed. Install it in Settings → Agents.",
  },
  requirement: {
    title: "can't start",
    retry: "Retry",
    reason: "runtime_requirement — something the agent needs is missing from this machine.",
  },
  authRequired: {
    title: "isn't signed in",
    retry: "Retry turn",
    reason: "agent_auth_required — Claude isn't signed in, or the sign-in expired.",
  },
};

export type AgentSessionErrorProps = {
  cause?: SessionErrorCause;
  /** The agent this happened to. Figma leads with it, not with "Agent". */
  agentName?: string;
  /** "Last update 3 min ago · 2 turns recorded" — how much work is at stake. */
  meta?: string;
  /** The real identifier and what happened, e.g. "agenthub.processFailed — ..." */
  reason?: string;
  onClose?: () => void;
  onRetry?: () => void;
  /** Replaces Retry for notInstalled/requirement: the fix is in Settings → Agents. */
  onOpenSettings?: () => void;
  /** The way out when it is more than one button — authRequired's sign-in. */
  children?: React.ReactNode;
  className?: string;
  id?: string;
};

/**
 * Agent / Session Error — Figma `948:2204`, variants `948:2190` Crashed and
 * `948:2203` Denied.
 *
 * Figma's note on the node is the whole design: "Clear means the DevKit error
 * code, what actually happened, and the one action that gets you out — a
 * coloured dot on its own gives you nothing to do."
 *
 * So the code shown is the one the backend actually sent, never a fallback
 * constant, and the copy is the reason rather than static prose — otherwise a
 * crash reaches the screen with nothing on it to act on.
 *
 * The two variants differ only in copy and in the primary action's label; the
 * surface, the structure and the tones are identical, so they are one layout
 * with two defaults rather than two branches.
 */
export function AgentSessionError({
  cause = "crashed",
  agentName = "Agent",
  meta,
  reason,
  onClose,
  onRetry,
  onOpenSettings,
  children,
  className,
  id,
}: AgentSessionErrorProps) {
  const copy = COPY[cause];
  const title = `${agentName} ${copy.title}`;
  const retryLabel = copy.retry;
  const defaultReason = copy.reason;
  const setupCause = cause === "notInstalled" || cause === "requirement";

  return (
    <div
      id={id}
      role="alert"
      data-testid="agent-session-error"
      data-cause={cause}
      className={cn(
        "flex w-full max-w-134 flex-col gap-2 rounded-control p-3",
        "bg-(--feedback-danger-soft) border border-(--border-danger)",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2">
        {/* 6px dot. Figma exports it as an image so its fill cannot be read
            off the node; it takes the card's tone. */}
        <span
          aria-hidden="true"
          className="size-1.5 shrink-0 rounded-full bg-status-danger"
        />
        <p className="min-w-0 flex-1 text-body-13-medium text-text-primary">
          {title}
        </p>
        {meta ? (
          <p className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap">
            {meta}
          </p>
        ) : null}
      </div>

      <p
        data-testid="agent-session-error-reason"
        className="w-full text-body-12 text-text-secondary"
      >
        {reason ?? defaultReason}
      </p>

      {children}

      <div className="flex w-full items-center gap-2">
        <span className="min-w-0 flex-1" />
        {onClose ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="session-error-close"
            onClick={onClose}
            // h-8 because Figma's button is px-3/py-2 on a 16px line box — 32
            // tall, which is control-height-md while size="sm" is 28. Everything
            // else about sm is right: px-3, radius/8, Label/12.
            className="h-8 border-border-subtle text-text-secondary"
          >
            Close session
          </Button>
        ) : null}
        {setupCause && onOpenSettings ? (
          <Button
            type="button"
            size="sm"
            data-testid="session-error-open-settings"
            onClick={onOpenSettings}
            className="h-8"
          >
            Open agent settings
          </Button>
        ) : onRetry ? (
          <Button
            type="button"
            size="sm"
            data-testid="session-error-retry"
            onClick={onRetry}
            className="h-8"
          >
            {retryLabel}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
