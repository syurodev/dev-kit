import * as React from "react"
import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"
import {
  MENU_LABEL,
  MENU_ROW,
  MENU_ROW_DESTRUCTIVE,
  MenuLabelText,
  MenuRow,
  MenuTrailing,
} from "@/components/ui/menu-row"
import { CheckIcon, ChevronRightIcon } from "lucide-react"

function DropdownMenu({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />
}

function DropdownMenuPortal({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Portal>) {
  return (
    <DropdownMenuPrimitive.Portal data-slot="dropdown-menu-portal" {...props} />
  )
}

function DropdownMenuTrigger({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return (
    <DropdownMenuPrimitive.Trigger
      data-slot="dropdown-menu-trigger"
      {...props}
    />
  )
}

/*
The content is NOT pinned to the trigger's width.

`w-(--radix-dropdown-menu-trigger-width)` is select behaviour — a listbox that
lines up under the field it belongs to. Every menu in this app is an action
menu, and most of them hang off an icon button: the connection row's actions
came out 128px wide (the old min-w-32 floor) while DropdownMenuItem declares
`min-w-[200px]`, so the items overflowed and `overflow-x-hidden` cut them —
"Re-index schen", "Delete connecti". ContextMenuContent never had the binding,
which is why the same list read fine on right-click and clipped on the button.

The floor stays at 32. A floor is not a cap: with no width imposed, the items'
own 200px minimum grows the box, which is exactly how the context menu has
always ended up the right size.
*/
function DropdownMenuContent({
  className,
  align = "start",
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        align={align}
        className={cn("z-50 max-h-(--radix-dropdown-menu-content-available-height) min-w-32 origin-(--radix-dropdown-menu-content-transform-origin) overflow-x-hidden overflow-y-auto rounded-lg glass-float p-1 text-popover-foreground duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:overflow-hidden data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95", className )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

function DropdownMenuGroup({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Group>) {
  return (
    <DropdownMenuPrimitive.Group data-slot="dropdown-menu-group" {...props} />
  )
}

/** A row: icon · label · shortcut or detail — see components/ui/menu-row. */
function DropdownMenuItem({
  className,
  variant = "default",
  icon,
  shortcut,
  detail,
  bare = false,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Item> & {
  variant?: "default" | "destructive"
  /** Optional. The column stays reserved either way. */
  icon?: React.ReactNode
  /** Optional key label, e.g. "⌘N", drawn as a key cap. */
  shortcut?: string
  /** Optional quiet text at the end, for a state rather than a key. */
  detail?: React.ReactNode
  /**
   * Not a row: the item is drawn entirely by `className` and its children —
   * a colour swatch in the editor's colour picker is a menu item, but it is a
   * round button, not a line of a list.
   */
  bare?: boolean
}) {
  if (bare) {
    return (
      <DropdownMenuPrimitive.Item
        data-slot="dropdown-menu-item"
        data-variant={variant}
        className={cn("cursor-pointer outline-none select-none", className)}
        {...props}
      >
        {children}
      </DropdownMenuPrimitive.Item>
    )
  }
  return (
    <DropdownMenuPrimitive.Item
      data-slot="dropdown-menu-item"
      data-variant={variant}
      className={cn(MENU_ROW, "group/dropdown-menu-item", MENU_ROW_DESTRUCTIVE, className)}
      {...props}
    >
      <MenuRow icon={icon} trailing={<MenuTrailing shortcut={shortcut} detail={detail} />}>
        {children}
      </MenuRow>
    </DropdownMenuPrimitive.Item>
  )
}

/** The check sits in the icon column, so a toggled row lines up with the rest. */
function DropdownMenuCheckboxItem({
  className,
  children,
  checked,
  shortcut,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem> & {
  shortcut?: string
}) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      data-slot="dropdown-menu-checkbox-item"
      className={cn(MENU_ROW, className)}
      checked={checked}
      {...props}
    >
      <MenuRow
        icon={
          <DropdownMenuPrimitive.ItemIndicator>
            <CheckIcon />
          </DropdownMenuPrimitive.ItemIndicator>
        }
        trailing={<MenuTrailing shortcut={shortcut} />}
      >
        {children}
      </MenuRow>
    </DropdownMenuPrimitive.CheckboxItem>
  )
}

function DropdownMenuRadioGroup({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioGroup>) {
  return (
    <DropdownMenuPrimitive.RadioGroup
      data-slot="dropdown-menu-radio-group"
      {...props}
    />
  )
}

function DropdownMenuRadioItem({
  className,
  children,
  shortcut,
  detail,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioItem> & {
  shortcut?: string
  detail?: React.ReactNode
}) {
  return (
    <DropdownMenuPrimitive.RadioItem
      data-slot="dropdown-menu-radio-item"
      className={cn(MENU_ROW, className)}
      {...props}
    >
      <MenuRow
        icon={
          <DropdownMenuPrimitive.ItemIndicator>
            <CheckIcon />
          </DropdownMenuPrimitive.ItemIndicator>
        }
        trailing={<MenuTrailing shortcut={shortcut} detail={detail} />}
      >
        {children}
      </MenuRow>
    </DropdownMenuPrimitive.RadioItem>
  )
}

function DropdownMenuLabel({
  className,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      data-slot="dropdown-menu-label"
      className={cn(MENU_LABEL, "text-caption-11-medium text-muted-foreground", className)}
      {...props}
    >
      <MenuLabelText>{children}</MenuLabelText>
    </DropdownMenuPrimitive.Label>
  )
}

function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-border", className)}
      {...props}
    />
  )
}

export type ShellMenuItemSpecProps = {
  tone?: "default" | "danger";
  state?: "rest" | "hover" | "focus";
  label: string;
  icon?: React.ReactNode;
  shortcut?: string;
  id?: string;
  className?: string;
};

export function ShellMenuItemSpec({
  tone = "default",
  state = "rest",
  label,
  icon,
  shortcut,
  id,
  className,
}: ShellMenuItemSpecProps) {
  const isDanger = tone === "danger";
  const isHover = state === "hover" || state === "focus";

  // The same three columns a real row has (components/ui/menu-row): the spec
  // card is what the gallery measures, so it must not be a different layout.
  return (
    <div
      id={id}
      data-slot="menu-item-spec"
      data-tone={tone}
      data-state={state}
      className={cn(
        "grid w-50 h-7 grid-cols-menu-row items-center gap-x-2 rounded-sm px-2 text-body-13 select-none cursor-pointer transition-colors",
        !isDanger && !isHover && "bg-transparent text-text-primary",
        !isDanger && isHover && "bg-state-soft text-text-primary",
        isDanger && !isHover && "bg-transparent text-status-danger",
        isDanger && isHover && "bg-feedback-danger-soft text-status-danger",
        className
      )}
    >
      <MenuRow icon={icon} trailing={<MenuTrailing shortcut={shortcut} />}>
        {label}
      </MenuRow>
    </div>
  );
}

function DropdownMenuSub({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Sub>) {
  return <DropdownMenuPrimitive.Sub data-slot="dropdown-menu-sub" {...props} />
}

function DropdownMenuSubTrigger({
  className,
  icon,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubTrigger> & {
  icon?: React.ReactNode
}) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      data-slot="dropdown-menu-sub-trigger"
      className={cn(MENU_ROW, className)}
      {...props}
    >
      <MenuRow icon={icon} trailing={<ChevronRightIcon className="text-text-tertiary" />}>
        {children}
      </MenuRow>
    </DropdownMenuPrimitive.SubTrigger>
  )
}

function DropdownMenuSubContent({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.Portal>
      {/* Portalled, unlike stock shadcn. The parent Content carries
          backdrop-filter for the glass tier, and that makes it a containing
          block for fixed-position descendants — so the submenu stopped
          escaping and the parent's overflow-x-hidden clipped it away
          entirely. */}
      <DropdownMenuPrimitive.SubContent
      data-slot="dropdown-menu-sub-content"
      className={cn("z-50 min-w-24 origin-(--radix-dropdown-menu-content-transform-origin) overflow-hidden rounded-lg glass-float p-1 text-popover-foreground duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95", className )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

export {
  DropdownMenu,
  DropdownMenuPortal,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
}
