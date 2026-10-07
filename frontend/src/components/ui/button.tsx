import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"
import { motion, useReducedMotion } from "framer-motion"

import { cn } from "@/lib/utils"
import { SPRING_PRESS } from "@/lib/motion"

// Keeping HTML callbacks in controlProps prevents Motion's gesture callback
// names (onDrag/onAnimationStart) from consuming native button events.
const MotionButton = motion.create(React.forwardRef<
  HTMLButtonElement,
  { asChild?: boolean; controlProps: React.ComponentProps<"button">; style?: React.CSSProperties }
>(({ asChild, controlProps, style, ...visualProps }, ref) => {
  const Comp = asChild ? Slot.Root : "button"
  return <Comp {...visualProps} {...controlProps} ref={ref} style={{ ...controlProps.style, ...style }} />
}))

const buttonVariants = cva(
  // CSS owns colours; Motion owns the press transform and can retarget it.
  "group/button motion-control inline-flex shrink-0 items-center justify-center whitespace-nowrap transition-control outline-none select-none disabled:pointer-events-none disabled:opacity-38 aria-invalid:border-status-danger focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-status-info focus-visible:border-status-info [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-action-primary text-action-primary-foreground hover:bg-(--action-primary-hover) active:bg-(--action-primary-pressed)",
        primary:
          "bg-action-primary text-action-primary-foreground hover:bg-(--action-primary-hover) active:bg-(--action-primary-pressed)",
        secondary:
          "bg-(--glass-panel-bg) border border-(--glass-border) text-text-primary hover:border-border hover:bg-state-soft active:bg-(--glass-panel-bg)",
        outline:
          "border border-border bg-transparent text-text-primary hover:bg-state-soft hover:border-border active:bg-surface-base",
        ghost:
          "bg-transparent text-text-secondary hover:bg-surface-raised hover:text-text-primary active:bg-surface-base",
        // A ghost for chrome that has no material of its own — the title bar
        // and the tab strip share the window's fill rather than painting one,
        // so ghost's `hover:bg-surface-raised` puts a solid card colour on a
        // translucent bar. --state-soft is the state layer, defined per mode
        // for exactly this (white where dark lifts, ink where light recesses).
        //
        // no-drag comes with it: a control sitting in the title bar's drag
        // region that hands its mousedown to the window move is a control you
        // cannot click, and every call site had to remember that by hand.
        titlebar:
          "bg-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary active:bg-state-soft cursor-pointer wails-no-drag",
        // The float tier as a control: see .glass-button in index.css, which
        // holds the wash, the travelling ring and the reduced-transparency and
        // reduced-motion answers. Only what a variant may own is here.
        //
        // `relative` because the ring is an absolutely-positioned ::before, and
        // text-text-primary rather than white: white is right on the dark
        // photograph this pattern comes from and wrong on five of this app's
        // ten palettes, where the glass lands light. --text-primary is the one
        // floored against every ground in themeContrast.test.ts.
        glass: "glass-button relative text-text-primary",
        destructive:
          "bg-(--feedback-danger-soft) text-status-danger border border-(--border-danger) hover:bg-(--feedback-danger-hover) active:bg-(--feedback-danger-pressed)",
        // The SOLID danger, for a control that has to stay the loudest thing
        // in its row while it says "this stops something" — `destructive` is
        // the tinted box, and measured 1.02:1 against the card on a dark theme:
        // correct for a delete row in a menu, far too quiet for the one live
        // button in a toolbar.
        //
        // The glyph is --surface-base rather than a foreground token of its
        // own, because --status-danger is already inverted against the theme:
        // a light rose on dark, a deep red on light. Measured on its own fill
        // across all ten palettes: 5.26:1 at worst.
        danger:
          "bg-status-danger text-surface-base hover:bg-(--status-danger-hover) active:bg-(--status-danger-pressed)",
        link: "text-status-info underline-offset-4 hover:underline",
      },
      size: {
        xs: "h-(--control-height-sm) gap-1.5 rounded-md px-3 text-label-12 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        sm: "h-(--control-height-sm) gap-1.5 rounded-md px-3 text-label-12 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        default: "h-(--control-height-md) gap-2 rounded-md px-4 text-body-14-medium",
        md: "h-(--control-height-md) gap-2 rounded-md px-4 text-body-14-medium",
        lg: "h-(--control-height-xl) gap-2 rounded-control px-4 text-body-14-medium",
        "icon-xs": "size-(--control-height-xs) rounded-sm p-0 [&_svg:not([class*='size-'])]:size-(--icon-12)",
        "icon-sm": "size-(--control-height-sm) rounded-md p-0 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        icon: "size-(--control-height-md) rounded-md p-0 [&_svg:not([class*='size-'])]:size-(--icon-16)",
        "icon-md": "size-(--control-height-md) rounded-md p-0 [&_svg:not([class*='size-'])]:size-(--icon-16)",
        "icon-lg": "size-(--control-height-xl) rounded-control p-0 [&_svg:not([class*='size-'])]:size-(--icon-20)",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

export interface ButtonProps
  extends React.ComponentProps<"button">,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ref,
  ...props
}: ButtonProps) {
  const reduced = useReducedMotion()

  return (
    <MotionButton
      ref={ref}
      asChild={asChild}
      data-slot="button"
      data-variant={variant}
      data-size={size}
      whileTap={reduced || props.disabled || variant === "link" ? undefined : { scale: 0.98 }}
      transition={SPRING_PRESS}
      controlProps={{ ...props, className: cn(buttonVariants({ variant, size, className })) }}
    />
  )
}

export { Button, buttonVariants }
