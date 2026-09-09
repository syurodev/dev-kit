import { System } from "@wailsio/runtime";

/**
 * Which desktop the app is drawing against, or `web` when it is not a desktop
 * at all — the component gallery, the test renderer, any plain browser.
 *
 * Until this existed the frontend had no platform branch anywhere (`grep` for
 * navigator.platform found two comments saying it deliberately did NOT branch),
 * and the shell chrome was written to macOS alone. That is fine for everything
 * the design system says — spacing, colour, type — and wrong for exactly one
 * thing: who draws the window controls, and therefore where they sit.
 */
export type Platform = "mac" | "windows" | "linux" | "web";

/**
 * Wails serves the OS synchronously off `window._wails.environment`, but it
 * injects that object on WindowLoadFinished, which can land after React has
 * mounted — Wails' own runtime treats the field as possibly-absent and polls
 * for it (drag.ts). React gets one shot at the first paint and has nothing to
 * re-run on, so a miss here would leave the title bar wrong for the life of the
 * window. The user agent is the fallback, and inside a webview it is a reliable
 * one: WebKitGTK says "X11; Linux", WKWebView says "Macintosh", WebView2 says
 * "Windows NT".
 */
function fromUserAgent(userAgent: string): Platform {
  if (/Macintosh|Mac OS X/i.test(userAgent)) return "mac";
  if (/Windows/i.test(userAgent)) return "windows";
  if (/Linux|X11|CrOS/i.test(userAgent)) return "linux";
  return "web";
}

export function detectPlatform(): Platform {
  // Server rendering (bun test, renderToString) has no window at all.
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return "web";
  }
  // The same probe main.tsx uses to decide the document is inside Wails. A
  // browser on Linux must not be mistaken for the Linux desktop app: it has a
  // real title bar of its own and no window to control.
  if (!("_wails" in window)) {
    return "web";
  }
  if (System.IsMac()) return "mac";
  if (System.IsWindows()) return "windows";
  if (System.IsLinux()) return "linux";
  return fromUserAgent(navigator.userAgent);
}

/**
 * Whether the OS insets its own window buttons into the app's title bar, so the
 * bar has to keep its left end clear for them.
 *
 * macOS only. main.go asks for MacTitleBarHiddenInset, which puts the traffic
 * lights inside our 40px bar; Figma 732:2170 reserves pl-21 (84px) for them and
 * starts the module zone at x=88. Linux and Windows put their window buttons on
 * the right — on Linux we now draw them ourselves — so on those platforms that
 * reservation is 84px of dead space with nothing in it.
 */
export function reservesWindowButtons(platform: Platform): boolean {
  return platform === "mac";
}

/**
 * Whether the app draws minimise/maximise/close itself.
 *
 * True exactly where main.go asks for a frameless window, which today is Linux
 * — see framelessWindow in native_webview_linux.go. Frameless means GTK draws
 * no decoration at all, so if the app does not draw the controls the window has
 * none. Windows keeps its native caption and macOS its traffic lights, and
 * neither wants a second set.
 */
export function ownsWindowControls(platform: Platform): boolean {
  return platform === "linux";
}
