import { cn } from "@/lib/utils";
import type { Tone, UsageRow } from "./accountModel";

/** How safe a value is, in the chrome's status colours — the summary card's own mapping. */
export const TONE_TEXT: Record<Tone, string> = {
  ok: "text-status-success",
  warning: "text-status-warning",
  danger: "text-status-danger",
};

/**
 * One usage window: its name and what it says on one line, a meter under it. The
 * meter is the summary card's (role="meter", same bar, same transition); only the
 * fill is inline, because it is the one dynamic value.
 */
export function UsageMeter({ row }: { row: UsageRow }) {
  return (
    <div data-testid="account-usage-row" data-tone={row.tone} className="flex flex-col gap-1.5">
      <div className="flex items-baseline gap-2 text-body-12">
        <span className="shrink-0 text-text-secondary">{row.label}</span>
        <span className={cn("ml-auto min-w-0 truncate text-right tabular-nums", row.tone ? TONE_TEXT[row.tone] : "text-text-primary")}>
          {row.text}
        </span>
      </div>
      <div
        role="meter"
        aria-label={`${row.label} used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={row.percent}
        className="h-1 w-full overflow-hidden rounded-full bg-state-soft"
      >
        <div
          className="h-full w-full origin-left rounded-full bg-text-primary transition-transform duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none"
          style={{ transform: `scaleX(${row.percent / 100})` }}
        />
      </div>
    </div>
  );
}
