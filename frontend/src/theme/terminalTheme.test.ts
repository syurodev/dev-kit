import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// terminalTheme() needs a live engine to resolve colours; what it READS is
// the contract, so that is asserted against the source.
const source = readFileSync(join(import.meta.dir, "terminalTheme.ts"), "utf8")
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/\/\/.*$/gm, "");

describe("the terminal palette comes from the Color palette", () => {
  test("chromatic slots read --ansi-*", () => {
    for (const slot of ["red", "green", "yellow", "blue", "magenta", "cyan"]) {
      expect(source).toContain(`c["--ansi-${slot}"]`);
    }
  });

  test("no slot reads the status ramp", () => {
    expect(source).not.toContain("--status-");
  });
});
