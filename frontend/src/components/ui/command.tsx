import * as React from "react"
import { Command as CommandPrimitive } from "cmdk"
import { Search, Check } from "lucide-react"

import { cn } from "@/lib/utils"
import { Kbd } from "@/components/ui/kbd"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/**
 * Shell / Command Palette (Figma 719:1475 — Results / Empty)
 *
 * - Box: 640 wide, rounded-14, glass-float 62% + backdrop-blur-[20px] + --glass-border,
 *   NO shadow, NO inset highlight
 * - Search row: h56, pl-5 pr-4, gap-3, glyph 20; text 16 SemiBold (text-title-16) —
 *   --text-tertiary as placeholder, --text-primary once typed
 * - Kbd chip: --surface-raised + --border-subtle, r6, p-1, Mono 11 --text-tertiary
 * - Hairline: 1px --border-subtle, one above the list and one above the footer
 * - List (Results): px-2, gap-1, pt-1 pb-2
 * - List (Empty): px-2, gap-2, pt-10 pb-12, items-center
 * - Group head: Mono 11 Regular --text-tertiary, pt-2 pb-1 pl-3 (text-code-11)
 *   EXCEPTION — MODULES / COMMANDS group heads are Code/11 mono uppercase text, not Label/11 Caps.
 * - Row: h40, r8, px-3, gap-3, glyph 16
 * - Active row: --surface-raised + label 13 Medium --text-primary
 * - Other rows: no background at all + label 13 Regular --text-secondary
 *   EXCEPTION — inactive rows have no background, not a faint one. Only the active row gets --surface-raised.
 * - Empty title / body: 16 SemiBold --text-secondary (text-title-16) / 13 Regular --text-tertiary (text-body-13)
 * - Footer: h40, px-5, gap-4, Mono 11 --text-tertiary (text-code-11)
 */

