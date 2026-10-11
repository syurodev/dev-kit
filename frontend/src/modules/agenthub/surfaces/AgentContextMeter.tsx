import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { AnimatedNumber, AnimatedNumberGroup } from "@/components/ui/animated-number";
import { Progress } from "@/components/ui/progress";
import type { RateLimit, SessionFailure, Usage } from "../types";

export type AgentContextMeterProps = {
  usage?: Usage | null;
  turnCount?: number;
  /** Sends /compact. Omitted when the agent offers no such command. */
  onCompact?: () => void;
  /** Controlled by the composer, so only one of its popovers is ever open. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  className?: string;
  /** Percentages read as usage rather than a loading indicator in the composer. */
  appearance?: "ring" | "percentage";
};

/** "24.5k", "1M" — the precision a glance needs. */
export function shortCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}k`;
  return String(n);
}

/**
 * How full the context window is, and what that costs. Warning at 80%,
 * danger at 95%: past those the agent is about to compact or refuse, and the
 * ring changing colour is the notice a glance picks up.
 */
function level(percent: number): "info" | "warning" | "danger" {
  if (percent >= 95) return "danger";
  if (percent >= 80) return "warning";
  return "info";
}

const RING = { info: "stroke-status-info", warning: "stroke-status-warning", danger: "stroke-status-danger" };
const BAR = {
  info: "[&>[data-slot=progress-indicator]]:bg-status-info",
  warning: "[&>[data-slot=progress-indicator]]:bg-status-warning",
  danger: "[&>[data-slot=progress-indicator]]:bg-status-danger",
};

/**
 * The composer's context gauge: a ring the size of an icon, and the numbers
 * behind it one click away.
 *
 * It replaces "29,480 / 1,000,000 context" spelled out beside the pills and
 * "2 turns · $0.91" in a band above the field — two lines of numbers a person
 * reads a few times a session, sitting on the composer every moment of it. The
 * ring answers the only question worth a glance, how full is it; the popover
 * holds the rest.
 *
 * What it does not show is what ACP does not say. Claude's own gauge also has
 * the auto-compact threshold and plan usage limits, and neither is on the
 * wire: a made-up "compacts at 97%" is worse than no line.
 */
/** What the vendor's rate-limit status means, in the user's terms. */
export function rateLimitLabel(status: string): string {
  switch (status) {
    case "rejected":
      return "Usage limit reached";
    case "allowed_warning":
      return "Usage limit close";
    default:
      return "Usage limit";
  }
}

/** The right-hand side of the rate-limit line: how much is used, and when it resets. */
export function rateLimitDetail(limit: RateLimit, now = Date.now()): string {
  const parts: string[] = [];
  if (typeof limit.utilization === "number") parts.push(`${Math.round(limit.utilization)}%`);
  if (limit.resetsAt) {
    const minutes = Math.max(0, Math.round((limit.resetsAt * 1000 - now) / 60_000));
    // Hours once it is far enough out that counting minutes says nothing: a
    // five-hour window opens with 300 of them.
    parts.push(minutes >= 90 ? `resets in ${Math.round(minutes / 60)}h` : `resets in ${minutes}m`);
  }
  return parts.join(" · ") || "no detail";
}

/**
 * A rejected rate limit, read as the same session-scoped problem a vendor's
 * own sessionFailure is — so it gets the same banner instead of only the
 * composer's context-meter popover, which nobody opens unprompted. Claude
 * only ever reports this on a usage_update's _claude/rateLimit, never through
 * the air extension's sessionFailure the rest of that banner exists for, so
 * without this a rejected turn had nothing on screen to say why: the turn
 * just stopped, same as the "user is left to guess" AgentSessionFailure was
 * built to end.
 *
 * "allowed_warning" stays in the popover only — the turn still went through,
 * so it is not yet a problem the session-level banner should interrupt with.
 *
 * revision is resetsAt, not a counter: the same window keeps re-arriving on
 * every usage_update while still rejected, and a dismissed banner must not
 * reopen until a new window (a new resetsAt) makes it news again.
 */
export function rateLimitFailure(usage: Usage | null | undefined): SessionFailure | null {
  const limit = usage?.rateLimit;
  if (!limit || limit.status !== "rejected") return null;
  return {
    id: "rate-limit",
    revision: limit.resetsAt ?? 0,
    category: "usage_limit",
    severity: "error",
    title: rateLimitLabel(limit.status),
    details: rateLimitDetail(limit),
  };
}

export function AgentContextMeter({
  usage,
  turnCount = 0,
  onCompact,
  open,
  onOpenChange,
  className,
  appearance = "ring",
}: AgentContextMeterProps) {
  const used = usage?.used ?? 0;
  const size = usage?.size ?? 0;
  if (!size) return null;

  const percent = Math.min(100, Math.round((used / size) * 100));
  const tone = level(percent);
  // r=6 on a 16 box: the circumference the dash array divides.
  const circumference = 2 * Math.PI * 6;

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size={appearance === "percentage" ? "sm" : "icon-sm"}
          title={`Context window ${percent}% used`}
          aria-label={`Context: ${shortCount(used)} of ${shortCount(size)} used (${percent}%)`}
          data-testid="composer-context"
          data-level={tone}
          className={cn("shrink-0 rounded-full", appearance === "percentage" && "h-7 w-11 px-2 text-caption-11 text-text-tertiary", appearance === "percentage" && tone === "warning" && "text-status-warning", appearance === "percentage" && tone === "danger" && "text-status-danger", className)}
        >
          {appearance === "percentage" ? <AnimatedNumber value={percent} suffix="%" /> : <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4 -rotate-90">
            {/* The token directly: --border-default has no colour utility, and
                `stroke-border-default` compiled to nothing — the track was
                missing, so at 3% the ring was a lone dot. */}
            <circle cx="8" cy="8" r="6" fill="none" strokeWidth="2" className="stroke-(--border-default)" />
            <circle
              cx="8"
              cy="8"
              r="6"
              fill="none"
              strokeWidth="2"
              strokeLinecap="round"
              strokeDasharray={circumference}
              strokeDashoffset={circumference * (1 - percent / 100)}
              className={cn(RING[tone], "transition-[stroke,stroke-dashoffset] duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none")}
            />
          </svg>}
        </Button>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-80 max-w-gutter gap-3 p-4">
        <div className="flex w-full items-baseline justify-between gap-3 text-body-13">
          <span className="text-text-secondary">Context window</span>
          <span className="tabular-nums text-text-secondary">{shortCount(used)} / {shortCount(size)}</span>
        </div>
        <div className="flex w-full items-center gap-3">
          <Progress value={percent} className={cn("h-1.5 flex-1 bg-state-soft", BAR[tone])} />
          <AnimatedNumber value={percent} suffix="%" className="min-w-10 text-right text-caption-11 text-text-secondary" />
        </div>
        {/* The subscription's own ceiling. Separate from the context window
            above it, and not derivable from it: a turn can fit and still be
            refused, and the only thing that fixes that is waiting. */}
        {usage?.rateLimit ? (
          <>
            <div className="h-px w-full bg-border-subtle" />
            <div data-testid="composer-rate-limit" className="flex w-full items-baseline justify-between gap-3 text-body-13">
              <span className={usage.rateLimit.status === "allowed" ? "text-text-secondary" : "text-status-warning"}>
                {rateLimitLabel(usage.rateLimit.status)}
              </span>
              <span className="tabular-nums text-text-tertiary">{rateLimitDetail(usage.rateLimit)}</span>
            </div>
          </>
        ) : null}
        <div className="h-px w-full bg-border-subtle" />
        <div className="flex w-full items-baseline justify-between gap-3 text-body-13">
          <AnimatedNumberGroup><span className="text-caption-11 text-text-tertiary">
            <AnimatedNumber value={turnCount} suffix={turnCount === 1 ? " turn" : " turns"} /> · <AnimatedNumber value={usage?.amount ?? 0} prefix="$" format={{ minimumFractionDigits: 2, maximumFractionDigits: 2 }} />
          </span></AnimatedNumberGroup>
          {onCompact ? (
            <Button
              variant="ghost" size="xs"
              type="button"
              onClick={onCompact}
              data-testid="composer-compact"
              className="text-status-info"
            >
              Compact session
            </Button>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}
