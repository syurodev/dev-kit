import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Toggle as TogglePrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

const toggleVariants = cva(
  "group/toggle inline-flex items-center justify-center gap-1 rounded-control text-body-13-medium whitespace-nowrap transition-control outline-none hover:bg-state-soft hover:text-foreground focus-visible:ring-2 focus-visible:ring-border-focus/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-status-danger aria-invalid:ring-status-danger/15 aria-pressed:bg-state-soft data-[state=on]:bg-state-soft [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-transparent",
        outline:
          "border border-border-subtle bg-surface-well hover:bg-state-soft",
      },
      size: {
        default:
          "h-(--control-height-md) min-w-(--control-height-md) px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
        sm: "h-(--control-height-sm) min-w-(--control-height-sm) rounded-md px-2.5 text-label-12 has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        lg: "h-(--control-height-lg) min-w-(--control-height-lg) px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Toggle({
  className,
  variant = "default",
  size = "default",
  ...props
}: React.ComponentProps<typeof TogglePrimitive.Root> &
  VariantProps<typeof toggleVariants>) {
  return (
    <TogglePrimitive.Root
      data-slot="toggle"
      className={cn(toggleVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Toggle, toggleVariants }
