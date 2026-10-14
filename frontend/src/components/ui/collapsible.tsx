"use client"

import { Collapsible as CollapsiblePrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function Collapsible({
  ...props
}: React.ComponentProps<typeof CollapsiblePrimitive.Root>) {
  return <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
}

function CollapsibleTrigger({
  ...props
}: React.ComponentProps<typeof CollapsiblePrimitive.CollapsibleTrigger>) {
  return (
    <CollapsiblePrimitive.CollapsibleTrigger
      data-slot="collapsible-trigger"
      {...props}
    />
  )
}

/**
 * What the trigger shows and hides, opening and closing by height.
 *
 * Radix measures the content into --radix-collapsible-content-height and keeps
 * it mounted until the closing animation ends; tw-animate's
 * collapsible-down/up move between 0 and that height, and index.css adds a fade
 * to the same animation. Opens over --motion-slow, shuts over --motion-base
 * (shutting should be quicker than opening), --ease-out-strong both ways: the
 * tokens AnimatedCollapse uses, so the two read as one gesture.
 *
 * Content that is open when it mounts does not animate (Radix), nothing is
 * rendered while closed, and under Reduce Motion it snaps. The reduce rule
 * carries the same data-open / data-closed variant as the animation: a bare
 * motion-reduce:animate-none is emitted earlier in the stylesheet than
 * data-open:animate-*, at the same specificity, and loses. Spacing belongs
 * inside the content, not on it: padding cannot shrink below itself, so a
 * padded content would snap its last few pixels shut. `animated={false}` is
 * the plain shadcn element.
 */
function CollapsibleContent({
  className,
  animated = true,
  ...props
}: React.ComponentProps<typeof CollapsiblePrimitive.CollapsibleContent> & {
  animated?: boolean
}) {
  return (
    <CollapsiblePrimitive.CollapsibleContent
      data-slot="collapsible-content"
      className={cn(
        animated &&
          "overflow-hidden ease-(--ease-out-strong) data-open:animate-collapsible-down data-open:duration-(--motion-slow) data-closed:animate-collapsible-up data-closed:duration-(--motion-base) data-open:motion-reduce:animate-none data-closed:motion-reduce:animate-none",
        className
      )}
      {...props}
    />
  )
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent }
