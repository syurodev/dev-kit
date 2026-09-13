import { describe, expect, it } from "bun:test";
import { flattenWithGuides, type TreeNextNode } from "./tree-view-next";

describe("flattenWithGuides", () => {
  it("records, per level of each row's path, whether that node is the last child", () => {
    const nodes: TreeNextNode[] = [
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
    ];
    const rows = flattenWithGuides(nodes);
    expect(rows.map((row) => [row.key, row.lasts, row.open])).toEqual([
      ["/pg", [false], true],
      // public is not crm's last sibling, so the guide at pg's depth runs on past it.
      ["/pg/public", [false, false], true],
      ["/pg/public/a", [false, false, true], false],
      ["/pg/crm", [false, true], true],
      // The loading line is the only child of crm, so it ends crm's guide.
      ["/pg/crm/…", [false, true, true], false],
      ["/mysql", [true], false],
    ]);
  });
});
