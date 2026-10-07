import * as React from "react"

import { cn } from "@/lib/utils"
import { CONTROL_SURFACE, CONTROL_FOCUS } from "./control-styles"

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-16 w-full rounded-md px-3 py-2 text-body-14 outline-none placeholder:text-text-tertiary disabled:cursor-not-allowed disabled:opacity-50",
        CONTROL_SURFACE,
        CONTROL_FOCUS,
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
