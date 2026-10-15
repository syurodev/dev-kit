import { describe, expect, it } from "bun:test";
import { arrowLabel, connectorEdgeData } from "./ConnectorEdge";

describe("arrowLabel", () => {
  it("is the order number of a numbered call", () => {
    expect(arrowLabel("2")).toBe("2");
    expect(arrowLabel("12 · if (order.isValid())")).toBe("12");
  });
  it("is nothing for a label that is not numbered, which stays at the midpoint", () => {
    expect(arrowLabel("implements")).toBeNull();
    expect(arrowLabel("only when type == REFUND")).toBeNull();
    expect(arrowLabel("3 retries before giving up")).toBeNull();
    expect(arrowLabel(undefined)).toBeNull();
  });
});

describe("connectorEdgeData", () => {
  it("puts labels at the arrowhead only when the canvas asks for it", () => {
    const asked = connectorEdgeData({ kind: "call", label: "2 · if (ok)", title: "ok", labelAt: "arrow" });
    expect(asked.labelAt).toBe("arrow");
    expect(asked.title).toBe("ok");
    expect(asked.showLabel).toBe(true);
  });
  it("keeps a midpoint label when not asked, even with a tooltip", () => {
    const plan = connectorEdgeData({ kind: "call", label: "2 · if (ok)", title: "ok" });
    expect(plan.labelAt).toBeUndefined();
    expect(plan.title).toBe("ok");
  });
  it("titles a label by itself when there is no tooltip, and shows an injection's", () => {
    expect(connectorEdgeData({ kind: "call", label: "only when REFUND" }).title).toBe("only when REFUND");
    expect(connectorEdgeData({ kind: "call" }).showLabel).toBe(false);
    expect(connectorEdgeData({ kind: "di" }).showLabel).toBe(true);
  });
});
