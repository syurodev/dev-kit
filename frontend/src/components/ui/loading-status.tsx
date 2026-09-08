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
        "flex h-12 w-[320px] items-center gap-3 rounded-[12px] border border-glass-border bg-glass-float pl-[14px] pr-4 backdrop-blur-[12px] shadow-[inset_0px_1px_0px_0px_rgba(255,255,255,0.06),0_6px_10px_0_rgba(0,0,0,0.3)] select-none",
        className
      )}
      {...props}
    >
      <LoaderCircle className="size-4 animate-spin text-icon-secondary shrink-0" strokeWidth={1.5} />
      <span className="text-body-13-medium text-text-secondary truncate">
        {text}
      </span>
    </div>
  )
}

export { LoadingStatus }
