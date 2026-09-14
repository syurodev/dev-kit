import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { AnimatedCollapse } from "./animated-collapse";

describe("AnimatedCollapse", () => {
  it("renders what is open, with the spacing inside the part that animates", () => {
    const html = renderToString(
      <AnimatedCollapse open className="pt-3">
        <p>output</p>
      </AnimatedCollapse>,
    );
    expect(html).toContain("output");
    expect(html).toContain('data-slot="animated-collapse"');
    // Clipped while its height moves, or content would spill over the rows below.
    expect(html).toContain("overflow:hidden");
    expect(html).toContain('class="pt-3"');
  });

  it("renders nothing at all while closed", () => {
    expect(
      renderToString(
        <AnimatedCollapse open={false}>
          <p>output</p>
        </AnimatedCollapse>,
      ),
    ).toBe("");
  });
});
