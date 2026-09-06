import { AlertOctagon, AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentRateLimitProps = {
  /**
   * Rate limit alert banner.
   * Figma node: 1044:3195 (Agent / Rate Limit)
   * Variants: Warning / Exhausted
   */
  level: "warning" | "exhausted";
  message?: string;
  resetTime?: string;
  remaining?: number;
  className?: string;
};

export function AgentRateLimit({
  level,
  message,
  resetTime,
  remaining,
  className,
}: AgentRateLimitProps) {
  const isExhausted = level === "exhausted";
  const Icon = isExhausted ? AlertOctagon : AlertTriangle;

  const defaultMessage = isExhausted
    ? "Rate limit exhausted — requests paused until reset"
    : typeof remaining === "number"
      ? `Rate limit approaching (${remaining} requests remaining)`
      : "Rate limit threshold reached";

  return (
    <div
      className={cn(
        "flex h-(--control-height-xl) w-full max-w-full items-center justify-between gap-2.5 rounded-md border px-3",
        isExhausted
          ? "border-status-danger/30 bg-status-danger/10"
          : "border-status-warning/30 bg-status-warning/10",
        className,
      )}
      data-testid="agent-rate-limit"
      data-level={level}
    >
      <div className="flex items-center gap-2 min-w-0">
        <Icon
          className={cn(
            "size-(--icon-14) shrink-0",
            isExhausted ? "text-status-danger" : "text-status-warning",
          )}
        />
        <span
          className={cn(
            "truncate text-caption-11-medium",
            isExhausted ? "text-status-danger" : "text-status-warning",
          )}
        >
          {message ?? defaultMessage}
        </span>
      </div>

      {resetTime && (
        <span
          className={cn(
            "shrink-0 text-caption-11",
            isExhausted ? "text-status-danger/80" : "text-status-warning/80",
          )}
        >
          {`Resets in ${resetTime}`}
        </span>
      )}
    </div>
  );
}
