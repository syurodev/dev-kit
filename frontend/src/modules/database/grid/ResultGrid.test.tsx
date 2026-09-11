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
    // One per column. There is no gutter header: the row-number column went
    // when the pager learned to name the page itself.
    expect(headerTags(html)).toHaveLength(2);
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

describe("a result with no rows", () => {
  // The screenshot that started this: columns resolved, zero rows, and the
  // grid drew a header row over blank space — indistinguishable from one that
  // is still loading.
  it("says so, and keeps the headers that say what would have been here", () => {
    const html = render({ result: { ...result, rows: [], rowCount: 0 } });
    expect(html).toContain('data-slot="empty"');
    expect(html).toContain("No rows");
    expect(headerTags(html)).toHaveLength(2);
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
    for (const th of headerTags(html)) {
      expect(th, `${th} cannot be right-clicked to act on its column`).toContain("data-column=");
    }
  });

  it("still marks each body cell with its row and column", () => {
    expect(render()).toContain('data-cell="0:0"');
  });

  // Both selectors, or the header target is unreachable.
  it("looks for either when the menu opens", () => {
    const source = readFileSync(join(import.meta.dir, "ResultGrid.tsx"), "utf8");
    expect(source).toContain('closest("[data-cell],[data-column]")');
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
  it("pins the first n columns where the ones before them end", () => {
    const html = render({ result: wide, widths: [80, 200, 200], frozen: 2 });
    expect(stickyLefts(html)).toEqual(["0", "80px", "0", "80px"]);
  });

  it("pins nothing by default", () => {
    expect(stickyLefts(render({ result: wide }))).toEqual([]);
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

describe("the row-number gutter, now gone", () => {
  // It numbered rows from the page offset, which only ever restated what the
  // pager already said an inch away. Its width was 56px of every table.
  it("leaves the first column as the table's own", () => {
    const html = render();
    expect(html).not.toContain(">#<");
    expect(html).toContain("<col");
    expect(html.indexOf("id")).toBeLessThan(html.indexOf("ada"));
  });
});
