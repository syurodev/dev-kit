import { describe, expect, it } from "bun:test";
import { toStep } from "./codegraph";

describe("toStep", () => {
  it("keeps how a frame leaves, and leaves it out when it does not", () => {
    const step = toStep({
      line: 4, col: 3, text: "x()", calleeKey: "#x", to: "", target: "", impls: [], visible: true, reason: "side effect",
      frames: [
        { id: "2:5", kind: "catch", text: "catch E", exits: true, exit: "throw" },
        { id: "3:5", kind: "if", text: "if a", exits: false, exit: "" },
      ],
    } as unknown as Parameters<typeof toStep>[0]);
    expect(step.frames).toEqual([
      { id: "2:5", kind: "catch", text: "catch E", exits: true, exit: "throw" },
      { id: "3:5", kind: "if", text: "if a", exits: undefined, exit: undefined },
    ]);
  });
});
