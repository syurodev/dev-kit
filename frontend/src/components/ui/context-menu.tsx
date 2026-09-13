import * as React from "react"
import { ContextMenu as ContextMenuPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"
import { Kbd } from "@/components/ui/kbd"
import { ChevronRightIcon, CheckIcon } from "lucide-react"

function ContextMenu({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Root>) {
  return <ContextMenuPrimitive.Root data-slot="context-menu" {...props} />
}

function ContextMenuTrigger({
  className,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Trigger>) {
  return (
    <ContextMenuPrimitive.Trigger
      data-slot="context-menu-trigger"
      className={cn("select-none", className)}
      {...props}
    />
  )
}

function ContextMenuGroup({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Group>) {
  return (
    <ContextMenuPrimitive.Group data-slot="context-menu-group" {...props} />
  )
}

function ContextMenuPortal({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Portal>) {
  return (
    <ContextMenuPrimitive.Portal data-slot="context-menu-portal" {...props} />
  )
}

function ContextMenuSub({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Sub>) {
  return <ContextMenuPrimitive.Sub data-slot="context-menu-sub" {...props} />
}

function ContextMenuRadioGroup({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.RadioGroup>) {
  return (
    <ContextMenuPrimitive.RadioGroup
      data-slot="context-menu-radio-group"
      {...props}
    />
  )
}

function ContextMenuContent({
  className,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Content> & {
  side?: "top" | "right" | "bottom" | "left"
}) {
  return (
    <ContextMenuPrimitive.Portal>
      <ContextMenuPrimitive.Content
        data-slot="context-menu-content"
        className={cn("z-50 max-h-(--radix-context-menu-content-available-height) min-w-36 origin-(--radix-context-menu-content-transform-origin) overflow-x-hidden overflow-y-auto rounded-lg glass-float p-1 text-popover-foreground duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95", className )}
        {...props}
      />
    </ContextMenuPrimitive.Portal>
  )
}

/**
 * Every row of a menu is the same three columns: an icon, the label, and a
 * shortcut.
 *
 * The icon column is there whether or not the row has an icon. A row without
 * one keeps the slot empty, so every label in the menu starts on the same
 * vertical line — the way IntelliJ's and VS Code's menus read, and the thing
 * a menu of mixed rows got wrong when the icon was just the first child of a
 * flex row. The last column is the shortcut, or a submenu's chevron.
 */
const ROW = cn(
  "relative grid h-7 w-full min-w-[200px] cursor-pointer grid-cols-[var(--icon-14)_minmax(0,1fr)_auto] items-center gap-x-2 rounded-[6px] px-2 text-body-13 outline-none select-none transition-colors",
  "bg-transparent text-text-primary",
  "focus:bg-state-soft hover:bg-state-soft data-open:bg-state-soft",
  "data-disabled:pointer-events-none data-disabled:opacity-50",
  "[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-3.5"
)

function MenuRow({
  icon,
  trailing,
  children,
}: {
  icon?: React.ReactNode
  trailing?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <>
      <span data-slot="context-menu-icon" aria-hidden className="flex size-(--icon-14) items-center justify-center">
        {icon}
      </span>
      <span className="min-w-0 truncate">{children}</span>
      <span className="flex items-center justify-end">{trailing}</span>
    </>
  )
}

/** A shortcut, set in the app's key cap, clear of the label. */
function Shortcut({ keys }: { keys?: string }) {
  return keys ? <Kbd className="ml-4">{keys}</Kbd> : null
}

function ContextMenuItem({
  className,
  variant = "default",
  icon,
  shortcut,
  children,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Item> & {
  variant?: "default" | "destructive"
  /** Optional. The column stays reserved either way. */
  icon?: React.ReactNode
  /** Optional, e.g. "⌘W" — see lib/shortcut for a per-platform label. */
  shortcut?: string
}) {
  return (
    <ContextMenuPrimitive.Item
      data-slot="context-menu-item"
      data-variant={variant}
      className={cn(
        ROW,
        "group/context-menu-item",
        "data-[variant=destructive]:bg-transparent data-[variant=destructive]:text-status-danger",
        "data-[variant=destructive]:focus:bg-[var(--feedback-danger-soft)] data-[variant=destructive]:hover:bg-[var(--feedback-danger-soft)] data-[variant=destructive]:focus:text-status-danger data-[variant=destructive]:hover:text-status-danger",
        "data-[variant=destructive]:*:[svg]:text-status-danger",
        className
      )}
      {...props}
    >
      <MenuRow icon={icon} trailing={<Shortcut keys={shortcut} />}>
        {children}
      </MenuRow>
    </ContextMenuPrimitive.Item>
  )
}

function ContextMenuSubTrigger({
  className,
  icon,
  children,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.SubTrigger> & {
  icon?: React.ReactNode
}) {
  return (
    <ContextMenuPrimitive.SubTrigger
      data-slot="context-menu-sub-trigger"
      className={cn(ROW, className)}
      {...props}
    >
      <MenuRow icon={icon} trailing={<ChevronRightIcon className="text-text-tertiary" />}>
        {children}
      </MenuRow>
    </ContextMenuPrimitive.SubTrigger>
  )
}

function ContextMenuSubContent({
  className,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.SubContent>) {
  return (
    <ContextMenuPrimitive.Portal>
      {/* Portalled, unlike stock shadcn. The parent Content carries
          backdrop-filter for the glass tier, and that makes it a containing
          block for fixed-position descendants — so the submenu stopped
          escaping and the parent's overflow-x-hidden clipped it away
          entirely. */}
      <ContextMenuPrimitive.SubContent
      data-slot="context-menu-sub-content"
      className={cn("z-50 min-w-32 origin-(--radix-context-menu-content-transform-origin) overflow-hidden rounded-lg glass-float p-1 text-popover-foreground duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95", className )}
        {...props}
      />
    </ContextMenuPrimitive.Portal>
  )
}

/** The check sits in the icon column, so a toggled row lines up with the rest. */
function ContextMenuCheckboxItem({
  className,
  children,
  checked,
  shortcut,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.CheckboxItem> & {
  shortcut?: string
}) {
  return (
    <ContextMenuPrimitive.CheckboxItem
      data-slot="context-menu-checkbox-item"
      className={cn(ROW, className)}
      checked={checked}
      {...props}
    >
      <MenuRow
        icon={
          <ContextMenuPrimitive.ItemIndicator>
            <CheckIcon />
          </ContextMenuPrimitive.ItemIndicator>
        }
        trailing={<Shortcut keys={shortcut} />}
      >
        {children}
      </MenuRow>
    </ContextMenuPrimitive.CheckboxItem>
  )
}

function ContextMenuRadioItem({
  className,
  children,
  shortcut,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.RadioItem> & {
  shortcut?: string
}) {
  return (
    <ContextMenuPrimitive.RadioItem
      data-slot="context-menu-radio-item"
      className={cn(ROW, className)}
      {...props}
    >
      <MenuRow
        icon={
          <ContextMenuPrimitive.ItemIndicator>
            <CheckIcon />
          </ContextMenuPrimitive.ItemIndicator>
        }
        trailing={<Shortcut keys={shortcut} />}
      >
        {children}
      </MenuRow>
    </ContextMenuPrimitive.RadioItem>
  )
}

/** A label starts where the rows' labels start: past the icon column. */
function ContextMenuLabel({
  className,
  children,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Label>) {
  return (
    <ContextMenuPrimitive.Label
      data-slot="context-menu-label"
      className={cn(
        "grid grid-cols-[var(--icon-14)_minmax(0,1fr)] gap-x-2 px-2 py-1 text-caption-11-medium text-muted-foreground",
        className
      )}
      {...props}
    >
      <span aria-hidden />
      <span className="min-w-0 truncate">{children}</span>
    </ContextMenuPrimitive.Label>
  )
}

function ContextMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Separator>) {
  return (
    <ContextMenuPrimitive.Separator
      data-slot="context-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-border", className)}
      {...props}
    />
  )
}

export {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuCheckboxItem,
  ContextMenuRadioItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuGroup,
  ContextMenuPortal,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuRadioGroup,
}
