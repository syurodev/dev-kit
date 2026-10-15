import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import type { SessionFailure } from "../types";

/** What the vendor's action hints do, in the user's words. */
const ACTION_LABEL: Record<string, string> = {
  login: "Sign in",
  retry: "Retry turn",
  resume: "Resume session",
  new_session: "New session",
};

/**
 * A problem the vendor reports about the session as a whole — a sign-in that
 * expired, a usage limit reached, a provider outage. It arrives on a
 * session_info_update (the air extension's sessionFailure, which DevKit asks
 * for in its client capabilities), and it is the only structured word the
 * agent gives for any of these: without it a turn simply stops, and the user
 * is left to guess.
 *
 * Built like AgentSessionError: one row on the app's own surface, with the
 * tone in the dot and the border rather than a filled block. A warning is a
 * notice, not a session that stopped, so it is toned down further.
 *
 * The buttons are the vendor's own hints ("login", "retry", "new_session") —
 * the ones this app can act on. An action it does not know is dropped rather
 * than drawn as a button that does nothing.
 */
export function AgentSessionFailure({
  failure,
  onAction,
  onDismiss,
  className,
}: {
  failure: SessionFailure;
  onAction: (action: string) => void;
  onDismiss?: () => void;
  className?: string;
}) {
  const warning = failure.severity === "warning";
  const actions = (failure.actions ?? []).filter((action) => action in ACTION_LABEL);
  const detail = failure.details || failure.reason || failure.category;

  return (
    <div
      role="alert"
      data-testid="agent-session-failure"
      data-severity={failure.severity}
      className={cn(
        "flex w-full max-w-180 flex-col gap-2 rounded-control px-3 py-2",
        "border bg-surface-raised",
        warning ? "border-(--border-warning)" : "border-(--border-danger)",
        className,
      )}
    >
      <div className="flex w-full flex-wrap items-center gap-x-2 gap-y-1.5">
        <span
          aria-hidden="true"
          className={cn("size-1.5 shrink-0 rounded-full", warning ? "bg-status-warning" : "bg-status-danger")}
        />
        <p className="shrink-0 text-body-13-medium text-text-primary">{failure.title}</p>
        {detail ? (
          <p
            data-testid="agent-session-failure-detail"
            title={detail}
            className="min-w-30 flex-1 truncate text-caption-11 text-text-tertiary"
          >
            {detail}
          </p>
        ) : (
          <span className="min-w-30 flex-1" />
        )}
        <div className="flex shrink-0 items-center gap-2">
          {onDismiss ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-testid="session-failure-dismiss"
              onClick={onDismiss}
              className="h-8 text-text-secondary"
            >
              Dismiss
            </Button>
          ) : null}
          {actions.map((action, index) => (
            <Button
              key={action}
              type="button"
              // The first hint the vendor gives is the one it means; the rest
              // are alternatives and read as such.
              variant={index === 0 ? "default" : "outline"}
              size="sm"
              data-testid={`session-failure-${action}`}
              onClick={() => onAction(action)}
              className={cn("h-8", index > 0 && "border-border-subtle")}
            >
              {ACTION_LABEL[action]}
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
}
