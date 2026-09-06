import * as React from "react"
import { Tooltip as TooltipPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function TooltipProvider({
  delayDuration = 100,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  )
}

function Tooltip({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />
}

function TooltipTrigger({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Trigger>) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
}

function TooltipContent({
  className,
  sideOffset = 0,
  children,
  portal = true,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content> & { portal?: boolean }) {
  const content = (
    <TooltipPrimitive.Content
      data-slot="tooltip-content"
      sideOffset={sideOffset}
      className={cn(
        "z-50 inline-flex h-8 items-center gap-1.5 rounded-md border border-[rgba(255,255,255,0.1)] bg-surface-glass-floating px-3 text-label-12 text-text-primary shadow-xl backdrop-blur-md select-none",
        "data-[state=delayed-open]:animate-in data-[state=delayed-open]:fade-in-0 data-[state=delayed-open]:zoom-in-95",
        "data-state-closed:animate-out data-state-closed:fade-out-0 data-state-closed:zoom-out-95",
        className,
      )}
      {...props}
    >
      {children}
    </TooltipPrimitive.Content>
  )

  if (!portal) {
    return content
  }

  return <TooltipPrimitive.Portal>{content}</TooltipPrimitive.Portal>
}

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger }
