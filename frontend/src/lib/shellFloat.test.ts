import { describe, expect, it } from "bun:test";
import { ignoreShellFloatInteraction } from "./shellFloat";

function outsideEvent(inFloat: boolean) {
  const event = {
    target: { closest: (selector: string) => (inFloat && selector === "[data-shell-float]" ? {} : null) },
    prevented: false,
    preventDefault() {
      event.prevented = true;
    },
  };
  return event;
}

describe("ignoreShellFloatInteraction", () => {
  it("keeps a dialog open when the click lands in a shell float, without calling the caller", () => {
    const event = outsideEvent(true);
    let called = false;
    ignoreShellFloatInteraction(event as never, () => {
      called = true;
    });
    expect(event.prevented).toBe(true);
    expect(called).toBe(false);
  });

  it("passes any other outside interaction to the caller untouched", () => {
    const event = outsideEvent(false);
    let called = false;
    ignoreShellFloatInteraction(event as never, () => {
      called = true;
    });
    expect(event.prevented).toBe(false);
    expect(called).toBe(true);
  });
});
