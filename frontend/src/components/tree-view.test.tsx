import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TreeView, flattenTree, pinnedRows, type TreeNodeData } from "./tree-view";

/** Counts chevrons — one per expandable row, whatever its state. */
function chevrons(html: string): number {
  return html.split("lucide-chevron-right").length - 1;
}

describe("TreeView", () => {
  it("draws a chevron for a node whose children have not arrived yet", () => {
    // The whole point of `expandable` being separate from `children`: a table
    // gets its columns only once it is opened, so a tree that infers "leaf"
    // from an empty children array leaves no way to open it.
    const html = renderToString(
      <TreeView nodes={[{ id: "t", label: "ba_account", expandable: true }]} />,
    );
    expect(chevrons(html)).toBe(1);
  });

  it("shows the loading line, then the empty line, under an opened node with nothing in it", () => {
    const node: TreeNodeData = {
      id: "t",
      label: "ba_account",
      expandable: true,
      expanded: true,
      loading: true,
      emptyLabel: "No columns",
    };
    expect(renderToString(<TreeView nodes={[node]} />)).toContain("Loading…");
    expect(renderToString(<TreeView nodes={[{ ...node, loading: false }]} />)).toContain(
      "No columns",
    );
  });

  it("renders children only while the node says it is expanded", () => {
    const node: TreeNodeData = {
      id: "s",
      label: "public",
      expandable: true,
      children: [{ id: "c", label: "ba_account" }],
    };
    expect(renderToString(<TreeView nodes={[node]} />)).not.toContain("ba_account");
    expect(renderToString(<TreeView nodes={[{ ...node, expanded: true }]} />)).toContain(
      "ba_account",
    );
  });

  it("steps the indent by depth and reports the level to assistive tech", () => {
    const html = renderToString(
      <TreeView
        nodes={[
          {
            id: "a",
            label: "folder",
            expandable: true,
            expanded: true,
            children: [{ id: "b", label: "connection" }],
          },
        ]}
      />,
    );
    // The step, not the two literals: density gets tuned, and a test that
    // pins the numbers only ever reports that someone tuned it. What must
    // hold is that depth still reads as depth.
    const pads = [...html.matchAll(/padding-left:(\d+)px/g)].map((m) => Number(m[1]));
    expect(pads.length).toBe(2);
    const step = pads[1] - pads[0];
    expect(step).toBeGreaterThan(6);
    expect(step).toBeLessThan(20);
    expect(html).toContain('aria-level="1"');
    expect(html).toContain('aria-level="2"');
  });

  it("wraps children in an animating branch and numbers rows for the stagger", () => {
    const html = renderToString(
      <TreeView
        nodes={[
          {
            id: "s",
            label: "public",
            expandable: true,
            expanded: true,
            children: [
              { id: "a", label: "ba_account" },
              { id: "b", label: "ba_user" },
            ],
          },
        ]}
      />,
    );
    // The CSS in index.css keys off exactly these: the branch class for the
    // 0fr→1fr track, role=group as the clipped grid item, --i for the delay.
    expect(html).toContain('class="tree-branch"');
    expect(html).toContain('role="group"');
    expect(html).toContain("--i:1");
    expect(html).not.toContain("data-closing");
  });

  it("gives a leaf no chevron", () => {
    const html = renderToString(<TreeView nodes={[{ id: "c", label: "id" }]} />);
    expect(chevrons(html)).toBe(0);
  });
});

/** A connection pinned at 0 with two schemas pinned at 24, each holding tables. */
function database(tablesPerSchema: number): TreeNodeData[] {
  const schema = (name: string): TreeNodeData => ({
    id: name,
    label: name,
    sticky: 24,
    expandable: true,
    expanded: true,
    children: Array.from({ length: tablesPerSchema }, (_, i) => ({ id: `t${i}`, label: `${name}_${i}` })),
  });
  return [
    { id: "pg", label: "pg", sticky: 0, expandable: true, expanded: true, children: [schema("public"), schema("crm")] },
  ];
}

describe("flattenTree", () => {
  it("lists what is showing, with subtree ends and a line under an empty opened node", () => {
    const rows = flattenTree([
      { id: "a", label: "a", expandable: true, expanded: true, children: [{ id: "b", label: "b" }] },
      { id: "c", label: "c", expandable: true, expanded: true, loading: true },
      { id: "d", label: "d", expandable: true, children: [{ id: "hidden", label: "x" }] },
    ]);
    expect(rows.map((row) => [row.key, row.level, row.end, !!row.placeholder])).toEqual([
      ["/a", 0, 2, false],
      ["/a/b", 1, 2, false],
      ["/c", 0, 4, false],
      ["/c/…", 1, 4, true],
      ["/d", 0, 5, false],
    ]);
  });

  it("keys by path, so the same id under two parents stays two rows", () => {
    const keys = flattenTree(database(2)).map((row) => row.key);
    expect(new Set(keys).size).toBe(keys.length);
  });
});

describe("pinnedRows", () => {
  const rows = flattenTree(database(10)); // pg, public, 10 tables, crm, 10 tables
  const H = 24;

  it("pins nothing before the list has scrolled", () => {
    expect(pinnedRows(rows, 0, H)).toEqual([]);
  });

  it("pins the connection and the schema being scrolled through", () => {
    // 5 rows down: pg and public have gone above their lines.
    expect(pinnedRows(rows, 5 * H, H)).toEqual([
      { index: 0, top: 0 },
      { index: 1, top: 24 },
    ]);
  });

  it("slides a schema out as the next one arrives, and pins that one instead", () => {
    // public's subtree ends at row 12, where crm sits; 10.5 rows down crm is
    // at 36, half a row below the schema line, pushing public up by half a row.
    expect(pinnedRows(rows, 10.5 * H, H)).toEqual([
      { index: 0, top: 0 },
      { index: 1, top: 12 },
    ]);
    expect(pinnedRows(rows, 13 * H, H)).toEqual([
      { index: 0, top: 0 },
      { index: 12, top: 24 },
    ]);
  });
});

describe("a tree past the virtualization threshold", () => {
  it("renders a window of its rows rather than all of them", () => {
    const html = renderToString(<TreeView nodes={database(300)} />);
    const rendered = html.split('role="treeitem"').length - 1;
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(100);
    // Flat, so no animated branches to key off.
    expect(html).not.toContain("tree-branch");
  });
});
