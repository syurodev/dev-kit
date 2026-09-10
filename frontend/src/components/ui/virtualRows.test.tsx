import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { VIRTUALIZE_THRESHOLD, shouldVirtualize, useVirtualRows } from "./virtual-rows";

describe("virtualization threshold", () => {
  it("windows only past the documented row count", () => {
    expect(shouldVirtualize(0)).toBe(false);
    expect(shouldVirtualize(1)).toBe(false);
    expect(shouldVirtualize(VIRTUALIZE_THRESHOLD)).toBe(false);
    expect(shouldVirtualize(VIRTUALIZE_THRESHOLD + 1)).toBe(true);
    expect(shouldVirtualize(500)).toBe(true);
  });
});

/** A table of the shape the three real ones share, small enough to read. */
function Table({ rowCount }: { rowCount: number }) {
  const rows = useVirtualRows(rowCount, 36);
  return (
    <div ref={rows.scrollRef}>
      <table>
        <thead>
          <tr>
            <th>n</th>
          </tr>
        </thead>
        <tbody>
          {rows.padTop > 0 && <tr style={{ height: rows.padTop }} aria-hidden />}
          {rows.indices.map((i) => (
            <tr key={i}>
              <td>{i}</td>
            </tr>
          ))}
          {rows.padBottom > 0 && <tr style={{ height: rows.padBottom }} aria-hidden />}
        </tbody>
      </table>
    </div>
  );
}

describe("a virtualized table's first paint", () => {
  it("renders a short table whole, with no spacers", () => {
    const html = renderToString(<Table rowCount={5} />);
    // Five data rows plus the header row.
    expect(html.match(/<tr/g)?.length).toBe(6);
    expect(html).not.toContain("aria-hidden");
    for (const n of [0, 1, 2, 3, 4]) expect(html).toContain(`<td>${n}</td>`);
  });

  it("keeps the table element, so sticky headers and table-fixed still apply", () => {
    const html = renderToString(<Table rowCount={5} />);
    expect(html).toContain("<thead>");
    expect(html).toContain("<tbody>");
    // Rows stay in flow — absolute positioning is what would break both.
    expect(html).not.toContain("position:absolute");
  });
});
