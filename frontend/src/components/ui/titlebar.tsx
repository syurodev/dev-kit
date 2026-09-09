import * as React from "react"
import { Window } from "@wailsio/runtime"

import { WindowControls } from "@/components/ui/window-controls"
import { cn } from "@/lib/utils"
import {
  detectPlatform,
  ownsWindowControls,
  reservesWindowButtons,
  type Platform,
} from "@/lib/platform"

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
 * This component owns the frame: height, drag region, window-button
 * reservation and the end insets. What goes inside is the caller's — the vault
 * gate shows status text, the workbench shows action clusters.
 *
 * It also owns the one thing about the frame that is not the same everywhere.
 * The bar above is a macOS bar: AppKit insets the traffic lights into it, so it
 * reserves 84px on the left and leaves the right end to the caller. Linux and
 * Windows put their window buttons on the right instead, which made that
 * reservation 84px of empty space on those platforms — and on Linux the window
 * is frameless (framelessWindow in native_webview_linux.go), so there are no
 * window buttons at all until this bar draws them. Both follow from
 * @/lib/platform; nothing else in the frontend branches on the OS.
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
  /**
   * Which desktop this bar is drawing against. Defaults to the real one; the
   * tests and the component gallery pass it so every platform's bar can be
   * rendered from anywhere.
   */
  platform?: Platform
}

function Titlebar({
  fullscreen = false,
  platform,
  className,
  children,
  ...props
}: TitlebarProps) {
  const active = useWindowActive()
  // Read once per mount rather than per render: the answer cannot change for
  // the life of the window, and re-reading it would re-run the user-agent
  // fallback on every keystroke that reaches the shell.
  const resolvedPlatform = React.useMemo(() => platform ?? detectPlatform(), [platform])
  const drawsControls = ownsWindowControls(resolvedPlatform)

  /**
   * Double-click to maximise, which every desktop offers on its own title bar
   * and a frameless window loses along with the decoration.
   *
   * Only where we draw the controls: on macOS the Wails runtime already turns a
   * double-click on a drag surface into wails:drag:doubleclick (drag.ts), and a
   * second handler here would fight it. The draggability test is the runtime's
   * own rule, so the exemptions stay in one place — every interactive child in
   * this bar already sets [--wails-draggable:no-drag], and the property
   * inherits, so their descendants are covered too.
   */
  const handleDoubleClick = React.useCallback(
    (event: React.MouseEvent<HTMLElement>) => {
      if (!drawsControls) return
      const target = event.target as HTMLElement | null
      if (!target || typeof window === "undefined") return
      const draggable = window
        .getComputedStyle(target)
        .getPropertyValue("--wails-draggable")
        .trim()
      if (draggable !== "drag") return
      void Window.ToggleMaximise().catch(() => {})
    },
    [drawsControls],
  )

  return (
    <header
      data-slot="title-bar"
      data-mode={fullscreen ? "fullscreen" : "windowed"}
      // The dim itself is a CSS rule in index.css rather than utilities here:
      // children set their own colours, and a parent utility cannot win against
      // a child's own class without an escalation war.
      data-active={active}
      data-platform={resolvedPlatform}
      onDoubleClick={handleDoubleClick}
      className={cn(
        // No material of its own: Figma's root is bare, `h-[40px] relative
        // w-[1440px]`, with no fill, border, blur or shadow.
        "relative flex h-10 shrink-0 items-center gap-1",
        // pt shifts the content down onto the traffic lights' optical line; see
        // --titlebar-optical-offset in index.css for why it is not zero. It is
        // a calibration against a control only AppKit draws, so it applies only
        // where AppKit draws it — elsewhere it is 2px of unexplained sag.
        reservesWindowButtons(resolvedPlatform)
          ? "pt-(--titlebar-optical-offset)"
          : "pt-0",
        "transition-[padding-left] duration-300 ease-out motion-reduce:transition-none",
        // The whole row is the drag surface; interactive children opt out with
        // [--wails-draggable:no-drag]. main.go keeps InvisibleTitleBarHeight at
        // zero because that native hit zone cannot make that distinction.
        "[--wails-draggable:drag]",
        // pl-21 (84) + the first control's own mx-1 (4) lands it at x=88, which
        // is where Figma starts the module zone. Traffic lights end at 72, so
        // this also reproduces the 16px clearance.
        //
        // Only macOS insets its window buttons into this bar. On Linux and
        // Windows they sit on the right, so reserving the left is 84px of dead
        // space — which is exactly how the bar looked on Ubuntu: the sidebar
        // toggle floated at x=88 with nothing beside it.
        fullscreen || !reservesWindowButtons(resolvedPlatform) ? "pl-4" : "pl-21",
        // Our own controls sit flush against the window corner the way native
        // ones do; a padded bar keeps its space/4 end inset.
        drawsControls ? "pr-1" : "pr-4",
        className,
      )}
      {...props}
    >
      {children}
      {/* After the caller's content, because it is the window's cluster rather
          than the screen's: the vault gate and the workbench both get it, and
          neither has to know it exists. Fullscreen has nothing to minimise,
          maximise or restore to. */}
      {drawsControls && !fullscreen && <WindowControls className="ml-auto" />}
    </header>
  )
}

export { Titlebar }
