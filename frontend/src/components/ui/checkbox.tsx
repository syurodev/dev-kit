"use client"

import { Checkbox as CheckboxPrimitive } from "radix-ui"
import * as React from "react"
import { Check } from "lucide-react"

import { cn } from "@/lib/utils"

function Checkbox({
  className,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-[18px] shrink-0 items-center justify-center rounded-[6px] border border-border bg-surface-base transition-colors outline-none",
        "focus-visible:border-[1.5px] focus-visible:border-status-info focus-visible:outline-none focus-visible:ring-0",
        "disabled:cursor-not-allowed disabled:opacity-45",
        "data-checked:bg-action-primary data-checked:border-border data-checked:text-action-primary-foreground",
        "aria-invalid:border-status-danger",
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current"
      >
        <Check className="size-3 stroke-[2.5]" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
