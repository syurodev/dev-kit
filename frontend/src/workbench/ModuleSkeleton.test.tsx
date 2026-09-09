import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ModuleSkeleton } from "./ModuleSkeleton";

describe("ModuleSkeleton", () => {
  it("paints a full module shape, not one line of text", () => {
    const html = renderToString(<ModuleSkeleton />);
    expect(html).toContain('role="status"');
    expect(html.match(/data-slot="skeleton"/g)?.length ?? 0).toBeGreaterThanOrEqual(12);
    // Shape only — no visible copy; the aria-label carries the meaning.
    expect(html.replace(/<[^>]+>/g, "").trim()).toBe("");
  });
});
