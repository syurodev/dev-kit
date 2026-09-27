"use client"

import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

/**
 * Switch (Figma 570:59) — 3 states (Off/On/Disabled).
 *
 * Track: 32×18, r=full (rounded-full).
 * Off: fill bg-white/6 (#ffffff @6%). Figma gives the track NO stroke in any
 * state — it is the one form control that breaks the shared glass recipe.
 *      thumb: 16×16, bg-text-primary (#f0f1eb).
 * On:  fill bg-action-primary (#c6ff34), thumb #020202.
 *      thumb: 16×16, bg-palette-onyx (#020202).
 * Disabled: looks like Off, dimmed.
 */
function Switch({
  className,
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        "peer group/switch relative inline-flex h-4.5 w-8 shrink-0 items-center rounded-full border border-transparent bg-white/6 transition-all outline-none",
        "focus-visible:border-(--stroke-1-5) focus-visible:border-status-info focus-visible:outline-none focus-visible:ring-0",
        "data-checked:bg-action-primary",
        "disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className={cn(
          "pointer-events-none block size-4 rounded-full bg-text-primary shadow-sm transition-transform",
          "translate-x-0 group-data-[state=checked]/switch:translate-x-3.5 group-data-[state=checked]/switch:bg-palette-onyx",
        )}
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
