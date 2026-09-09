import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'
import App from './App'
import { installNativeZoomGuard } from './lib/nativeZoom'
import { detectPlatform } from './lib/platform'

// Inside Wails, html/body stay clear so nothing paints over the native window;
// background/app is painted by the shell wrapper in AppShell, which is the
// layer Figma says owns it. index.css keeps html/body opaque for plain
// browsers, and this class lifts that exactly where Wails is.
//
// (This used to say the window is transparent for native vibrancy. It has been
// solid by default since the backdrop change in main.go.)
if ('_wails' in window) {
  document.documentElement.classList.add('native')
  // Only inside the desktop window. A browser tab — the component gallery —
  // keeps its page zoom, which there is a real accessibility affordance rather
  // than a gesture landing on the wrong layer.
  installNativeZoomGuard(window)

  // WebKitGTK paints no backdrop-filter on the software path this app runs on
  // Linux, so every glass tier there is a plain hole in the window: the New
  // agent session dialog showed the page straight through itself. CSS cannot
  // detect this — @supports(backdrop-filter) parses the declaration, it does
  // not render it, and WebKitGTK answers true and then draws nothing. Only the
  // platform knows, so the platform says so and index.css makes the tiers
  // opaque under this class.
  if (detectPlatform() === 'linux') {
    document.documentElement.classList.add('no-backdrop-blur')
  }
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
