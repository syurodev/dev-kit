"use client"

import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { AnimatePresence, motion, useReducedMotion } from "framer-motion"
import { Tabs as TabsPrimitive } from "radix-ui"

import { noMotion, slideTransition, slideVariants, type SlideCustom } from "@/lib/motion"
import { cn } from "@/lib/utils"

/**
 * What the triggers and panels need that Radix keeps to itself: the selected
 * value (so the one active trigger can host the shared sliding indicator), the
 * direction the selection just moved in (so panels slide the way the strip
 * reads — pick a tab to the right, content arrives from the right), and a
 * register so triggers can report the strip's order. Radix never exposes it.
 */
const TabsValueContext = React.createContext<{
  value?: string
  dir: number
  id: string
  reduced: boolean
  register: (value: string) => void
}>({ id: "", dir: 1, reduced: false, register: () => {} })

function Tabs({
  className,
  orientation = "horizontal",
  value,
  defaultValue,
  onValueChange,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  const [inner, setInner] = React.useState(defaultValue)
  const current = value ?? inner
  const id = React.useId()
  const reduced = useReducedMotion() ?? false
  // Strip order by trigger mount order; direction is where `current` sits
  // relative to the value it replaced. Read during render, settled in an
  // effect, so a controlled change and a click compute the same way.
  const order = React.useRef<string[]>([])
  const previous = React.useRef(current)
  const dir =
    order.current.indexOf(current ?? "") < order.current.indexOf(previous.current ?? "") ? -1 : 1
  React.useEffect(() => {
    previous.current = current
  }, [current])
  const register = React.useCallback((v: string) => {
    if (!order.current.includes(v)) order.current.push(v)
  }, [])
  const ctx = React.useMemo(
    () => ({ value: current, dir, id, reduced, register }),
    [current, dir, id, reduced, register],
  )
  return (
    <TabsValueContext.Provider value={ctx}>
      <TabsPrimitive.Root
        data-slot="tabs"
        data-orientation={orientation}
        value={current}
        onValueChange={(next) => {
          setInner(next)
          onValueChange?.(next)
        }}
        // relative + x-clip: the leaving panel goes absolute against this box
        // and both panels travel past its sides. clip, not hidden, so nothing
        // vertical (focus rings, an overflowing row) gets cut.
        className={cn(
          "group/tabs relative flex gap-2 overflow-x-clip data-horizontal:flex-col",
          className
        )}
        {...props}
      />
    </TabsValueContext.Provider>
  )
}

const tabsListVariants = cva(
  "group/tabs-list inline-flex w-fit items-center justify-center text-text-tertiary group-data-horizontal/tabs:h-8 group-data-vertical/tabs:h-fit group-data-vertical/tabs:flex-col",
  {
    variants: {
      variant: {
        // A recessed track and, on it, a chip the colour of the page. Both
        // sides used to be raw alphas — black/25 under white/10 — which is a
        // dark-only idea: composited on a light surface the track went grey
        // and the chip landed 6 values away from it, so the selected tab was
        // invisible in light mode. Tokens flip per mode; 25% of black does
        // not.
        default: "h-8 rounded-md bg-(--segment-track) p-1 gap-1",
        line: "gap-1 bg-transparent p-0 rounded-none",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
)

function TabsList({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List> &
  VariantProps<typeof tabsListVariants>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      data-variant={variant}
      className={cn(tabsListVariants({ variant }), className)}
      {...props}
    />
  )
}

function TabsTrigger({
  className,
  value,
  children,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  const ctx = React.useContext(TabsValueContext)
  const { register } = ctx
  React.useLayoutEffect(() => register(value), [register, value])
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      value={value}
      className={cn(
        // Figma `Tabs / Trigger` 317:380 sets label AND count to 14 Medium
        // ("text-sm font-medium" in the node description). Step 2 moved this off
        // caption-11 but landed on 13; the real value is 14, which is why
        // `text-title-14` / `text-body-14-medium` were added to the scale.
        "group/tabs-trigger relative inline-flex h-6 items-center justify-center gap-1.5 rounded-sm px-2 py-0.5 text-body-14-medium whitespace-nowrap text-text-tertiary transition-all",
        "hover:text-text-primary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-status-info",
        "disabled:pointer-events-none disabled:opacity-50",
        "data-active:text-text-primary",
        "[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        className,
      )}
      {...props}
    >
      {/* One indicator per Tabs, shared by layoutId: it slides from the old
          trigger to the new one instead of vanishing here and appearing
          there. A pill under the default variant, a hairline under `line`. */}
      {ctx.value === value ? (
        <motion.span
          aria-hidden
          data-slot="tabs-indicator"
          layoutId={`${ctx.id}-indicator`}
          transition={{ duration: 0.18, ease: [0.23, 1, 0.32, 1] }}
          className={cn(
            "absolute rounded-sm",
            "group-data-[variant=default]/tabs-list:inset-0 group-data-[variant=default]/tabs-list:border group-data-[variant=default]/tabs-list:border-border-default group-data-[variant=default]/tabs-list:bg-(--segment-chip)",
            "group-data-[variant=line]/tabs-list:inset-x-0 group-data-[variant=line]/tabs-list:bottom-0 group-data-[variant=line]/tabs-list:h-0.5 group-data-[variant=line]/tabs-list:bg-text-primary",
          )}
        />
      ) : null}
      <span className="relative inline-flex items-center gap-1.5">{children}</span>
    </TabsPrimitive.Trigger>
  )
}

function TabsContent({
  className,
  value,
  children,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  const ctx = React.useContext(TabsValueContext)
  const panel = React.useRef<HTMLDivElement>(null)
  // Measured on the way out: while this panel is leaving, `panel` still holds
  // its node, so the exit variant can pin it exactly where it was.
  const custom: SlideCustom = {
    dir: ctx.dir,
    top: panel.current?.offsetTop ?? 0,
    height: panel.current?.offsetHeight ?? 0,
  }
  // forceMount: while this panel is the one leaving, Radix already considers
  // another value selected and would hide it before its slide could play.
  return (
    <AnimatePresence initial={false} custom={custom}>
      {ctx.value === value ? (
        <TabsPrimitive.Content
          key={value}
          data-slot="tabs-content"
          value={value}
          forceMount
          asChild
          {...props}
        >
          <motion.div
            ref={panel}
            custom={custom}
            variants={slideVariants}
            initial="enter"
            animate="center"
            exit="exit"
            transition={ctx.reduced ? noMotion : slideTransition}
            className={cn("flex-1 text-body-13 text-text-primary outline-none", className)}
          >
            {children}
          </motion.div>
        </TabsPrimitive.Content>
      ) : null}
    </AnimatePresence>
  )
}

function TabsCount({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="tabs-count"
      className={cn(
        "inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1",
        "bg-white/6 text-body-14-medium tabular-nums text-text-tertiary",
        className,
      )}
      {...props}
    />
  )
}

export { Tabs, TabsList, TabsTrigger, TabsCount, TabsContent, tabsListVariants }
