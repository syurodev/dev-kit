"use client"

import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"
import { motion, useReducedMotion } from "framer-motion"

import { cn } from "@/lib/utils"
import { noMotion, SPRING_LAYOUT } from "@/lib/motion"

function Switch({
  className,
  checked,
  defaultChecked,
  onCheckedChange,
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  // Radix remains the owner of uncontrolled/form-reset state. This value
  // only gives the thumb a spring target when Radix reports a change.
  const [visualChecked, setVisualChecked] = React.useState(defaultChecked ?? false)
  const active = checked ?? visualChecked
  const reduced = useReducedMotion()
  return (
    <SwitchPrimitive.Root
      checked={checked}
      defaultChecked={defaultChecked}
      onCheckedChange={(next) => {
        setVisualChecked(next)
        onCheckedChange?.(next)
      }}
      data-slot="switch"
      className={cn(
        "peer group/switch relative inline-flex h-4.5 w-8 shrink-0 items-center rounded-full border border-border-subtle bg-surface-well transition-control outline-none",
        "focus-visible:ring-2 focus-visible:ring-border-focus/50 focus-visible:outline-none",
        "data-checked:bg-action-primary",
        "disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        asChild
      >
        <motion.span
        data-slot="switch-thumb"
        initial={false}
        animate={{ x: active ? 14 : 0 }}
        transition={reduced ? noMotion : SPRING_LAYOUT}
        className={cn(
          "pointer-events-none block size-4 rounded-full bg-text-primary shadow-sm transition-colors duration-(--motion-fast) motion-reduce:transition-none",
          "group-data-[state=checked]/switch:bg-action-primary-foreground",
        )}
        />
      </SwitchPrimitive.Thumb>
    </SwitchPrimitive.Root>
  )
}

export { Switch }
