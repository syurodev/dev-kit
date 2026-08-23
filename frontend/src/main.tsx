import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'
import App from './App'

// Inside Wails the window itself is transparent (main.go paints RGBA 0,0,0,0
// on darwin) so the native vibrancy backdrop can show through — but only if
// the page doesn't paint over it. index.css keeps html/body opaque for plain
// browsers; this class lifts that exactly where the vibrancy exists.
if ('_wails' in window) {
  document.documentElement.classList.add('native')
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
