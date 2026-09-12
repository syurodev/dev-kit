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

/**
 * The `<th>`s that stand for a column. The gutter's corner cell is a `<th>`
 * too and answers to none of the rules below — it has no column to sort, to
 * drag or to act on.
 */
function columnHeaderTags(html: string): string[] {
  return headerTags(html).filter((th) => th.includes("data-column="));
}

describe("the header row's semantics", () => {
  // A26. useSortable's `attributes` set role="button" and tabIndex=0. On a
  // `<th>` that replaces the implicit columnheader role — the whole grid loses
  // its column semantics — and it nests the real sort <button> inside a
  // role="button" element, which is invalid ARIA. They exist for dnd-kit's
  // keyboard drag, which this grid has no sensor for.
  it("leaves a column header a columnheader, not a button", () => {
    const html = render({ onSort: () => {}, onReorder: () => {} });
    // One per column, plus the gutter's corner.
    expect(columnHeaderTags(html)).toHaveLength(2);
    expect(headerTags(html)).toHaveLength(3);
    for (const th of columnHeaderTags(html)) {
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

describe("a result with no rows", () => {
  // The screenshot that started this: columns resolved, zero rows, and the
  // grid drew a header row over blank space — indistinguishable from one that
  // is still loading.
  it("says so, and keeps the headers that say what would have been here", () => {
    const html = render({ result: { ...result, rows: [], rowCount: 0 } });
    expect(html).toContain('data-slot="empty"');
    expect(html).toContain("No rows");
    expect(columnHeaderTags(html)).toHaveLength(2);
  });

  // It lives inside the grid's horizontal scroller, so a wide table scrolled
  // sideways used to carry the message off-screen with it.
  it("stays put when a wide table is scrolled sideways", () => {
    const html = render({ result: { ...result, rows: [], rowCount: 0 } });
    expect(html).toContain("sticky left-0");
  });

  it("stays out of the way when there are rows", () => {
    expect(render()).not.toContain('data-slot="empty"');
  });
});

describe("the context menu's target", () => {
  // The menu rendered every item behind a `cell`, and only <td> carried a
  // data-cell — so right-clicking a column HEADER, which is where you go to
  // act on a column, opened an empty popup. An empty popup and a right-click
  // that did not register look exactly the same.
  it("marks a column header as a target of its own", () => {
    const html = render();
    expect(columnHeaderTags(html)).toHaveLength(2);
    for (const th of columnHeaderTags(html)) {
      expect(th, `${th} cannot be right-clicked to act on its column`).toContain("data-column=");
    }
  });

  it("still marks each body cell with its row and column", () => {
    expect(render()).toContain('data-cell="0:0"');
  });

  // All three selectors, or one of the targets is unreachable.
  it("looks for every target when the menu opens", () => {
    const source = readFileSync(join(import.meta.dir, "ResultGrid.tsx"), "utf8");
    expect(source).toContain('"[data-cell],[data-column],[data-row]"');
  });
});

describe("frozen columns", () => {
  const wide: QueryResult = {
    columns: ["id", "name", "note"],
    rows: [["1", "ada", "x"]],
    rowCount: 1,
    truncated: false,
  };

  /** Every `left:` a sticky cell declares, in render order. */
  function stickyLefts(html: string): string[] {
    return [...html.matchAll(/position:sticky;left:([^;"]*)/g)].map((m) => m[1]);
  }

  // Header and body both, at cumulative offsets: `left: 0` on all of them
  // stacks every frozen column on the same edge.
  // Header and body both, at cumulative offsets measured FROM the gutter: the
  // row numbers are sticky at 0, so a frozen run starting there would paint
  // underneath them. The two bare "0"s are the gutter's own cells.
  it("pins the first n columns where the ones before them end", () => {
    const html = render({ result: wide, widths: [80, 200, 200], frozen: 2 });
    expect(stickyLefts(html)).toEqual(["0", "44px", "124px", "0", "44px", "124px"]);
  });

  it("pins nothing but the gutter by default", () => {
    expect(stickyLefts(render({ result: wide }))).toEqual(["0", "0"]);
  });

  // A frozen cell has columns sliding under it, so a transparent one shows
  // both at once.
  it("gives a pinned cell a ground of its own", () => {
    const html = render({ result: wide, widths: [80, 200, 200], frozen: 1 });
    expect(html).toContain("background:linear-gradient");
  });

  // The bug: `--surface-well` is rgba(255,255,255,0.06) — a TINT, meant to sit
  // on a surface rather than be one. Painted onto a pinned cell it veiled the
  // columns sliding underneath instead of hiding them, so two columns' text
  // rendered on top of each other. The ground has to be opaque, which means
  // the tint layered over a base rather than used as one.
  it("grounds a pinned cell on an opaque colour, not on a tint", () => {
    const html = render({ result: wide, widths: [80, 200, 200], frozen: 1 });
    const pinned = html.slice(html.indexOf("position:sticky"));
    expect(pinned).toContain("var(--surface-base)");
    expect(pinned, "a translucent ground lets the scrolling columns read through")
      .toMatch(/linear-gradient\(var\(--surface-well\)/);
  });
});

describe("the sticky header", () => {
  // A `position: sticky` row group moves its CELLS; a background set on the
  // <thead> stays behind at the scroll origin. Styled that way the header goes
  // transparent the moment you scroll and the first row reads through it.
  it("carries its background on the cells, not the row group", () => {
    const html = render();
    const thead = html.slice(html.indexOf("<thead"), html.indexOf("</thead>"));
    expect(thead).toContain("sticky top-0");
    expect(thead.slice(0, thead.indexOf("<tr"))).not.toContain("bg-surface-raised");
    for (const th of headerTags(html)) {
      expect(th, `${th} would go transparent as soon as the grid scrolls`).toContain(
        "bg-surface-raised",
      );
    }
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

describe("the row-number gutter", () => {
  it("numbers the rows and marks each one as a target", () => {
    const html = render();
    expect(html).toContain('data-row="0"');
    expect(html).toContain('data-row="1"');
  });

  // The number is the row's place in the TABLE. Numbering from 1 on every page
  // makes "row 3" mean three different rows depending on where the pager is.
  it("counts from the page offset, not from the page", () => {
    // Read out of the gutter cells themselves: "1" also appears as a VALUE in
    // the id column, so matching it anywhere in the html proves nothing.
    const numbers = (html: string) =>
      [...html.matchAll(/data-row="\d+"[^>]*>(\d+)</g)].map((m) => m[1]);

    expect(numbers(render())).toEqual(["1", "2"]);
    expect(numbers(render({ rowOffset: 400 }))).toEqual(["401", "402"]);
  });

  // It is sticky at the left edge with columns sliding under it, so the same
  // rule the pinned cells live by applies: the ground has to be opaque.
  it("grounds the numbers on an opaque colour", () => {
    const gutter = render().slice(render().indexOf("data-row="));
    expect(gutter).toContain("var(--surface-base)");
  });
});

describe("the column header's glyphs", () => {
  // The type is what says whether a value can be compared, sorted or quoted,
  // and the grid had it in hand and drew nothing with it.
  it("draws a key on the key columns and a datatype on the rest", () => {
    const html = render({
      columnTypes: { id: "integer", name: "text" },
      keyColumns: ["id"],
    });
    expect(html).toContain("lucide-key-round");
    expect(html).toContain("lucide-type");
    // The key REPLACES the type glyph rather than joining it: two glyphs in a
    // 64px column leave no room for the name.
    expect(html).not.toContain("lucide-hash");
  });

  // A console result has no catalogue behind it — its columns can be
  // expressions — so it passes neither prop and must still render.
  it("claims nothing when the caller knows no types", () => {
    const html = render();
    expect(html).toContain("lucide-columns-3");
    expect(html).not.toContain("lucide-key-round");
  });
});

/**
 * Editing is the table tab's alone.
 *
 * A console result can be a join, an aggregate or an expression: there is no
 * single table behind it, and no key that names one of its rows. The grid is
 * shared, so the gate is which caller hands it `onEditCells` — asserted here
 * because a render cannot show it (React writes no handlers into HTML, so an
 * SSR check for one passes whatever the truth is).
 *
 * The SQL and every refusal are covered by sql/updateCell.test.ts; the
 * rectangle by grid/gridFocus.test.ts.
 */
describe("who may edit", () => {
  const read = (file: string) => readFileSync(join(import.meta.dir, "..", file), "utf8");

  it("is the table tab, and not the console", () => {
    expect(read("TableDataView.tsx")).toContain("onEditCells");
    expect(
      read("components/QueryConsole.tsx"),
      "a console result has no single table to write back to",
    ).not.toContain("onEditCells");
  });

  // The refusal is computed once from the connection and the table, not per
  // cell: without it a table with no key would build an UPDATE that matches
  // every row that happens to look the same.
  it("asks updateRefusal before it offers", () => {
    const body = read("TableDataView.tsx");
    expect(body).toContain("updateRefusal(");
    expect(body).toContain("editRefusal={editRefusal}");
  });
});
