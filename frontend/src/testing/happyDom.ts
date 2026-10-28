import { GlobalWindow } from "happy-dom";

/**
 * A DOM for the few tests that need React to actually mount — an error
 * boundary is never run by `renderToString`, so a boundary's real behaviour
 * (siblings stay up, a retry remounts) can only be seen in a client render.
 * Registered on the test file's own globals; `bun test --isolate` gives every
 * file its own, so nothing leaks into the string-render tests. Import this
 * before `react-dom/client`.
 */
const window = new GlobalWindow({ url: "http://localhost/" });

// The DOM classes and document, not the timers, console or fetch Bun already has.
const keep = new Set([
  "console",
  "setTimeout",
  "clearTimeout",
  "setInterval",
  "clearInterval",
  "queueMicrotask",
  "fetch",
  "Response",
  "Request",
  "Headers",
  "URL",
  "URLSearchParams",
  "AbortController",
  "AbortSignal",
  "TextEncoder",
  "TextDecoder",
  "performance",
  "structuredClone",
]);
const source = window as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(window)) {
  if (keep.has(key) || key in globalThis) continue;
  try {
    Object.defineProperty(globalThis, key, { value: source[key], configurable: true, writable: true });
  } catch {
    // Some window properties are accessors that throw outside a real page.
  }
}
for (const key of ["window", "document", "navigator", "location", "getComputedStyle", "MutationObserver"]) {
  Object.defineProperty(globalThis, key, {
    value: key === "window" ? window : source[key],
    configurable: true,
    writable: true,
  });
}
Object.defineProperty(globalThis, "IS_REACT_ACT_ENVIRONMENT", { value: true, configurable: true, writable: true });

export const domWindow = window;
