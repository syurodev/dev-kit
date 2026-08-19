import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const alertVariants = cva(
  "group/alert relative flex w-full gap-3 rounded-[10px] border px-4 py-3 text-left transition-colors",
  {
    variants: {
      variant: {
        default:
          "border-border bg-surface-raised text-text-primary [&_svg]:text-text-secondary",
        info:
          "border-status-info/70 bg-status-info/10 text-text-primary [&_svg]:text-status-info",
        success:
          "border-status-success/70 bg-status-success/10 text-text-primary [&_svg]:text-status-success",
        warning:
          "border-status-warning/70 bg-status-warning/10 text-text-primary [&_svg]:text-status-warning",
        destructive:
          "border-status-danger bg-status-danger/10 text-text-primary [&_svg]:text-status-danger",
        error:
          "border-status-danger bg-status-danger/10 text-text-primary [&_svg]:text-status-danger",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface AlertProps
  extends React.ComponentProps<"div">,
    VariantProps<typeof alertVariants> {}

function Alert({ className, variant = "default", ...props }: AlertProps) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="alert-title"
      className={cn(
        "text-[13px] font-semibold leading-tight text-text-primary",
        className
      )}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="alert-description"
      className={cn(
        "text-[12px] font-normal leading-relaxed text-text-secondary mt-0.5",
        className
      )}
      {...props}
    />
  )
}

function AlertContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-content"
      className={cn("flex flex-1 flex-col gap-0.5 min-w-0", className)}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription, AlertContent, alertVariants }
