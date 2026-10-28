import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

// main.tsx has side effects (it mounts the app), so it cannot be imported by a
// test; what has to hold is that App is mounted inside the root boundary.
test("main.tsx mounts App inside RootErrorBoundary", () => {
  const source = readFileSync(new URL("./main.tsx", import.meta.url), "utf8");
  expect(source).toMatch(/<RootErrorBoundary>\s*<App \/>\s*<\/RootErrorBoundary>/);
});
