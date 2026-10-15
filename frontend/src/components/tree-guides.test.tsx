import { describe, expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { TREE_CHEVRON, TREE_ROW_HEIGHT, TreeGuides, treeContentX, treeLineX } from "./tree-guides";

const spans = (html: string) => [...html.matchAll(/<span style="([^"]+)"><\/span>/g)].map((m) => m[1]);

describe("TreeGuides", () => {
  it("draws nothing for a closed top row", () => {
    expect(spans(renderToStaticMarkup(<TreeGuides level={0} lasts={[true]} open={false} leaf rowHeight={TREE_ROW_HEIGHT} />))).toEqual([]);
  });

  it("runs a line for each ancestor branch that continues, then an elbow into the row", () => {
    // A grandchild with siblings below it, whose parent has siblings below it too.
    const html = renderToStaticMarkup(<TreeGuides level={2} lasts={[true, false, false]} open={false} leaf={false} rowHeight={28} />);
    const [outer, line, elbow] = spans(html);
    expect(outer).toContain(`left:${treeLineX(0)}px;top:0;bottom:0`);
    expect(line).toContain(`left:${treeLineX(1)}px;top:0;bottom:0`);
    expect(elbow).toContain(`left:${treeLineX(1)}px;top:0;width:${treeContentX(2) - treeLineX(1)}px;height:14px`);
    expect(elbow).toContain("border-bottom-left-radius:6px");
    expect(html).toContain('aria-hidden="true"');
  });

  it("stops the lines of branches that end: a last child gets only its elbow", () => {
    const html = renderToStaticMarkup(<TreeGuides level={2} lasts={[false, true, true]} open={false} leaf={false} rowHeight={28} />);
    const all = spans(html);
    expect(all).toHaveLength(1);
    expect(all[0]).toContain("border-bottom-left-radius");
  });

  it("takes a leaf's elbow through its empty chevron slot", () => {
    const [elbow] = spans(renderToStaticMarkup(<TreeGuides level={1} lasts={[true, true]} open={false} leaf rowHeight={28} />));
    expect(elbow).toContain(`width:${treeContentX(1) - treeLineX(0) + TREE_CHEVRON}px`);
  });

  it("drops a stub from an open row's chevron", () => {
    const [stub] = spans(renderToStaticMarkup(<TreeGuides level={0} lasts={[true]} open leaf={false} rowHeight={28} />));
    expect(stub).toContain(`left:${treeLineX(0)}px;top:${14 + TREE_CHEVRON / 2}px;bottom:0`);
  });
});
