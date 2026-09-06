"use client"

import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Tabs as TabsPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

function Tabs({
  className,
  orientation = "horizontal",
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      data-orientation={orientation}
      className={cn(
        "group/tabs flex gap-2 data-horizontal:flex-col",
        className
      )}
      {...props}
    />
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
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        // Figma `Tabs / Trigger` 317:380 sets label AND count to 14 Medium
        // ("text-sm font-medium" in the node description). Step 2 moved this off
        // caption-11 but landed on 13; the real value is 14, which is why
        // `text-title-14` / `text-body-14-medium` were added to the scale.
        "group/tabs-trigger relative inline-flex h-6 items-center justify-center gap-1.5 rounded-sm px-2 py-0.5 text-body-14-medium whitespace-nowrap text-text-tertiary transition-all",
        "hover:text-text-primary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-status-info",
        "disabled:pointer-events-none disabled:opacity-50",
        "group-data-[variant=default]/tabs-list:data-active:bg-white/10 group-data-[variant=default]/tabs-list:data-active:border group-data-[variant=default]/tabs-list:data-active:border-white/8 group-data-[variant=default]/tabs-list:data-active:text-text-primary",
        "group-data-[variant=line]/tabs-list:bg-transparent group-data-[variant=line]/tabs-list:data-active:bg-transparent group-data-[variant=line]/tabs-list:data-active:text-text-primary",
        "after:absolute after:bottom-0 after:inset-x-0 after:h-0.5 after:bg-text-primary after:opacity-0 after:transition-opacity group-data-[variant=line]/tabs-list:data-active:after:opacity-100",
        "[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-(--icon-14)",
        className,
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn("flex-1 text-body-13 text-text-primary outline-none", className)}
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
