import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'
import App from './App'
import { applyNativeCorners } from './theme/nativeCorners'

// Inside Wails, html/body stay clear so nothing paints over the native window;
// background/app is painted by the shell wrapper in AppShell, which is the
// layer Figma says owns it. index.css keeps html/body opaque for plain
// browsers, and this class lifts that exactly where Wails is.
//
// (This used to say the window is transparent for native vibrancy. It has been
// solid by default since the backdrop change in main.go.)
if ('_wails' in window) {
  document.documentElement.classList.add('native')
}

// Fire and forget: the window's corner radius comes from the OS, and until the
// answer arrives the default in index.css is already a complete look.
void applyNativeCorners()

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
