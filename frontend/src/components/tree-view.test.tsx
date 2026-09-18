import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TreeView, flattenTree, pinnedRows, type TreeNodeData } from "./tree-view";

/** Counts chevrons — one per expandable row, whatever its state. */
function chevrons(html: string): number {
  return html.split("lucide-chevron-right").length - 1;
}

describe("TreeView", () => {
  it("keeps names primary and gives only the selected name medium weight", () => {
    const html = renderToString(<TreeView nodes={[
      { id: "selected", label: "Selected connection", selected: true },
      { id: "rest", label: "Other table" },
    ]} />);
    const labels = [...html.matchAll(/data-slot="tree-label" class="([^"]+)"/g)]
      .map((match) => match[1]);
    expect(labels).toHaveLength(2);
    expect(labels[0]).toContain("text-body-13-medium");
    expect(labels[1]).not.toContain("text-body-13-medium");
    for (const label of labels) expect(label).toContain("text-text-primary");
  });

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

  it("steps each row in by depth and reports the level to assistive tech", () => {
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
    // The step, not the literals: density gets tuned, and what must hold is
    // that depth still reads as depth.
    const lefts = [...html.matchAll(/left:(\d+)px;padding-left:var\(--space-tree-guide\)/g)].map((m) => Number(m[1]));
    expect(lefts.length).toBe(2);
    expect(lefts[1] - lefts[0]).toBeGreaterThan(6);
    expect(lefts[1] - lefts[0]).toBeLessThan(20);
    expect(html).toContain('aria-level="1"');
    expect(html).toContain('aria-level="2"');
  });

  it("opens a branch with no motion but the chevron's turn", () => {
    const html = renderToString(
      <TreeView
        nodes={[{ id: "s", label: "public", expandable: true, expanded: true, children: [{ id: "a", label: "a" }] }]}
      />,
    );
    expect(html).not.toContain("tree-branch");
    expect(html).not.toContain("tree-row-enter");
    expect(html).toContain("rotate-90");
  });

  it("gives a leaf no chevron", () => {
    const html = renderToString(<TreeView nodes={[{ id: "c", label: "id" }]} />);
    expect(chevrons(html)).toBe(0);
  });

  it("keeps trailing marks on screen and holds actions back until the row is hovered", () => {
    const html = renderToString(
      <TreeView
        nodes={[
          {
            id: "p",
            label: "orders-db",
            actions: <button type="button">more</button>,
            trailing: <span data-mark="">prod</span>,
          },
        ]}
      />,
    );
    // Actions with no count take no room until shown.
    expect(html).toMatch(/class="[^"]*hidden group-hover\/row:flex[^"]*"><button type="button">more/);
    // A status mark is not an action: nothing hides it.
    expect(html).toMatch(/<span data-mark="">prod<\/span>/);
    expect(html).not.toMatch(/hidden[^"]*"><span data-mark/);
  });
});

/** A connection pinned at row 0 with two schemas pinned at row 1, each holding tables. */
function database(tablesPerSchema: number): TreeNodeData[] {
  const schema = (name: string): TreeNodeData => ({
    id: name,
    label: name,
    sticky: 1,
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

  it("records, per level of each row's path, whether that node is the last child", () => {
    const rows = flattenTree([
      {
        id: "pg",
        label: "pg",
        expandable: true,
        expanded: true,
        children: [
          { id: "public", label: "public", expandable: true, expanded: true, children: [{ id: "a", label: "a" }] },
          { id: "crm", label: "crm", expandable: true, expanded: true, loading: true },
        ],
      },
      { id: "mysql", label: "mysql" },
    ]);
    expect(rows.map((row) => [row.key, row.lasts, row.open])).toEqual([
      ["/pg", [false], true],
      // public is not crm's last sibling, so the guide at pg's depth runs on past it.
      ["/pg/public", [false, false], true],
      ["/pg/public/a", [false, false, true], false],
      ["/pg/crm", [false, true], true],
      // The loading line is crm's only child, so it ends crm's guide.
      ["/pg/crm/…", [false, true, true], false],
      ["/mysql", [true], false],
    ]);
  });
});

describe("pinnedRows", () => {
  const rows = flattenTree(database(10)); // pg, public, 10 tables, crm, 10 tables
  const H = 24;

  it("pins nothing before the list has scrolled", () => {
    expect(pinnedRows(rows, 0, H)).toEqual([]);
  });

  it("pins the connection and the schema being scrolled through", () => {
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

describe("a long tree", () => {
  it("renders a window of its rows rather than all of them", () => {
    const html = renderToString(<TreeView nodes={database(300)} />);
    const rendered = html.split('role="treeitem"').length - 1;
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(100);
  });
});
