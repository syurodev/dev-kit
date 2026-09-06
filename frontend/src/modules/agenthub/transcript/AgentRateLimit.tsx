import { TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentRateLimitProps = {
  level: "warning" | "exhausted";
  /** The whole sentence. Figma writes the number and the reset time into it. */
  message?: string;
  /** "14:20" — when the window rolls over. */
  resetTime?: string;
  /** Percent of the window left, for the default warning sentence. */
  remaining?: number;
  onDetails?: () => void;
  className?: string;
};

/**
 * Agent / Rate Limit — Figma `1044:3195`, variants `1044:3179` Warning and
 * `1044:3187` Exhausted.
 *
 * One row, and the text stays in the ordinary secondary tier in both variants.
 * Only the ground and the icon carry the level — state/soft with a
 * status/warning triangle, or feedback/danger-soft with a status/danger one.
 * The previous version tinted the sentence itself and put a coloured border
 * around the row, which made a warning about a quota shout louder than the
 * session error next to it.
 *
 * Both sentences name the time you get it back. A limit with no reset time is
 * indistinguishable from a broken agent.
 */
export function AgentRateLimit({
  level,
  message,
  resetTime,
  remaining,
  onDetails,
  className,
}: AgentRateLimitProps) {
  const exhausted = level === "exhausted";

  const fallback = exhausted
    ? `Rate limit reached — the next turn is blocked${resetTime ? ` until ${resetTime}` : ""}`
    : `${typeof remaining === "number" ? `${remaining}% of the` : "Little of the"} window left${
        resetTime ? ` — resets ${resetTime}` : ""
      }`;

  return (
    <div
      data-testid="agent-rate-limit"
      data-level={level}
      className={cn(
        "flex w-full max-w-full items-center gap-2 rounded-control p-3",
        exhausted ? "bg-(--feedback-danger-soft)" : "bg-(--state-soft)",
        className,
      )}
    >
      <TriangleAlert
        aria-hidden="true"
        className={cn(
          "size-3 shrink-0",
          exhausted ? "text-status-danger" : "text-status-warning",
        )}
      />
      <p className="min-w-0 flex-1 text-body-12 text-text-secondary">
        {message ?? fallback}
      </p>
      {onDetails ? (
        <button
          type="button"
          onClick={onDetails}
          className="shrink-0 text-caption-11-medium whitespace-nowrap text-text-tertiary hover:text-text-primary cursor-pointer"
        >
          Details
        </button>
      ) : null}
    </div>
  );
}
