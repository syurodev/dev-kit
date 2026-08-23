import * as React from "react"

import { cn } from "@/lib/utils"

export interface InputProps extends React.ComponentProps<"input"> {
  inputSize?: "sm" | "default" | "lg"
}

function Input({ className, type, inputSize = "default", ...props }: InputProps) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "w-full min-w-0 rounded-[8px] border border-border bg-foreground/5 px-3 text-[14px] text-text-primary transition-colors outline-none",
        "placeholder:text-text-tertiary",
        "focus-visible:border-[1.5px] focus-visible:border-status-info focus-visible:outline-none focus-visible:ring-0",
        "disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-surface-raised disabled:opacity-38 disabled:text-text-tertiary",
        "aria-invalid:border-status-danger aria-invalid:ring-0",
        inputSize === "sm" && "h-8 text-xs px-2.5",
        inputSize === "default" && "h-10 text-[14px]",
        inputSize === "lg" && "h-11 text-base px-3.5",
        className
      )}
      {...props}
    />
  )
}

export { Input }
