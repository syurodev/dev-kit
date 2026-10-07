import * as React from "react"
import { RadioGroup as RadioGroupPrimitive } from "radix-ui"
import { motion, useReducedMotion } from "framer-motion"

import { cn } from "@/lib/utils"
import { SPRING_PRESS } from "@/lib/motion"
import { CONTROL_FOCUS, CONTROL_SURFACE } from "./control-styles"

const MotionRadioItem = motion.create(React.forwardRef<
  React.ComponentRef<typeof RadioGroupPrimitive.Item>,
  { controlProps: React.ComponentProps<typeof RadioGroupPrimitive.Item>; style?: React.CSSProperties }
>(({ controlProps, style, ...visualProps }, ref) => (
  <RadioGroupPrimitive.Item {...visualProps} {...controlProps} ref={ref} style={{ ...controlProps.style, ...style }} />
)))

function RadioGroup({
  className,
  ...props
}: React.ComponentProps<typeof RadioGroupPrimitive.Root>) {
  return (
    <RadioGroupPrimitive.Root
      data-slot="radio-group"
      className={cn("grid w-full gap-2", className)}
      {...props}
    />
  )
}

function RadioGroupItem({
  className,
  ref,
  disabled,
  ...props
}: React.ComponentProps<typeof RadioGroupPrimitive.Item>) {
  const reduced = useReducedMotion()
  return (
    <MotionRadioItem
      ref={ref}
      data-slot="radio-group-item"
      whileTap={reduced || disabled ? undefined : { scale: 0.92 }}
      transition={SPRING_PRESS}
      controlProps={{
        ...props,
        disabled,
        className: cn(
          "group/radio-group-item peer motion-control relative flex aspect-square size-4 shrink-0 rounded-full outline-none after:absolute after:-inset-x-3 after:-inset-y-2 disabled:cursor-not-allowed disabled:opacity-50 data-checked:border-action-primary data-checked:bg-action-primary data-checked:text-action-primary-foreground",
          CONTROL_SURFACE,
          CONTROL_FOCUS,
          className
        ),
        children: (
          <RadioGroupPrimitive.Indicator
            data-slot="radio-group-indicator"
            className="flex size-4 items-center justify-center animate-in fade-in-0 zoom-in-calm duration-(--motion-fast) ease-(--ease-out-strong)"
          >
            <span className="absolute top-1/2 left-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-action-primary-foreground" />
          </RadioGroupPrimitive.Indicator>
        ),
      }}
    />
  )
}

export { RadioGroup, RadioGroupItem }
