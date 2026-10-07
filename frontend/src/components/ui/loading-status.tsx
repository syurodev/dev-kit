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
      role="status"
      className={cn(
        "flex h-12 w-80 max-w-full items-center gap-3 rounded-lg border border-border-subtle bg-surface-raised pl-3.5 pr-4 select-none",
        className
      )}
      {...props}
    >
      <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none text-icon-secondary shrink-0" strokeWidth={1.5} />
      <span className="text-body-13-medium text-text-secondary truncate">
        {text}
      </span>
    </div>
  )
}

export { LoadingStatus }
