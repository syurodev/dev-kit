import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { VendorIcon } from "./VendorIcon";

describe("VendorIcon", () => {
  test("each shipped vendor has its own mark", () => {
    for (const vendor of ["claude", "codex", "opencode"]) {
      const html = renderToString(<VendorIcon vendor={vendor} />);
      expect(html).toContain(`data-vendor="${vendor}"`);
      expect(html).toContain("<path");
      expect(html).not.toContain("lucide-bot");
    }
  });

  test("opencode's mark is the supplied frame, on currentColor", () => {
    const html = renderToString(<VendorIcon vendor="opencode" />);
    expect(html).toContain('d="M22 24H2V0h20zM17 4.8H7v14.4h10z"');
    expect(html).toContain('fill="currentColor"');
  });

  test("a vendor nobody drew falls back to the generic mark, not someone else's", () => {
    const html = renderToString(<VendorIcon vendor="mystery" />);
    expect(html).toContain("lucide-bot");
    expect(html).not.toContain("data-vendor");
  });

  test("only Claude takes a colour while it works", () => {
    expect(renderToString(<VendorIcon vendor="claude" working />)).toContain("text-vendor-claude");
    expect(renderToString(<VendorIcon vendor="opencode" working />)).not.toContain("text-vendor-");
  });
});
