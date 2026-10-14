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
    const body = html.split("<tbody")[1];
    expect(bodyRows(html)).toBeLessThan(500);
    // The first screen is drawn before the scroller is measured, and the rows
    // below it are a spacer holding their height.
    expect((body.match(/<td/g) ?? []).length).toBeGreaterThan(0);
    expect(body).toMatch(/<tr style="height:\d+px" aria-hidden="true"><\/tr><\/tbody>/);
  });

  // Windowing estimates one height for every row, so a windowed row may not
  // wrap: its cells truncate and say the whole value on hover.
  test("a windowed table keeps every row one line high", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(500)} />);
    const cell = html.split("<tbody")[1].split("<td")[2];
    expect(cell).toContain("truncate");
    expect(cell).toContain("whitespace-nowrap");
    expect(cell).toContain('title="row 0"');
    const short = renderToString(<DataTable columns={columns} rows={rows(3)} />);
    expect(short.split("<tbody")[1].split("<td")[2]).not.toContain("truncate");
  });

  test("a cell that cannot wrap says its whole value on hover", () => {
    const html = renderToString(
      <DataTable columns={[{ key: "n", header: "n", nowrap: true }]} rows={[["a very long name"]]} />,
    );
    expect(html).toContain('title="a very long name"');
  });

  test("ends its box at the last column, scrolling sideways past the room", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(3)} />);
    const wrapper = html.slice(0, html.indexOf(">"));
    expect(wrapper).toContain("w-fit");
    expect(wrapper).toContain("max-w-full");
    expect(wrapper).toContain("overflow-auto");
  });

  test("a resize handle states its range in pixels", () => {
    const html = renderToString(
      <DataTable columns={columns} rows={rows(3)} initialWidths={[90, 210]} />,
    );
    expect(html).toContain('aria-valuemin="64"');
    expect(html).toContain('aria-valuemax="1200"');
    expect(html).toContain('aria-valuetext="90 pixels"');
    expect(html).toContain("touch-none");
  });

  test("clickable rows are focusable, keyboard-operable and show focus", () => {
    const html = renderToString(
      <DataTable columns={columns} rows={rows(2)} onRowClick={() => {}} />,
    );
    const body = html.split("<tbody")[1];
    expect(body.match(/<tr tabindex="0"/g)?.length).toBe(2);
    expect(body.match(/role="button"/g)?.length).toBe(2);
    expect(body).toContain('aria-keyshortcuts="Enter Space"');
    expect(body).toContain("focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset");
    const still = renderToString(<DataTable columns={columns} rows={rows(2)} />);
    expect(still).not.toContain('role="button"');
    expect(still).not.toContain("tabindex=\"0\" aria-keyshortcuts");
  });

  test("clickable rows can be named, and say which one is open", () => {
    const html = renderToString(
      <DataTable
        columns={columns}
        rows={rows(3)}
        onRowClick={() => {}}
        rowAriaLabel={(row, i) => `Message ${i} ${String(row[0])}`}
        expandedRow={1}
      />,
    );
    const trs = html.split("<tbody")[1].split("<tr").slice(1);
    expect(trs[0]).toContain('aria-label="Message 0 0"');
    expect(trs[0]).toContain('aria-expanded="false"');
    expect(trs[1]).toContain('aria-expanded="true"');
    expect(trs[1]).toContain('data-expanded="true"');
    expect(trs[1]).toContain("bg-(--state-soft)");
    expect(trs[0].split(">")[0]).not.toContain(" bg-(--state-soft)");
    // Without expandedRow nothing claims to expand.
    const plain = renderToString(<DataTable columns={columns} rows={rows(2)} onRowClick={() => {}} />);
    expect(plain).not.toContain("aria-expanded");
  });

  test("keeps mono and alignment on cells, with a caption when given", () => {
    const html = renderToString(<DataTable columns={columns} rows={rows(2)} caption="Users" />);
    expect(html).toContain("text-code-12");
    expect(html).toContain("text-right");
    expect(html).toContain("<caption");
    expect(html).toContain("Users");
  });
});
