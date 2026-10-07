import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const alertVariants = cva(
  "group/alert relative flex w-full min-h-18 items-center gap-3 rounded-control border px-4 py-3 text-left transition-control [&_svg]:size-4.5 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        info:
          "border-status-info bg-status-info/8 text-text-primary [&_svg]:text-status-info",
        success:
          "border-status-success bg-status-success/12 text-text-primary [&_svg]:text-status-success",
        warning:
          "border-status-warning bg-status-warning/10 text-text-primary [&_svg]:text-status-warning",
        error:
          "border-status-danger bg-status-danger/12 text-text-primary [&_svg]:text-status-danger",
      },
    },
    defaultVariants: {
      variant: "info",
    },
  }
)

export interface AlertProps
  extends React.ComponentProps<"div">,
    VariantProps<typeof alertVariants> {}

function Alert({ className, variant = "info", ...props }: AlertProps) {
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
        "text-title-13 text-text-primary",
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
        "text-body-12 text-text-secondary",
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
      className={cn("flex flex-1 flex-col gap-1 min-w-0", className)}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription, AlertContent, alertVariants }
