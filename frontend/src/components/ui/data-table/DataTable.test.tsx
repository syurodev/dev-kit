import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { DataTable, type DataTableColumn } from "./DataTable";

const columns: DataTableColumn[] = [
  { key: "id", header: "id", align: "right", mono: true },
  { key: "name", header: "name" },
];

function rows(n: number): (string | null)[][] {
  return Array.from({ length: n }, (_, i) => [String(i), i === 1 ? null : `row ${i}`]);
}

const bodyRows = (html: string) => (html.split("<tbody")[1]?.match(/<tr/g) ?? []).length;

describe("DataTable", () => {
  test("lays out fixed columns from a colgroup", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(3)} />);
    expect(html).toContain("table-fixed");
    expect(html).toContain("<colgroup>");
    expect(html.match(/<col /g)?.length).toBe(2);
  });

  test("respects the widths it is given", () => {
    const html = renderToString(
      <DataTable columns={columns} rows={rows(3)} initialWidths={[90, 210]} />,
    );
    expect(html).toContain('<col style="width:90px"/>');
    expect(html).toContain('<col style="width:210px"/>');
    expect(html).toContain('aria-valuenow="90"');
    expect(html).toContain('aria-valuenow="210"');
  });

  test("gives every header a focusable resize handle", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(3)} />);
    expect(html.match(/role="separator"/g)?.length).toBe(2);
    expect(html.match(/aria-orientation="vertical"/g)?.length).toBe(2);
    expect(html).toContain('aria-label="Resize id"');
    expect(html.match(/tabindex="0"/g)?.length).toBe(2);
    expect(html).toContain("cursor-col-resize");
  });

  test("sizes columns from string content when no widths are given", () => {
    const html = renderToString(
      <DataTable
        columns={[{ key: "a", header: "a" }, { key: "b", header: "b" }]}
        rows={[["5", "2026-03-03 09:21:31 a long value"]]}
      />,
    );
    const widths = [...html.matchAll(/<col style="width:(\d+)px"\/>/g)].map((m) => Number(m[1]));
    expect(widths).toHaveLength(2);
    expect(widths[0]).toBeLessThan(widths[1]);
  });

  test("draws NULL for a null cell", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(3)} />);
    expect(html).toContain("NULL");
    expect(html).toContain("italic");
  });

  test("renders a short table whole", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(100)} />);
    expect(bodyRows(html)).toBe(100);
  });

  test("windows a long table instead of rendering every row", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(500)} />);
    expect(bodyRows(html)).toBeLessThan(500);
  });

  test("keeps mono and alignment on cells, with a caption when given", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(2)} caption="Users" />);
    expect(html).toContain("text-code-12");
    expect(html).toContain("text-right");
    expect(html).toContain("<caption");
    expect(html).toContain("Users");
  });
});
