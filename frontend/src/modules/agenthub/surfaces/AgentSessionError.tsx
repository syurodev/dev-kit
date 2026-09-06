import { AlertCircle, ShieldAlert, RotateCcw, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";

export type SessionErrorCause = "crashed" | "denied";

export type AgentSessionErrorProps = {
  cause?: SessionErrorCause;
  errorCode?: string;
  message?: string;
  onRetry?: () => void;
  onSwitchVendor?: () => void;
  onClose?: () => void;
  className?: string;
};

/**
 * Agent / Session Error (Figma 536×124)
 * Banner/card reporting a crashed or denied session state with DevKit error code.
 */
export function AgentSessionError({
  cause = "crashed",
  errorCode,
  message,
  onRetry,
  onSwitchVendor,
  onClose,
  className,
}: AgentSessionErrorProps) {
  const isDenied = cause === "denied";
  const defaultCode = errorCode ?? (isDenied ? "DEV-KIT-403" : "DEV-KIT-500");
  const title = isDenied ? "Session Permission Denied" : "Agent Session Crashed";
  const description =
    message ??
    (isDenied
      ? "The execution request was rejected by caller identity policy or sandbox restriction."
      : "The agent process exited unexpectedly. Inspect terminal output or retry.");

  return (
    <div
      role="alert"
      data-testid="agent-session-error"
      className={cn(
        "flex max-w-[536px] flex-col gap-3 rounded-lg border border-status-danger/30 bg-surface-base p-3.5 shadow-sm",
        className,
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {isDenied ? (
            <ShieldAlert className="size-4 text-status-warning shrink-0" aria-hidden="true" />
          ) : (
            <AlertCircle className="size-4 text-status-danger shrink-0" aria-hidden="true" />
          )}
          <h4 className="text-title-16 text-text-primary">{title}</h4>
        </div>
        <span
          data-testid="agent-session-error-code"
          className="rounded-sm border border-border-subtle bg-surface-tinted px-1.5 py-0.5 text-caption-11-medium text-text-tertiary"
        >
          {defaultCode}
        </span>
      </div>

      <p className="text-body-12 text-text-secondary">{description}</p>

      <div className="flex items-center gap-2 pt-0.5">
        {onRetry ? (
          <button
            type="button"
            data-testid="session-error-retry"
            onClick={onRetry}
            className="inline-flex h-7 items-center gap-1.5 rounded-control bg-action-primary px-3 text-label-12 text-action-primary-foreground transition-colors hover:opacity-90 cursor-pointer"
          >
            <RotateCcw className="size-3 shrink-0" aria-hidden="true" />
            <span>Retry Session</span>
          </button>
        ) : null}

        {onSwitchVendor ? (
          <button
            type="button"
            data-testid="session-error-switch-vendor"
            onClick={onSwitchVendor}
            className="inline-flex h-7 items-center gap-1.5 rounded-control border border-border-subtle bg-surface-raised px-3 text-label-12 text-text-primary transition-colors hover:bg-surface-tinted cursor-pointer"
          >
            <span>Switch to Mock</span>
          </button>
        ) : null}

        {onClose ? (
          <button
            type="button"
            data-testid="session-error-close"
            onClick={onClose}
            className="inline-flex h-7 items-center gap-1.5 rounded-control px-2 text-label-12 text-text-tertiary transition-colors hover:text-text-primary cursor-pointer ml-auto"
          >
            <XCircle className="size-3.5 shrink-0" aria-hidden="true" />
            <span>Close</span>
          </button>
        ) : null}
      </div>
    </div>
  );
}
