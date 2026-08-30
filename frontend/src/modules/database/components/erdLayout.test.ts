import { describe, expect, it } from "bun:test";
import { placeByHop } from "./ERDView";

const OPTS = { nodeWidth: 400, gap: 20 };

describe("placeByHop", () => {
  it("puts each hop in its own column", () => {
    const specs = [
      { tableName: "hub", height: 100 },
      { tableName: "a", height: 100 },
      { tableName: "b", height: 100 },
      { tableName: "far", height: 100 },
    ];
    const hops = { hub: 0, a: 1, b: 1, far: 2 };
    const neighbours = {
      hub: new Set(["a", "b"]),
      a: new Set(["hub", "far"]),
      b: new Set(["hub"]),
      far: new Set(["a"]),
    };

    const out = placeByHop(specs, hops, neighbours, OPTS);

    expect(out.get("hub")!.x).toBe(50);
    expect(out.get("a")!.x).toBe(450);
    expect(out.get("b")!.x).toBe(450);
    expect(out.get("far")!.x).toBe(850);
  });

  it("centres a short column against the tallest one", () => {
    const specs = [
      { tableName: "hub", height: 100 },
      { tableName: "a", height: 100 },
      { tableName: "b", height: 100 },
    ];
    const hops = { hub: 0, a: 1, b: 1 };
    const neighbours = { hub: new Set(["a", "b"]) };

    const out = placeByHop(specs, hops, neighbours, OPTS);

    // Neighbour column is 100 + 20 + 100 = 220 tall; the hub is 100.
    expect(out.get("a")!.y).toBe(50);
    expect(out.get("b")!.y).toBe(170);
    expect(out.get("hub")!.y).toBe(50 + (220 - 100) / 2);
  });

  // Without the ordering pass the second column falls in whatever order the
  // tables arrived in, and the edges between the columns braid across each
  // other instead of running parallel.
  it("orders a column by where its neighbours sit in the column before it", () => {
    const specs = [
      { tableName: "hub", height: 10 },
      { tableName: "p1", height: 10 },
      { tableName: "p2", height: 10 },
      // Declared in the order that would braid: c2 hangs off p2, c1 off p1.
      { tableName: "c2", height: 10 },
      { tableName: "c1", height: 10 },
    ];
    const hops = { hub: 0, p1: 1, p2: 1, c2: 2, c1: 2 };
    const neighbours = {
      hub: new Set(["p1", "p2"]),
      p1: new Set(["hub", "c1"]),
      p2: new Set(["hub", "c2"]),
      c1: new Set(["p1"]),
      c2: new Set(["p2"]),
    };

    const out = placeByHop(specs, hops, neighbours, OPTS);

    expect(out.get("p1")!.y).toBeLessThan(out.get("p2")!.y);
    expect(out.get("c1")!.y).toBeLessThan(out.get("c2")!.y);
  });
});