function Command({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive>) {
  return (
    <CommandPrimitive
      data-slot="command"
      className={cn(
        "flex w-[640px] max-w-[640px] flex-col overflow-hidden rounded-float bg-glass-float backdrop-blur-[20px] border border-glass-border shadow-none text-text-primary select-none",
        className
      )}
      {...props}
    />
  )
}

function CommandDialog({
  title = "Command Palette",
  description = "Search for a command to run...",
  children,
  className,
  showCloseButton = false,
  commandProps,
  ...props
}: React.ComponentProps<typeof Dialog> & {
  title?: string
  description?: string
  className?: string
  showCloseButton?: boolean
  /** For the Command inside — shouldFilter, loop, a controlled value. */
  commandProps?: Omit<React.ComponentProps<typeof CommandPrimitive>, "children" | "className">
}) {
  return (
    <Dialog {...props}>
      <DialogHeader className="sr-only">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <DialogContent
        className={cn(
          // sm:max-w-[640px], not just max-w: DialogContent carries its own
          // sm:max-w-sm, and tailwind-merge keeps both because the variants
          // differ — so at any real window width the dialog was 384px and cut
          // the 640px palette inside it off at the right (no esc chip, the
          // shortcuts against the edge). The min() keeps the 1rem gutters the
          // dialog's own max-w gave a narrow window, which a bare 640px would
          // have overridden.
          // Up by the titlebar, where Spotlight and an editor's quick-open
          // sit, not a third of the way down: it is a way to go somewhere, so
          // it belongs where the eye already is for navigation, and from there
          // it has most of the window for its list. 3.5rem is the 40px
          // titlebar plus a 1rem gap.
          //
          // One variable, because two numbers have to agree: where the dialog
          // starts, and how tall the column inside may be (below).
          "[--command-top:3.5rem] top-(--command-top) translate-y-0 overflow-hidden rounded-float! p-0 border-none bg-transparent shadow-none w-[640px] max-w-[min(640px,calc(100%-2rem))] sm:max-w-[min(640px,calc(100%-2rem))]",
          className
        )}
        showCloseButton={showCloseButton}
      >
        {/* Fills the dialog, which is what decides the width: left at its own
            640px it would overflow a narrower dialog and be clipped again.

            The height is capped at what is left of the window below the
            dialog's top, less a 1rem gutter. A list's own max-height knows
            nothing about where the dialog starts: a 28rem list under a 56px
            input, starting a third of the way down a 640px window, ran off the
            bottom and took the footer with it. Capping the column instead
            works for any list — the input and footer do not shrink, so the
            list gives way and scrolls inside. */}
        <Command className="w-full max-h-[calc(100vh-var(--command-top)-1rem)]" {...commandProps}>
          {children}
        </Command>
      </DialogContent>
    </Dialog>
  )
}

function CommandInput({
  className,
  shortcut = "esc",
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Input> & {
  shortcut?: string;
}) {
  return (
    <div
      data-slot="command-input-wrapper"
      className="flex h-14 shrink-0 items-center gap-3 border-b border-border-subtle pl-5 pr-4"
    >
      <Search className="size-5 shrink-0 text-text-tertiary" />
      <CommandPrimitive.Input
        data-slot="command-input"
        className={cn(
          "flex-1 bg-transparent text-title-16 text-text-primary placeholder:text-text-tertiary outline-none disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
        {...props}
      />
      {shortcut ? <Kbd>{shortcut}</Kbd> : null}
    </div>
  )
}

function CommandList({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.List>) {
  return (
    <CommandPrimitive.List
      data-slot="command-list"
      className={cn(
        "no-scrollbar max-h-80 overflow-x-hidden overflow-y-auto outline-none px-2 pt-1 pb-2 flex flex-col gap-1",
        className
      )}
      {...props}
    />
  )
}

function CommandEmpty({
  className,
  title = "No results found",
  children = "Try searching for a different command or keyword.",
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Empty> & {
  title?: string;
}) {
  return (
    <CommandPrimitive.Empty
      data-slot="command-empty"
      className={cn(
        "flex flex-col items-center justify-center gap-2 px-2 pt-10 pb-12 text-center",
        className
      )}
      {...props}
    >
      <p className="text-title-16 text-text-secondary">{title}</p>
      <p className="text-body-13 text-text-tertiary">{children}</p>
    </CommandPrimitive.Empty>
  )
}

function CommandGroup({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Group>) {
  return (
    <CommandPrimitive.Group
      data-slot="command-group"
      className={cn(
        "overflow-hidden text-text-primary **:[[cmdk-group-heading]]:pt-2 **:[[cmdk-group-heading]]:pb-1 **:[[cmdk-group-heading]]:pl-3 **:[[cmdk-group-heading]]:text-code-11 **:[[cmdk-group-heading]]:uppercase **:[[cmdk-group-heading]]:font-normal **:[[cmdk-group-heading]]:text-text-tertiary",
        className
      )}
      {...props}
    />
  )
}

function CommandSeparator({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Separator>) {
  return (
    <CommandPrimitive.Separator
      data-slot="command-separator"
      className={cn("h-px bg-border-subtle", className)}
      {...props}
    />
  )
}

function CommandItem({
  className,
  children,
  indicator = true,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Item> & {
  /**
   * The trailing check on the highlighted row. Right for a picker, where the
   * row is about to become the choice. Wrong for the palette: Figma 719:1475
   * draws none, and because the check and a shortcut chip both carry ml-auto,
   * the two split the free space and leave the chip in the middle of the row.
   */
  indicator?: boolean
}) {
  return (
    <CommandPrimitive.Item
      data-slot="command-item"
      className={cn(
        "group/command-item relative flex h-10 cursor-pointer items-center gap-3 rounded-md px-3 text-body-13 text-text-secondary outline-none select-none transition-colors",
        // Inactive: NO background at all (EXCEPTION)
        "bg-transparent",
        // Active: --surface-raised + text-body-13-medium text-text-primary
        "data-[selected=true]:bg-surface-raised data-[selected=true]:text-body-13-medium data-[selected=true]:text-text-primary",
        "data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50",
        "[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      {children}
      {indicator ? (
        <Check className="ml-auto size-4 opacity-0 group-data-[selected=true]/command-item:opacity-100 text-text-secondary" />
      ) : null}
    </CommandPrimitive.Item>
  )
}

function CommandShortcut({
  className,
  ...props
}: React.ComponentProps<typeof Kbd>) {
  // The app's one key chip (kbd.tsx), not a second one drawn here: a shortcut
  // beside a command should look like the same shortcut in a menu, a tooltip
  // or the module switcher. It stays as it is on the selected row — only the
  // row's label steps up (Figma 719:1475).
  return <Kbd className={cn("ml-auto shrink-0", className)} {...props} />
}

/** A key, then what it does — the footer's unit. */
function CommandHint({ keys, children }: { keys: string; children: React.ReactNode }) {
  return (
    <span className="flex shrink-0 items-center gap-1.5">
      <Kbd>{keys}</Kbd>
      {children}
    </span>
  )
}

function CommandFooter({
  className,
  children,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="command-footer"
      className={cn(
        "flex h-10 shrink-0 items-center gap-4 border-t border-border-subtle px-5 text-code-11 text-text-tertiary",
        className
      )}
      {...props}
    >
      <CommandHint keys="↑↓">navigate</CommandHint>
      <CommandHint keys="↵">open</CommandHint>
      <CommandHint keys="esc">close</CommandHint>
      {/* Pushed to the far end: where a caller says what the list is scoped to. */}
      {children ? <span className="ml-auto flex min-w-0 items-center gap-4 truncate">{children}</span> : null}
    </div>
  )
}

export {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandShortcut,
  CommandSeparator,
  CommandFooter,
  CommandHint,
}
