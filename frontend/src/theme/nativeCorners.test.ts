import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { nativeCornerKey } from "./nativeCorners";
import { WINDOW_GUTTER } from "../workbench/panelLayout";

describe("native window corners", () => {
  test("only a measured platform gets a key; everything else takes the default", () => {
    expect(nativeCornerKey("darwin", "27.0")).toBe("macos-26");
    expect(nativeCornerKey("darwin", "26.1")).toBe("macos-26");
    // macOS 15 and earlier have a smaller window corner that nobody has
    // measured yet, so they fall through rather than borrow Tahoe's.
    expect(nativeCornerKey("darwin", "15.4")).toBeUndefined();
    // Deliberately unlisted until measured the same way macOS was.
    expect(nativeCornerKey("windows", "10.0.22631")).toBeUndefined();
    expect(nativeCornerKey("linux", "24.04")).toBeUndefined();
    // A shell that answers with nothing usable must not guess.
    expect(nativeCornerKey("darwin", undefined)).toBeUndefined();
    expect(nativeCornerKey("darwin", "Unknown")).toBeUndefined();
    expect(nativeCornerKey(undefined, undefined)).toBeUndefined();
  });

  test("the concentric corner subtracts the real window gutter", () => {
    // inner radius = outer radius - gap. index.css writes the gap as a literal
    // 4px because one rule uses it, so this is what stops the two drifting:
    // change WINDOW_GUTTER and the corner stops being concentric silently.
    expect(WINDOW_GUTTER).toBe(4);

    const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");
    const rules = css
      .split("\n")
      .filter((line) => line.includes("border-bottom-") && line.includes("--radius-window"));
    expect(rules).toHaveLength(2);
    for (const rule of rules) {
      expect(rule).toContain(`calc(var(--radius-window) - ${WINDOW_GUTTER}px)`);
    }
  });

  test("the macOS corner stays tied to the toolbar style that sets it", () => {
    // Apple publishes three window corners by chrome: toolbar 26pt, compact
    // toolbar 20pt, titlebar 16pt. Which one applies is decided in main.go, in
    // another language, and nothing else would notice the day it changes.
    const mainGo = readFileSync(new URL("../../../main.go", import.meta.url), "utf8");
    expect(mainGo).toContain("MacToolbarStyleUnifiedCompact");

    const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");
    const macos = css.match(/\[data-os="macos-26"\]\s*\{[^}]*--radius-window:\s*(\d+)px/);
    expect(Number(macos?.[1])).toBe(20);
  });

  test("the default window radius matches the shell's own corner plus the gutter", () => {
    // With no data-os the geometry must be exactly what it was before any of
    // this existed: radius/10 on the card, 4px of gutter. Otherwise the
    // fallback silently redesigns the app on every unmeasured platform.
    const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");
    const control = css.match(/--radius-control:\s*(\d+)px/)?.[1];
    const windowRadius = css.match(/--radius-window:\s*(\d+)px/)?.[1];
    expect(Number(windowRadius)).toBe(Number(control) + WINDOW_GUTTER);
  });
});
