import type { ReactNode } from "react";

import { Button, type ButtonProps } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * A button whose icon opens into the button.
 *
 * A component rather than a Button variant, and the reason is structural: the
 * wash that fills the pill on hover is a layer INSIDE the icon, scaled up
 * until it covers the button, and the label's colour flips in step with it.
 * A variant is a class string — it cannot require that its children be these
 * two parts, so a caller who took the variant alone would get a round pill
 * with no animation and nothing to say why.
 *
 * What a variant CAN own it does not own here either: this passes its look as
 * className rather than adding a `craft` variant, because `rounded-full` has
 * to beat the radius `size` sets and cva puts size after variant. className
 * lands last, which is the same reason it is the right place for a one-caller
 * look.
 *
 *     <CraftButton>
 *       <CraftButtonIcon><Plus /></CraftButtonIcon>
 *       <CraftButtonLabel>New connection</CraftButtonLabel>
 *     </CraftButton>
 */
function CraftButton({ children, className, ...rest }: ButtonProps) {
  return (
    <Button
      className={cn(
        // overflow-hidden is what makes the icon's wash stop at the pill's
        // edge; without it the circle keeps going across the page.
        //
        // duration is the app's slow tier, not the 500ms this pattern is
        // usually written with: motion.md asks for brief, and --motion-slow
        // (240ms) is the longest this app moves anything.
        "relative gap-2 overflow-hidden rounded-full border-0 duration-(--motion-slow)",
        "hover:bg-background hover:shadow-md active:translate-y-0",
        "dark:border dark:border-transparent dark:hover:border-primary/30",
        className,
      )}
      {...rest}
    >
      {children}
    </Button>
  );
}

/**
 * The label. It sits above the wash (z-2) and turns to the text colour of the
 * ground the wash brings with it.
 */
function CraftButtonLabel({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "relative z-2 transition-colors duration-(--motion-slow) group-hover/button:text-foreground",
        className,
      )}
    >
      {children}
    </span>
  );
}

/**
 * The icon, and the wash behind it.
 *
 * Two layers in one 20px box: one at -z-1 that scales to fifteen times its
 * size and becomes the button's ground, and one at z-2 that stays a circle and
 * holds the glyph.
 *
 * The colours are the app's guaranteed pairs rather than the ones this pattern
 * ships with. At rest the glyph was `text-primary` on a background-coloured
 * circle — and --primary is --action-primary, which measured 1.05:1 against
 * this app's own surfaces on devkit light while it was being used for the
 * selected-tab hairline. On hover the circle becomes --action-primary, so its
 * glyph is --action-primary-foreground: the one colour the palette promises is
 * legible on it.
 */
function CraftButtonIcon({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <span className={cn("relative z-1 size-5", className)}>
      <span className="bg-background absolute inset-0 -z-1 size-5 rounded-full transition-transform duration-(--motion-slow) group-hover/button:scale-1500" />
      <span className="bg-background text-text-primary group-hover/button:bg-action-primary group-hover/button:text-action-primary-foreground relative z-2 flex size-5 items-center justify-center rounded-full transition-all duration-(--motion-slow)">
        {children}
      </span>
    </span>
  );
}

export { CraftButton, CraftButtonIcon, CraftButtonLabel };
