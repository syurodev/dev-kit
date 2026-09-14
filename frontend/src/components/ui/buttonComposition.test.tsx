import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { Button } from "./button";
import { cn } from "@/lib/utils";
import { CraftButton, CraftButtonIcon, CraftButtonLabel } from "./craft-button";

describe("reading typography composition", () => {
  for (const type of ["text-body-14", "text-caption-12"]) {
    it(`${type} overrides size without stripping foreground colour`, () => {
      const merged = cn("text-body-13 text-text-primary", type);
      expect(merged).not.toContain("text-body-13");
      expect(merged).toContain(type);
      expect(merged).toContain("text-text-primary");
    });
  }
});

/**
 * Where a look belongs: a variant when it is only a look, a component when it
 * is a structure. Both of these came in as components; only one had a reason
 * to stay one.
 */
describe("glass is a variant", () => {
  it("renders the shared glass class rather than its own recipe", () => {
    const html = renderToString(
      <Button variant="glass" size="lg">
        Continue
      </Button>,
    );
    expect(html).toContain("glass-button");
    // ::before is absolute against it.
    expect(html).toContain("relative");
  });

  /**
   * The rule the title bar's nine copies broke: a variant that sets height,
   * padding or radius fights `size`, and loses — cva emits size after variant,
   * so the variant's version is dead class text that still has to be read.
   */
  it("owns no size of its own", () => {
    const source = readFileSync(join(import.meta.dir, "button.tsx"), "utf8");
    const variant = source.match(/glass:\s*"([^"]*)"/)?.[1];
    expect(variant, "the glass variant should exist in buttonVariants").toBeString();
    expect(variant).not.toMatch(/\bh-|\bpx-|\bpy-|\bsize-|\brounded-/);
  });

  it("takes its height and radius from size, so the sizes stay interchangeable", () => {
    const small = renderToString(<Button variant="glass" size="sm" />);
    const large = renderToString(<Button variant="glass" size="lg" />);
    expect(small).toContain("h-(--control-height-sm)");
    expect(large).toContain("h-(--control-height-xl)");
  });
});

/**
 * The trap under any icon button that has to change width.
 *
 * `size="icon-sm"` emits `size-(--control-height-sm)`, which sets width AND
 * height. tailwind-merge does not treat a later `w-auto` as conflicting with
 * it, so both survive into the class list and the winner is whichever rule the
 * stylesheet happens to emit last — not the one written last. Taking the
 * height from a text size and stating the width is what makes it decidable.
 */
describe("a width put beside an icon size", () => {
  it("does not win, which is why the Run button does not use one", () => {
    const merged = cn("size-(--control-height-sm) rounded-md p-0", "w-auto px-2");
    expect(merged).toContain("size-(--control-height-sm)");
    expect(merged).toContain("w-auto");
  });

  it("is decidable when the height comes from a text size", () => {
    const merged = cn("h-(--control-height-sm) gap-1.5 rounded-md px-2.5", "w-auto gap-0 px-2");
    expect(merged).not.toContain("px-2.5");
    expect(merged).not.toContain("gap-1.5");
    expect(merged).toContain("w-auto");
  });
});

describe("craft is a component", () => {
  const html = renderToString(
    <CraftButton>
      <CraftButtonIcon>
        <svg />
      </CraftButtonIcon>
      <CraftButtonLabel>New connection</CraftButtonLabel>
    </CraftButton>,
  );

  // The wash is a child that grows past its own box; the pill has to clip it.
  it("clips the wash to the pill", () => {
    expect(html).toContain("overflow-hidden");
    expect(html).toContain("rounded-full");
    expect(html).toContain("group-hover/button:scale-[15]");
  });

  // The coordination the variant could not have done: three elements reacting
  // to one hover, through the group the base Button already declares.
  it("drives the label and the icon from the button's own hover group", () => {
    expect(renderToString(<Button />)).toContain("group/button");
    expect(html).toContain("group-hover/button:text-foreground");
    expect(html).toContain("group-hover/button:bg-action-primary");
  });

  /**
   * --action-primary is a brand colour and free to be pale: it measured 1.05:1
   * against this app's surfaces on devkit light. The glyph on the hover circle
   * therefore has to be the foreground the palette pairs with it, not the app
   * background this pattern ships with.
   */
  it("puts the palette's own foreground on the accent circle", () => {
    expect(html).toContain("group-hover/button:text-action-primary-foreground");
    expect(html).not.toContain("group-hover/button:text-background");
  });

  it("moves on the app's motion scale, not on 500ms", () => {
    expect(html).toContain("duration-(--motion-slow)");
    expect(html).not.toContain("duration-500");
  });
});
