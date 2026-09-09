"use client"

import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { motion } from "framer-motion"
import { Tabs as TabsPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

// ponytail: mirrors --motion-fast in index.css, same trade as tree-view.tsx.
const CONTENT_OUT_MS = 120

/**
 * What the triggers and panels need that Radix keeps to itself: the selected
 * value (so the one active trigger can host the shared sliding indicator) and
 * a lagging `shown` value, which is how a switch becomes "old panel fades,
 * then the new one fades in" instead of a cut — the old panel stays mounted
 * for CONTENT_OUT_MS after Radix has already moved on, and the new one is
 * not mounted until then, so only one panel ever takes up space.
 */
const TabsValueContext = React.createContext<{
  value?: string
  shown?: string
  id: string
}>({ id: "" })

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
  const [shown, setShown] = React.useState(current)
  React.useEffect(() => {
    if (current === shown) return
    const lag = window.matchMedia("(prefers-reduced-motion: reduce)").matches
      ? 0
      : CONTENT_OUT_MS
    const t = setTimeout(() => setShown(current), lag)
    return () => clearTimeout(t)
  }, [current, shown])
  const id = React.useId()
  const ctx = React.useMemo(() => ({ value: current, shown, id }), [current, shown, id])
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
        className={cn(
          "group/tabs flex gap-2 data-horizontal:flex-col",
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
        default: "h-8 rounded-md bg-black/25 p-1 gap-1",
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
            "group-data-[variant=default]/tabs-list:inset-0 group-data-[variant=default]/tabs-list:border group-data-[variant=default]/tabs-list:border-white/8 group-data-[variant=default]/tabs-list:bg-white/10",
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
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  const ctx = React.useContext(TabsValueContext)
  if (ctx.shown !== value) return null
  // forceMount: while this panel is the one leaving, Radix already considers
  // another value selected and would hide it before its fade could play.
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      value={value}
      forceMount
      data-leaving={ctx.value === value ? undefined : ""}
      className={cn(
        "tabs-content-swap flex-1 text-body-13 text-text-primary outline-none",
        className,
      )}
      {...props}
    />
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
