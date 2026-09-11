import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import type { QueryResult } from "@/bridge/database";
import { ResultGrid } from "./ResultGrid";

const result: QueryResult = {
  columns: ["id", "name"],
  rows: [
    ["1", "ada"],
    ["2", "grace"],
  ],
  rowCount: 2,
  truncated: false,
};

/** The grid with only the props both callers pass. */
function render(extra: Partial<Parameters<typeof ResultGrid>[0]> = {}): string {
  return renderToString(
    <ResultGrid
      result={result}
      widths={[]}
      columnOrder={[]}
      onResize={() => {}}
      onInspectCell={() => {}}
      {...extra}
    />,
  );
}

/** Every `<th>` the grid renders, as its opening tag. `<thead>` is not one. */
function headerTags(html: string): string[] {
  return html.match(/<th(?=[\s>])[^>]*>/g) ?? [];
}

describe("the header row's semantics", () => {
  // A26. useSortable's `attributes` set role="button" and tabIndex=0. On a
  // `<th>` that replaces the implicit columnheader role — the whole grid loses
  // its column semantics — and it nests the real sort <button> inside a
  // role="button" element, which is invalid ARIA. They exist for dnd-kit's
  // keyboard drag, which this grid has no sensor for.
  it("leaves a column header a columnheader, not a button", () => {
    const html = render({ onSort: () => {}, onReorder: () => {} });
    // The gutter header plus one per column.
    expect(headerTags(html)).toHaveLength(3);
    for (const th of headerTags(html)) {
      expect(th, `${th} overrides the cell's columnheader role`).not.toContain('role="button"');
      expect(th, `${th} puts the header itself in the tab order`).not.toContain("tabindex");
    }
    // The sort control is still there, and it is the button.
    expect(html).toContain('<button type="button"');
    expect(html).toContain("ada");
  });

  it("keeps the resize handle a separator of its own", () => {
    const html = render();
    expect(html).toContain('aria-label="Resize id"');
    expect(html).toContain('role="separator"');
  });
});

describe("the column drag", () => {
  const source = readFileSync(join(import.meta.dir, "ResultGrid.tsx"), "utf8");

  // Listeners are pointer handlers, so they leave no trace in rendered HTML
  // and the assertion has to be made against the source. The pointer drag is
  // the drag this grid actually has: dropping these would take it away.
  it("still hands the header cell its pointer listeners", () => {
    expect(source).toContain("{...listeners}");
    expect(source).not.toContain("{...attributes}");
  });

  // If this ever registers a KeyboardSensor, `attributes` stop being dead
  // weight and have to come back with it — and the sort button has to stop
  // being a child of the drag target at the same time. The assertion is on the
  // registration, not the name: the comment above the `<th>` names the sensor
  // precisely so the next person knows what would bring the attributes back.
  it("registers a pointer sensor and no keyboard sensor", () => {
    expect(source).toContain("useSensor(PointerSensor");
    expect(source).not.toMatch(/useSensor\(\s*KeyboardSensor/);
  });
});
