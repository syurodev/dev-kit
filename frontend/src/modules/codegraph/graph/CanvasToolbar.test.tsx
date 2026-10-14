import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { CanvasToolbar } from "./CanvasToolbar";

describe("CanvasToolbar", () => {
  test("offers only the tools that do something", () => {
    const html = renderToString(<CanvasToolbar onFit={() => {}} fitActive />);
    expect(html).toContain('aria-label="Fit to view"');
    expect(html).not.toContain('aria-label="Search nodes"');
    expect(html).not.toContain('aria-label="Expand all"');
  });
});
