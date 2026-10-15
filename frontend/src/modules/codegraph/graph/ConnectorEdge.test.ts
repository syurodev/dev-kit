import { describe, expect, it } from "bun:test";
import { arrowLabel } from "./ConnectorEdge";

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
