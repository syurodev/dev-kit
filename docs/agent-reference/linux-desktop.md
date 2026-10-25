### Linux desktop (GTK4 / WebKitGTK)
The shell was written macOS-first and now branches in exactly two places; nothing else
in the frontend knows what OS it is on (`frontend/src/lib/platform.ts` is the only
platform module).

- **Black window on first run.** WebKitGTK's DMABUF renderer fails silently on hosts
  that cannot share the buffer (VMs, containers, some Wayland/driver combinations):
  the page loads and the DOM is live in the Web Inspector, but nothing paints. It looks
  total rather than merely wrong because `.native` leaves html/body transparent, so what
  shows through is the window's near-black `BackgroundColour`. `native_webview_linux.go`
  now sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` before GTK comes up unless the variable is
  already in the environment — export it yourself (any value) to take the decision back.
- **Frameless window.** GTK would otherwise stack its own decoration above DevKit's 40px
  title bar. `Frameless` is on for Linux only (`framelessWindow`); Wails' JS runtime keeps
  drag (`--wails-draggable`) and re-implements edge resize for this case. The frontend
  draws minimise/maximise/close itself (`components/ui/window-controls.tsx`) because a
  frameless window otherwise has none, and `Titlebar` drops the 84px macOS traffic-light
  reservation everywhere except macOS. Wails skips `gtk_window_set_title` on a frameless
  window, so `applyWindowTitle` puts WM_NAME back by hand.
- **Scroll boxes in an inert shell panel stop scrolling** (Linux app with overlay scrollbars
  only: `lib/platform.quietsScrollBoxes` marks `<html data-quiet-scroll-boxes>`). WebKitGTK
  drops a clipped scroller's layer and rebuilds it in the frames that reveal it, so while a
  column or the bottom row is inert, boxes that opt in with `data-scroll-box` (and
  `.cm-scroller`) are `overflow: hidden` (`workbench/slots/slots.css`). Give a new scroll box
  inside a pane body `data-scroll-box`.
- **Verifying the GUI headlessly.** `Xvfb :99 -screen 0 1400x900x24` + `DISPLAY=:99
  ./bin/dev-kit`, then `import -window root shot.png` and drive it with `xdotool`. Needs
  `dbus-x11` on PATH (single-instance uses `dbus-launch`). A bare Xvfb has no window
  manager, so maximise/minimise do nothing there — close and the layout are testable.

