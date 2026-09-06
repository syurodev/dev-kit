import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";

import { cn } from "@/lib/utils";

/**
 * Icon Button (Figma 27:85)
 *
 * Six variants in Figma: Tone (Neutral / Primary) × State (Default / Hover /
 * Disabled), Size=MD only. Neutral is the glass-panel recipe — jet at 55% with
 * a white 10% hairline — not a transparent button; those are exactly
 * --glass-panel-bg / --glass-border. Its icon is text/primary, not secondary.
 *
 * There is no sm, lg, secondary or outline here on purpose. The 28px icon
 * affordance in the titlebar is `Titlebar Action` (674:47115), its own
 * component with its own focus ring, and it arrives in step 4 of the reconcile
 * plan. Folding it in here would duplicate the concept before it is built.
 */
const iconButtonVariants = cva(
  "group/icon-button inline-flex shrink-0 items-center justify-center size-(--control-height-md) rounded-md p-0 transition-all outline-none select-none disabled:pointer-events-none disabled:opacity-38 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-status-info [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-(--icon-16)",
  {
    variants: {
      tone: {
        neutral:
          "bg-(--glass-panel-bg) border border-(--glass-border) text-text-primary hover:bg-(--glass-float-bg)",
        primary:
          "bg-action-primary text-action-primary-foreground hover:bg-(--action-primary-hover) active:bg-(--action-primary-pressed)",
      },
    },
    defaultVariants: {
      tone: "neutral",
    },
  },
);

export interface IconButtonProps
  extends React.ComponentProps<"button">,
    VariantProps<typeof iconButtonVariants> {
  asChild?: boolean;
}

const IconButton = React.forwardRef<HTMLButtonElement, IconButtonProps>(
  (
    {
      className,
      tone = "neutral",
      asChild = false,
      ...props
    },
    ref,
  ) => {
    const Comp = asChild ? Slot.Root : "button";

    return (
      <Comp
        ref={ref}
        data-slot="icon-button"
        data-tone={tone}
        className={cn(iconButtonVariants({ tone, className }))}
        {...props}
      />
    );
  },
);
IconButton.displayName = "IconButton";

export { IconButton, iconButtonVariants };
