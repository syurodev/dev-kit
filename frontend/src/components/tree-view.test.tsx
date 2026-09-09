import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TreeView, type TreeNodeData } from "./tree-view";

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
    expect(html).toContain("padding-left:8px");
    expect(html).toContain("padding-left:20px");
    expect(html).toContain('aria-level="1"');
    expect(html).toContain('aria-level="2"');
  });

  it("gives a leaf no chevron", () => {
    const html = renderToString(<TreeView nodes={[{ id: "c", label: "id" }]} />);
    expect(chevrons(html)).toBe(0);
  });
});
