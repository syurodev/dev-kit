import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { themeCss, themeSelector } from "./applyTheme";

describe("themeSelector", () => {
  // The whole reason a family is written under TWO attributes. index.css puts
  // the base at `:root` (0,1,0) and light at `:root[data-theme="light"]`
  // (0,2,0); a family at plain `:root` loses to light mode and would apply in
  // dark only — silently, which is the worst way for it to fail.
  test("outranks light mode without an !important", () => {
    const selector = themeSelector("dracula", "light");
    const classes = selector.match(/\[|:root/g) ?? [];
    expect(selector).toBe(':root[data-theme-family="dracula"][data-theme="light"]');
    // :root + two attribute selectors = three, against light mode's two.
    expect(classes).toHaveLength(3);
  });

  test("names the mode, so a stale block cannot paint the wrong one", () => {
    expect(themeSelector("ayu", "dark")).toContain('[data-theme="dark"]');
    expect(themeSelector("ayu", "dark")).not.toContain("light");
  });
});

describe("themeCss", () => {
  const palettes = {
    dark: { "--surface-base": "#1e1e2e", "--text-primary": "#cdd6f4" },
    light: { "--surface-base": "#eff1f5" },
  } as const;

  test("writes one block per mode, both at once", () => {
    const css = themeCss("catppuccin", palettes);
    expect(css).toContain(':root[data-theme-family="catppuccin"][data-theme="dark"]');
    expect(css).toContain(':root[data-theme-family="catppuccin"][data-theme="light"]');
    expect(css).toContain("--surface-base:#1e1e2e");
    expect(css).toContain("--surface-base:#eff1f5");
  });

  // Both modes are authored together because the mode can change without the
  // family changing — the system flipping at sunset. Re-authoring CSS on that
  // turns a free attribute swap into a parse.
  test("a mode flip needs no new CSS", () => {
    const css = themeCss("catppuccin", palettes);
    expect(css.match(/data-theme="(dark|light)"/g)).toHaveLength(2);
  });

  // devkit is the absence of an override, not a copy of index.css. An empty
  // palette must produce no rule at all, or it would pin the base values at
  // (0,3,0) and any later override would need more specificity still.
  test("an empty palette writes nothing", () => {
    expect(themeCss("devkit", { dark: {}, light: {} })).toBe("");
  });

  test("a palette with only one mode writes only that mode", () => {
    const css = themeCss("github", { dark: { "--surface-base": "#0d1117" }, light: {} });
    expect(css).toContain('[data-theme="dark"]');
    expect(css).not.toContain('[data-theme="light"]');
  });
});

describe("the swap does not animate", () => {
  // Measured in Chromium on a 2 000-row list before this existed: every row
  // ran a 150 ms background-color transition, because `transition-colors` is
  // on most of the app's rows and a palette swap changes every colour at once.
  // That is the paint smoothness §4.2 forbids, and it contradicts §1 — a theme
  // change is instant-layer, so the frame after the click is already the new
  // theme. Asserted against the source because the behaviour is a class that
  // exists for two frames, which renderToString cannot see.
  const source = readFileSync(join(import.meta.dir, "applyTheme.ts"), "utf8");
  const css = readFileSync(join(import.meta.dir, "../index.css"), "utf8");

  test("the writer suppresses transitions across the swap", () => {
    expect(source).toContain("theme-switching");
    expect(source, "one rAF only proves a recalc, not a paint").toMatch(
      /requestAnimationFrame\([\s\S]*requestAnimationFrame/,
    );
  });

  test("index.css still defines what the class suppresses", () => {
    expect(css).toContain(":root.theme-switching");
    expect(css).toMatch(/:root\.theme-switching[\s\S]{0,200}transition:\s*none\s*!important/);
  });

  // The first application has no previous colour to animate away from, so the
  // class would only buy a style recalc.
  test("nothing is suppressed on the first application", () => {
    expect(source).toContain("root.dataset.themeFamily !== undefined");
  });
});
