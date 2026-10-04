import { platformLabel } from "@/lib/shortcut"
import { cn } from "@/lib/utils"

/**
 * The app's one key chip. A plain-text label written with macOS glyphs is
 * spelled for the platform here (lib/shortcut `platformLabel`), so a menu
 * item's `shortcut="⌘W"` reads Ctrl+W on Linux and Windows without every
 * caller remembering to ask.
 */
function Kbd({ className, children, ...props }: React.ComponentProps<"kbd">) {
  return (
    <kbd
      data-slot="kbd"
      className={cn(
        "pointer-events-none inline-flex h-5 w-fit min-w-5 items-center justify-center gap-1 rounded-sm bg-muted px-1 text-label-12 text-muted-foreground select-none in-data-[slot=tooltip-content]:bg-foreground/10 in-data-[slot=tooltip-content]:text-foreground [&_svg:not([class*='size-'])]:size-(--icon-12)",
        className
      )}
      {...props}
    >
      {typeof children === "string" ? platformLabel(children) : children}
    </kbd>
  )
}

function KbdGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <kbd
      data-slot="kbd-group"
      className={cn("inline-flex items-center gap-1", className)}
      {...props}
    />
  )
}

export { Kbd, KbdGroup }
