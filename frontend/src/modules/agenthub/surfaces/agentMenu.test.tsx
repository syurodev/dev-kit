import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { MenuTrailing } from "@/components/ui/menu-row";

describe("a menu row's trailing detail", () => {
  it("truncates rather than overflowing the row", () => {
    // The slot is one line at the end of a fixed-height row. A caller that put
    // a sentence in it — the Agent Hub's command menu put a skill's whole
    // description there — overflowed the row and drew over its neighbours.
    const html = renderToString(<MenuTrailing detail="a sentence that is much too long for one row" />);
    expect(html).toContain("truncate");
    // An absolute cap, not a share of the row: the slot is an auto column, and a
    // percentage of it is taken of the width the text asked for, which halved
    // the text and left the column — and the label's squeeze — at full width.
    expect(html).toContain("max-w-40");
    expect(html).not.toContain("max-w-1/2");
  });

  it("prefers a shortcut when a row has both", () => {
    const html = renderToString(<MenuTrailing shortcut="⌘K" detail="ignored" />);
    expect(html).toContain("⌘K");
    expect(html).not.toContain("ignored");
  });
});
