import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * The app's one and only title bar — Figma `Titlebar 732:2170`.
 *
 * There used to be five: Workbench's at 40px, VaultGate's at 30px, a dead
 * `vault-shell.tsx` at 30px, a hand-copied class string inside step4a's test,
 * and another copy in the component gallery. They drifted apart in every way
 * they could — different heights, different traffic-light reservations
 * (pl-20 vs pl-21), different fullscreen insets (pl-1 vs pl-4).
 *
 * Height matters more than it looks. macOS places the traffic lights at a fixed
 * offset from the window's top edge and Wails v3 exposes no way to move them,
 * so a bar that changes height between screens cannot keep them centred on
 * both. One bar, one height, one alignment.
 *
 * This component owns the frame: height, drag region, traffic-light
 * reservation and the right inset. What goes inside is the caller's — the vault
 * gate shows status text, the workbench shows action clusters.
 */

/**
 * Whether the OS window has focus.
 *
 * Figma 732:2170 `State` is a contrast change only — nothing moves, every glyph
 * steps down to text/tertiary and the lights go the grey macOS paints them.
 * The webview's focus/blur fire on window activation, so no Wails event is
 * needed; document.hasFocus() seeds it so a titlebar mounted while the window
 * is already in the background does not start out looking active.
 */
function useWindowActive(): boolean {
  const [active, setActive] = React.useState(() =>
    typeof document === "undefined" ? true : document.hasFocus(),
  )

  React.useEffect(() => {
    const onFocus = () => setActive(true)
    const onBlur = () => setActive(false)
    window.addEventListener("focus", onFocus)
    window.addEventListener("blur", onBlur)
    setActive(document.hasFocus())
    return () => {
      window.removeEventListener("focus", onFocus)
      window.removeEventListener("blur", onBlur)
    }
  }, [])

  return active
}

export interface TitlebarProps extends React.ComponentProps<"header"> {
  /**
   * Fullscreen has no traffic lights, so the content zone starts at space/4
   * instead of reserving room for them. Figma 732:2170 `Mode`.
   */
  fullscreen?: boolean
}

function Titlebar({
  fullscreen = false,
  className,
  children,
  ...props
}: TitlebarProps) {
  const active = useWindowActive()

  return (
    <header
      data-slot="title-bar"
      data-mode={fullscreen ? "fullscreen" : "windowed"}
      // The dim itself is a CSS rule in index.css rather than utilities here:
      // children set their own colours, and a parent utility cannot win against
      // a child's own class without an escalation war.
      data-active={active}
      className={cn(
        // No material of its own: Figma's root is bare, `h-[40px] relative
        // w-[1440px]`, with no fill, border, blur or shadow.
        // pt shifts the content down onto the traffic lights' optical line;
        // see --titlebar-optical-offset in index.css for why it is not zero.
        "relative flex h-10 shrink-0 items-center gap-1 pr-4 pt-(--titlebar-optical-offset)",
        "transition-[padding-left] duration-300 ease-out motion-reduce:transition-none",
        // The whole row is the drag surface; interactive children opt out with
        // [--wails-draggable:no-drag]. main.go keeps InvisibleTitleBarHeight at
        // zero because that native hit zone cannot make that distinction.
        "[--wails-draggable:drag]",
        // pl-21 (84) + the first control's own mx-1 (4) lands it at x=88, which
        // is where Figma starts the module zone. Traffic lights end at 72, so
        // this also reproduces the 16px clearance.
        fullscreen ? "pl-4" : "pl-21",
        className,
      )}
      {...props}
    >
      {children}
    </header>
  )
}

export { Titlebar }
