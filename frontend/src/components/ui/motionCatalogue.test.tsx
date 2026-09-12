import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { Button } from "./button";
import { Chip } from "./chip";
import { TreeView } from "../tree-view";

const CSS = readFileSync(join(import.meta.dir, "../../index.css"), "utf8");

/** The rule from smoothness 4.1: everything reads its timing off a token. */
function usesMotionToken(html: string): boolean {
  return /duration-\(--motion-(fast|base|slow)\)/.test(html);
}

describe("motion catalogue", () => {
  it("Button presses and transitions on tokens", () => {
    const html = renderToString(<Button>Run</Button>);
    expect(html).toContain("active:scale-[0.98]");
    expect(usesMotionToken(html)).toBe(true);
    // disabled↔enabled is a colour and opacity change; transition-all covers it.
    expect(html).toContain("transition-all");
  });

  it("Chip transitions its colours rather than snapping", () => {
    const html = renderToString(<Chip>3 partitions</Chip>);
    expect(html).toContain("transition-colors");
    expect(usesMotionToken(html)).toBe(true);
  });

  it("a selected tree row is outlined, an unselected one holds the same box", () => {
    const selected = renderToString(
      <TreeView nodes={[{ id: "a", label: "public", selected: true }]} />,
    );
    const plain = renderToString(<TreeView nodes={[{ id: "a", label: "public" }]} />);
    expect(selected).toContain("border-action-primary/45");
    // Both carry a border — only its colour flips, so selecting a row cannot
    // move its label or shove the subtree below it down a pixel.
    expect(selected).not.toContain("border-transparent");
    expect(plain).toContain("border-transparent");
    expect(plain).not.toContain("border-action-primary");
  });
});

describe("motion defined in index.css", () => {
  it("the skeleton→content crossfade starts from transparent", () => {
    const block = CSS.match(/\.module-enter\s*\{[^}]*@starting-style\s*\{[^}]*\}[^}]*\}/s);
    expect(block, ".module-enter must carry an @starting-style block").not.toBeNull();
    expect(block![0]).toContain("opacity: 0");
    expect(block![0]).toContain("var(--motion-base)");
  });

  it("the row accent animates transform and opacity only", () => {
    const block = CSS.match(/\.row-accent::before\s*\{[^}]*\}/s)![0];
    expect(block).toContain("var(--motion-fast)");
    // 4.2: compositor properties only — no width/left animation.
    expect(block).toMatch(/transition:[^;]*opacity[^;]*scale/s);
  });

  it("every motion added here is switched off by prefers-reduced-motion", () => {
    const reduced = CSS.match(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\n\}/)![0];
    for (const cls of ["module-enter", "row-accent", "value-blink"]) {
      expect(reduced, `${cls} must be disabled under reduced motion`).toContain(cls);
    }
  });
});
