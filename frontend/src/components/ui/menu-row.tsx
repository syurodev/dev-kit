import * as React from "react"

import { cn } from "@/lib/utils"
import { Kbd } from "@/components/ui/kbd"

/**
 * One row of a menu — context menu and dropdown alike — as three columns: an
 * icon, the label, and whatever sits at the end (a shortcut, a chevron, a
 * status).
 *
 * The icon column is there whether or not the row has an icon. A row without
 * one keeps the slot empty, so every label in a menu starts on the same
 * vertical line — the way IntelliJ's and VS Code's menus read, and the thing
 * a menu of mixed rows got wrong when the icon was just the first child of a
 * flex row. Both menu components use this, so they cannot drift apart.
 */
export const MENU_ROW = cn(
  "relative grid h-7 w-full min-w-50 cursor-pointer grid-cols-menu-row items-center gap-x-2 rounded-sm px-2 text-body-13 outline-none select-none transition-control",
  "bg-transparent text-text-primary",
  "focus:bg-state-soft hover:bg-state-soft data-open:bg-state-soft",
  "data-disabled:pointer-events-none data-disabled:opacity-50",
  "[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-3.5"
)

/** The destructive tone, for either menu's item. */
export const MENU_ROW_DESTRUCTIVE = cn(
  "data-[variant=destructive]:bg-transparent data-[variant=destructive]:text-status-danger",
  "data-[variant=destructive]:focus:bg-feedback-danger-soft data-[variant=destructive]:hover:bg-feedback-danger-soft data-[variant=destructive]:focus:text-status-danger data-[variant=destructive]:hover:text-status-danger",
  "data-[variant=destructive]:*:[svg]:text-status-danger"
)

export function MenuRow({
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
      <span data-slot="menu-icon" aria-hidden className="flex size-(--icon-14) items-center justify-center">
        {icon}
      </span>
      <span className="min-w-0 truncate">{children}</span>
      <span className="flex items-center justify-end">{trailing}</span>
    </>
  )
}

/**
 * The end column's content: a shortcut in the app's key cap, or — for a row
 * whose end is a state rather than a key, "in sync", "2 running" — quiet text.
 */
export function MenuTrailing({ shortcut, detail }: { shortcut?: string; detail?: React.ReactNode }) {
  if (shortcut) return <Kbd className="ml-4">{shortcut}</Kbd>
  // Truncates: the trailing slot is one line at the end of a fixed-height row,
  // and a caller that puts a sentence in it used to overflow the row and draw
  // over its neighbours. Capped in absolute terms, not as a share of the row:
  // the slot is an `auto` grid column, a percentage of it is worked out against
  // the width the text itself asked for, and so it cut the text in half while
  // the column kept the whole width — the label was squeezed out by a detail it
  // could not even show. 10rem is what a state or a time needs.
  if (detail)
    return <span className="ml-4 max-w-40 min-w-0 truncate text-body-12 text-text-tertiary">{detail}</span>
  return null
}

/** A label starts where the rows' labels start: past the icon column. */
export const MENU_LABEL = "grid grid-cols-[var(--icon-14)_minmax(0,1fr)] gap-x-2 px-2 py-1"

export function MenuLabelText({ children }: { children: React.ReactNode }) {
  return (
    <>
      <span aria-hidden />
      <span className="min-w-0 truncate">{children}</span>
    </>
  )
}
