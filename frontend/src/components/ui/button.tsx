import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center font-medium whitespace-nowrap transition-all outline-none select-none disabled:pointer-events-none disabled:opacity-38 aria-invalid:border-status-danger focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-status-info focus-visible:border-status-info [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-action-primary text-action-primary-foreground hover:brightness-110 active:brightness-95",
        primary:
          "bg-action-primary text-action-primary-foreground hover:brightness-110 active:brightness-95",
        secondary:
          "bg-surface-raised border border-border text-text-primary hover:border-status-info hover:bg-surface-raised/90 active:bg-surface-base",
        outline:
          "border border-border bg-transparent text-text-primary hover:bg-surface-raised hover:border-status-info active:bg-surface-base",
        ghost:
          "bg-transparent text-text-secondary hover:bg-surface-raised hover:text-text-primary active:bg-surface-base",
        destructive:
          "bg-status-danger/15 text-status-danger border border-status-danger/30 hover:bg-status-danger/25 active:bg-status-danger/30",
        link: "text-status-info underline-offset-4 hover:underline",
      },
      size: {
        xs: "h-7 gap-1.5 rounded-[8px] px-2.5 text-xs [&_svg:not([class*='size-'])]:size-3.5",
        sm: "h-7 gap-1.5 rounded-[8px] px-3 text-xs [&_svg:not([class*='size-'])]:size-3.5",
        default: "h-8 gap-2 rounded-[10px] px-4 text-[13px]",
        md: "h-8 gap-2 rounded-[10px] px-4 text-[13px]",
        lg: "h-10 gap-2 rounded-[10px] px-4.5 text-[14px]",
        "icon-xs": "size-6 rounded-[6px] p-0 [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-7 rounded-[8px] p-0 [&_svg:not([class*='size-'])]:size-3.5",
        icon: "size-8 rounded-[8px] p-0 [&_svg:not([class*='size-'])]:size-4",
        "icon-md": "size-8 rounded-[8px] p-0 [&_svg:not([class*='size-'])]:size-4",
        "icon-lg": "size-10 rounded-[10px] p-0 [&_svg:not([class*='size-'])]:size-5",
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
  ...props
}: ButtonProps) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
