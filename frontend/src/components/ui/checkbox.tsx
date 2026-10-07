"use client"

import { Checkbox as CheckboxPrimitive } from "radix-ui"
import * as React from "react"
import { motion, useReducedMotion } from "framer-motion"

import { cn } from "@/lib/utils"
import { CONTROL_SURFACE, CONTROL_FOCUS } from "./control-styles"
import { SPRING_PRESS } from "@/lib/motion"

// Native DOM callbacks stay inside controlProps: Motion's onDrag and
// onAnimationStart have different signatures from the HTML events.
const MotionCheckboxRoot = motion.create(React.forwardRef<
  React.ComponentRef<typeof CheckboxPrimitive.Root>,
  { controlProps: React.ComponentProps<typeof CheckboxPrimitive.Root>; style?: React.CSSProperties }
>(({ controlProps, style, ...visualProps }, ref) => (
  <CheckboxPrimitive.Root {...visualProps} {...controlProps} ref={ref} style={{ ...controlProps.style, ...style }} />
)))

/** Radix owns checked/form state. Motion only supplies tactile press feedback. */
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
  const reduce = useReducedMotion()

  const control = (
    <MotionCheckboxRoot
      ref={ref}
      data-slot="checkbox"
      whileTap={reduce || disabled ? undefined : { scale: 0.92 }}
      transition={SPRING_PRESS}
      controlProps={{
        ...props,
        id: checkboxId,
        disabled,
        className: cn(
          "peer motion-control relative flex size-4.5 shrink-0 items-center justify-center rounded-sm outline-none",
          CONTROL_SURFACE,
          CONTROL_FOCUS,
          "disabled:cursor-not-allowed disabled:text-text-tertiary disabled:opacity-50",
          "data-checked:border-action-primary data-checked:bg-action-primary data-checked:text-action-primary-foreground",
          "data-[state=indeterminate]:border-action-primary data-[state=indeterminate]:bg-action-primary data-[state=indeterminate]:text-action-primary-foreground",
          className,
        ),
        children: (
          <CheckboxPrimitive.Indicator
            forceMount
            data-slot="checkbox-indicator"
            className="checkbox-mark group/check grid place-content-center text-current data-[state=unchecked]:opacity-0 data-[state=unchecked]:scale-50"
          >
            <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={3} strokeLinecap="round" strokeLinejoin="round" className="size-(--icon-12)">
              <path pathLength={1} d="M5 13l4 4L19 7" className="checkbox-check-path group-data-[state=indeterminate]/check:hidden" />
              <path pathLength={1} d="M6 12h12" className="checkbox-mixed-path hidden group-data-[state=indeterminate]/check:block" />
            </svg>
          </CheckboxPrimitive.Indicator>
        ),
      }}
    />
  )

  if (!label) {
    return control
  }

  return (
    <label
      htmlFor={checkboxId}
      data-slot="checkbox-row"
      className={cn(
        "inline-flex h-8 max-w-55 items-center gap-2 select-none cursor-pointer text-body-13 text-text-primary",
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
