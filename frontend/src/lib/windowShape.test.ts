import { describe, expect, it } from "bun:test";
import { radiusFromPayload } from "./windowShape";

describe("radiusFromPayload", () => {
  it("takes the radius the backend reports", () => {
    expect(radiusFromPayload({ radius: 16 })).toBe(16);
    // Wails can deliver a single emitted value wrapped in an array.
    expect(radiusFromPayload([{ radius: 8 }])).toBe(8);
  });

  it("ignores anything that is not a plausible radius, so the CSS default stands", () => {
    expect(radiusFromPayload({ radius: 0 })).toBeNull();
    expect(radiusFromPayload({ radius: -1 })).toBeNull();
    expect(radiusFromPayload({ radius: 500 })).toBeNull();
    expect(radiusFromPayload({ radius: "16" })).toBeNull();
    expect(radiusFromPayload(null)).toBeNull();
  });
});
