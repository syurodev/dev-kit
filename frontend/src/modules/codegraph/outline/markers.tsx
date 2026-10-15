import type { ReactElement } from "react";
import { ArrowUp, CornerDownLeft, CornerDownRight, Repeat } from "lucide-react";
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

/**
 * How a branch leaves: the ↩ after its heading, its tooltip saying which way
 * (exitMarker). A throw is tinted with the chrome's warning, the rest stay quiet.
 */
export function ExitIcon({ marker }: { marker: ExitMarker }) {
  return (
    <CornerDownLeft
      data-exit={marker.word}
      aria-label={`branch ${marker.word}`}
      className={cn("size-3 shrink-0", marker.tone === "warning" ? "text-status-warning" : "text-text-tertiary")}
    />
  );
}

/** "3 calls · logging": the calls folded away. */
export const FOLD_CHIP = "truncate rounded-sm border border-dashed border-border px-1.5 text-caption-11 text-text-tertiary";

/** "2 implementations", "(base)", "shown above", "recursion" — each with its lucide icon. */
export const QUIET_MARK = "shrink-0 text-caption-11 text-text-tertiary";

/** A quiet mark that leads or trails with an icon, sized to the caption. */
const ICON_MARK = cn(QUIET_MARK, "inline-flex items-center gap-0.5 [&_svg]:size-3 [&_svg]:shrink-0");

/** A branch heading's text: which kind of row it is, so a palette hue. */
export const BRANCH_TEXT = "text-(--hue-blue)";

export function implementationsLabel(n: number): string {
  return `${n} ${n === 1 ? "implementation" : "implementations"}`;
}

/** The turn into an override: before an implementation row, and before "N implementations". */
export function ImplArrow() {
  return <CornerDownRight aria-hidden className="size-3 shrink-0 text-text-tertiary" />;
}

/** "N implementations" after a call into an abstract or overridden method. */
export function ImplementationsMark({ n }: { n: number }) {
  return (
    <span className={ICON_MARK}>
      <CornerDownRight aria-hidden />
      {implementationsLabel(n)}
    </span>
  );
}

/** A row drawn elsewhere: "shown above" (expanded higher up) or "recursion" (open on this path). */
export function RefMark({ reason }: { reason: "shown-above" | "recursion" }) {
  return reason === "recursion" ? (
    <span className={ICON_MARK}>
      <Repeat aria-hidden />
      recursion
    </span>
  ) : (
    <span className={ICON_MARK}>
      shown above
      <ArrowUp aria-hidden />
    </span>
  );
}
