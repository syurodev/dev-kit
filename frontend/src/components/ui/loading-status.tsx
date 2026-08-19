import * as React from "react"
import { LoaderCircle } from "lucide-react"

import { cn } from "@/lib/utils"

export interface LoadingStatusProps extends React.ComponentProps<"div"> {
  text?: string
}

function LoadingStatus({
  text = "Checking vault status…",
  className,
  ...props
}: LoadingStatusProps) {
  return (
    <div
      data-slot="loading-status"
      className={cn(
        "flex h-12 w-fit items-center gap-3 rounded-[12px] bg-surface-raised px-4 border border-border-subtle shadow-sm",
        className
      )}
      {...props}
    >
      <LoaderCircle className="size-4 animate-spin text-status-info" strokeWidth={1.5} />
      <span className="text-[13px] font-medium text-text-secondary">
        {text}
      </span>
    </div>
  )
}

export { LoadingStatus }
