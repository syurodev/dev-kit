import { cn } from "@/lib/utils";
import { VendorIcon } from "../surfaces/VendorIcon";

export type AvatarState = "running" | "waiting" | "error" | null;

/**
 * A session's face: the vendor's mark on a 30px disc, and its state drawn on
 * the disc rather than beside it — an arc going round while it works, a dot
 * when it waits on you or failed. Idle draws nothing: a row of identical
 * "idle" marks says nothing.
 */
export function SessionAvatar({
  vendor,
  state,
  className,
}: {
  vendor: string;
  state: AvatarState;
  className?: string;
}) {
  return (
    <span
      data-slot="session-avatar"
      data-state={state ?? "idle"}
      className={cn("relative inline-flex size-7.5 shrink-0", className)}
    >
      <span className="flex size-full items-center justify-center rounded-full bg-surface-base ring-1 ring-border-subtle ring-inset">
        <VendorIcon vendor={vendor} className="size-3.75" />
      </span>
      {state === "running" ? (
        <span aria-hidden="true" className="agent-avatar-spin pointer-events-none absolute -inset-0.75 rounded-full" />
      ) : null}
      {state === "waiting" || state === "error" ? (
        <span
          aria-hidden="true"
          className={cn(
            "agent-avatar-badge absolute -right-px -bottom-px size-2.5 rounded-full",
            state === "waiting" ? "agent-avatar-breathe bg-status-warning" : "bg-status-danger",
          )}
        />
      ) : null}
    </span>
  );
}
