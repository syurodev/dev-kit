import type { ReactElement } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ExitMarker } from "./outlineHints";

/**
 * The outline's small marks, drawn the same in a row and in the legend that
 * explains them.
 */

/** Longer than a button's: moving down the list should not light a tooltip on every row (as the sidebar tree). */
const HINT_DELAY = 600;

/** The app's tooltip on one mark. It wraps, since a hint is a sentence. */
export function Hint({ text, children }: { text: string; children: ReactElement }) {
  return (
    <Tooltip delayDuration={HINT_DELAY}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="top" className="h-auto max-w-80 py-1.5 whitespace-normal">
        {text}
      </TooltipContent>
    </Tooltip>
  );
}

/** A Hint when there is something to say. */
export function MaybeHint({ text, children }: { text: string | null; children: ReactElement }) {
  return text ? <Hint text={text}>{children}</Hint> : children;
}

const CHIP = "shrink-0 rounded-sm border px-1.5 text-caption-11";

/** How a branch leaves: a word, not an arrow. A throw wears the chrome's warning, the rest stay quiet. */
export function ExitChip({ marker, className }: { marker: ExitMarker; className?: string }) {
  return (
    <span
      data-exit={marker.word}
      className={cn(CHIP, marker.tone === "warning" ? "border-status-warning/50 text-status-warning" : "border-border-subtle text-text-tertiary", className)}
    >
      {marker.word}
    </span>
  );
}

/** "3 calls · logging": the calls folded away. */
export const FOLD_CHIP = "truncate rounded-sm border border-dashed border-border px-1.5 text-caption-11 text-text-tertiary";

/** "↳ 2 implementations", "(base)", "shown above ↑", "↻ recursion". */
export const QUIET_MARK = "shrink-0 text-caption-11 text-text-tertiary";

/** A branch heading's text: which kind of row it is, so a palette hue. */
export const BRANCH_TEXT = "text-(--hue-blue)";

export function implementationsLabel(n: number): string {
  return `↳ ${n} ${n === 1 ? "implementation" : "implementations"}`;
}
