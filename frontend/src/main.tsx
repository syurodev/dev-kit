import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'
import App from './App'
import { installNativeZoomGuard } from './lib/nativeZoom'
import { installWindowShape } from './lib/windowShape'
import { SQUIRCLE_SHELL_CLASS } from './lib/shellSquircle'
import { markQuietScrollBoxes } from './lib/platform'

// Inside Wails, html/body stay clear so nothing paints over the native window;
// background/app is painted by the shell wrapper in AppShell, which is the
// layer Figma says owns it. index.css keeps html/body opaque for plain
// browsers, and this class lifts that exactly where Wails is. The window is
// solid by default — see the backdrop handling in main.go.
if ('_wails' in window) {
  document.documentElement.classList.add('native')
  // Only inside the desktop window. A browser tab — the component gallery —
  // keeps its page zoom, which there is a real accessibility affordance rather
  // than a gesture landing on the wrong layer.
  installNativeZoomGuard(window)
  // The OS window's corner radius, so the shell's cards run parallel to it.
  installWindowShape()
  // macOS draws that corner as a squircle, not an arc; the cards follow it.
  // navigator, not detectPlatform: _wails.environment may not be there yet.
  if (/Macintosh|Mac OS X/.test(navigator.userAgent)) {
    document.documentElement.classList.add(SQUIRCLE_SHELL_CLASS)
  }
  // WebKitGTK with overlay scrollbars: shell panels quiet their scroll boxes
  // while inert (lib/platform.quietsScrollBoxes, workbench/slots/slots.css).
  markQuietScrollBoxes(document)
}

// Set by the native side when WebKitGTK composites on the CPU and will not paint
// backdrop-filter (webviewStartURL, native_webview_linux.go). Before the first
// render, so no glass surface is ever drawn see-through. See index.css.
if (new URLSearchParams(window.location.search).has('software-compositing')) {
  document.documentElement.classList.add('no-backdrop-blur')
}

// Dev-only instrumentation (smoothness 9). Dynamic, so the chunk never lands
// in the bundle the performance budget measures; the localStorage switch is
// what makes it reachable in a production build when a "it feels janky"
// report needs numbers.
if (import.meta.env.DEV || localStorage.getItem('devkitPerf') === '1') {
  void import('./lib/perfTrace').then((m) => m.installPerfTrace())
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
