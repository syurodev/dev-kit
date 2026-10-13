import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { PaletteSwatch } from "./PaletteSwatch";
import { FAMILIES } from "@/theme/families";

describe("PaletteSwatch", () => {
  test("shows eight hues and a line of code in the palette's colours", () => {
    const html = renderToString(<PaletteSwatch family="dracula" mode="dark" />);
    expect(html.match(/data-hue=/g)).toHaveLength(8);
    expect(html).toContain(`--hue-red:${FAMILIES.dracula.dark["--hue-red"]}`);
    expect(html).toContain("text-(--syntax-keyword)");
    expect(html).toContain("text-(--syntax-string)");
  });

  test("never paints chrome", () => {
    const html = renderToString(<PaletteSwatch family="dracula" mode="dark" />);
    expect(html).not.toContain("bg-action-primary");
    expect(html).not.toContain("--surface-base:");
  });
});
