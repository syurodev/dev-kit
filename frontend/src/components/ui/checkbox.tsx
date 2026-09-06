"use client"

import { Checkbox as CheckboxPrimitive } from "radix-ui"
import * as React from "react"
import { Check } from "lucide-react"

import { cn } from "@/lib/utils"

/**
 * Checkbox (Figma 28:78) — 4 states (Unchecked/Checked/Focus/Disabled).
 *
 * Row: 220×32, gap 8.
 * Control: 18×18, r=6 (rounded-sm).
 * Shared form surface recipe:
 * - Unchecked / Focus / Disabled: bg-white/6 border border-status-info/16
 * - Focus: border-(--stroke-1-5) border-status-info (1.5px)
 * - Checked: fill #c6ff34 (bg-action-primary) but the hairline stays
 *   #b2d5e5 @16% — Figma does not switch the border to lime — plus a 12×12
 *   check glyph stroked #020202 at 1.5px
 *
 * Label: text-body-13 text-text-primary (#f0f1eb), dims to text-text-tertiary (#8b8e8a) when disabled.
 */
export interface CheckboxProps
  extends React.ComponentProps<typeof CheckboxPrimitive.Root> {
  label?: React.ReactNode
}

const Checkbox = React.forwardRef<
  React.ComponentRef<typeof CheckboxPrimitive.Root>,
  CheckboxProps
>(({ className, label, id, disabled, ...props }, ref) => {
  const generatedId = React.useId()
  const checkboxId = id || (label ? generatedId : undefined)

  const control = (
    <CheckboxPrimitive.Root
      ref={ref}
      id={checkboxId}
      disabled={disabled}
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-[18px] shrink-0 items-center justify-center rounded-sm border border-status-info/16 bg-white/6 transition-colors outline-none",
        "focus-visible:border-(--stroke-1-5) focus-visible:border-status-info focus-visible:outline-none focus-visible:ring-0",
        "disabled:cursor-not-allowed disabled:text-text-tertiary disabled:opacity-50",
        "data-checked:bg-action-primary data-checked:text-action-primary-foreground",
        "aria-invalid:border-status-danger",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current"
      >
        <Check className="size-(--icon-12)" strokeWidth={2} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )

  if (!label) {
    return control
  }

  return (
    <label
      htmlFor={checkboxId}
      data-slot="checkbox-row"
      className={cn(
        "inline-flex h-8 max-w-[220px] items-center gap-2 select-none cursor-pointer text-body-13 text-text-primary",
        disabled && "cursor-not-allowed text-text-tertiary opacity-50",
      )}
    >
      {control}
      <span>{label}</span>
    </label>
  )
})
Checkbox.displayName = "Checkbox"

export { Checkbox }
