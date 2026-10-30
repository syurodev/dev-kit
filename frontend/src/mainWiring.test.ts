import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

// main.tsx has side effects (it mounts the app), so it cannot be imported by a
// test; what has to hold is that App is mounted inside the root boundary.
test("main.tsx mounts App inside RootErrorBoundary", () => {
  const source = readFileSync(new URL("./main.tsx", import.meta.url), "utf8");
  expect(source).toMatch(/<RootErrorBoundary>\s*<App \/>\s*<\/RootErrorBoundary>/);
});

// Window-level errors outside React (an event handler, a rejected promise) go to
// DevKit's log through the same helper as the root boundary. Installed inside the
// desktop-window block: a browser tab has no Go to report to.
test("main.tsx installs the window error reporting once, inside the desktop-window block", () => {
  const source = readFileSync(new URL("./main.tsx", import.meta.url), "utf8");
  expect(source).toMatch(/import \{ installWindowErrorReporting \} from '\.\/bridge\/clientErrors'/);
  expect(source.match(/installWindowErrorReporting\(window\)/g)?.length).toBe(1);
  const block = source.slice(source.indexOf("if ('_wails' in window) {"), source.indexOf("// Set by the native side"));
  expect(block).toContain("installWindowErrorReporting(window)");
});
