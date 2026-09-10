import * as React from "react"
import { Events, Window } from "@wailsio/runtime"
import { Minimize2, Minus, Square, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/**
 * Minimise / maximise / close, drawn by the app because the OS is not drawing
 * them.
 *
 * This exists only where main.go asks for a frameless window — Linux today, see
 * framelessWindow in native_webview_linux.go. Frameless means GTK draws no
 * decoration at all: without this component the window has no controls and no
 * way to close from the keyboard-free path.
 *
 * Not a Figma component: 732:2170 is a macOS bar and has no such cluster,
 * because AppKit supplies it. Rather than invent a shape, every value is
 * borrowed from the title bar's own action buttons — 28px, radius 8, state/soft
 * hover, 12px glyph — so the controls read as part of the same row. Close is the
 * one departure, taking the danger tokens on hover: the desktop convention, and
 * the only thing separating it from "collapse a panel" at a glance.
 */

/**
 * Whether the window is maximised, kept live off the Wails events.
 *
 * Linux emits Common.WindowMaximise / WindowUnMaximise (webview_window_linux.go
 * maps the GTK state notify onto them), so the glyph stays correct when the
 * user maximises by dragging to the top edge or through a keyboard shortcut —
 * not just when they press our own button.
 */
function useMaximised(): boolean {
  const [maximised, setMaximised] = React.useState(false)

  React.useEffect(() => {
    let mounted = true
    let receivedEvent = false

    const update = (next: boolean) => {
      receivedEvent = true
      if (mounted) setMaximised(next)
    }

    const offMaximise = Events.On(Events.Types.Common.WindowMaximise, () => update(true))
    const offUnMaximise = Events.On(Events.Types.Common.WindowUnMaximise, () => update(false))

    void Window.IsMaximised()
      .then((value) => {
        // The seed must not overwrite an event that already arrived: the
        // promise resolves over the runtime transport and can land second.
        if (mounted && !receivedEvent) setMaximised(value)
      })
      .catch(() => {
        // Browser-only development has no native window to query.
      })

    return () => {
      mounted = false
      offMaximise()
      offUnMaximise()
    }
  }, [])

  return maximised
}

// The title bar's own action buttons, minus the tooltip: a tooltip that follows
// the pointer to the very corner of the screen is in the way of the control it
// describes. aria-label carries the name instead.
//
// rounded-md rather than the arbitrary 8px radius the workbench's copies write
// by hand — the same measurement, but taken from --radius-md on the scale,
// which is what designSystemConformance measures components/ against.
const CONTROL =
  "size-7 shrink-0 rounded-md hover:bg-state-soft hover:text-text-primary [--wails-draggable:no-drag]"

export function WindowControls({ className }: { className?: string }) {
  const maximised = useMaximised()

  return (
    <div className={cn("flex shrink-0 items-center gap-1", className)}>
      {/* The same hairline the workbench puts between its own title-bar
          clusters. Without it close sits flush against the last app action and
          reads as one more of them — a mis-click that ends the session rather
          than toggling a panel. */}
      <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden />
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="Minimise window"
        className={CONTROL}
        onClick={() => void Window.Minimise().catch(() => {})}
      >
        <Minus />
      </Button>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={maximised ? "Restore window" : "Maximise window"}
        className={CONTROL}
        onClick={() => void Window.ToggleMaximise().catch(() => {})}
      >
        {maximised ? <Minimize2 /> : <Square />}
      </Button>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="Close window"
        className={cn(
          CONTROL,
          "hover:bg-(--feedback-danger-hover) hover:text-status-danger",
        )}
        onClick={() => void Window.Close().catch(() => {})}
      >
        <X />
      </Button>
    </div>
  )
}
