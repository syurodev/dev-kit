import { describe, expect, it } from "bun:test";
import { breakOffsets } from "./wrapBreaks";

/** Renders the offsets as `│` so an expectation reads like the wrapped line. */
function withBreaks(text: string): string {
  const offsets = new Set(breakOffsets(text));
  let out = "";
  for (let i = 0; i < text.length; i++) {
    if (offsets.has(i)) out += "│";
    out += text[i];
  }
  return out;
}

describe("breakOffsets", () => {
  it("breaks after an opening bracket and before a member access", () => {
    expect(withBreaks("@RequestHeader(SyncHeaderResolver.DEVICE_HEADER)")).toBe(
      "@RequestHeader(│SyncHeaderResolver│.DEVICE_HEADER)",
    );
  });

  it("breaks a call chain before each dot and method reference", () => {
    expect(withBreaks("summaries.stream().map(DeviceController::toResponse).toList()")).toBe(
      "summaries│.stream()│.map(│DeviceController│::toResponse)│.toList()",
    );
  });

  it("breaks after a comma only when no space follows", () => {
    expect(withBreaks("f(a,b, c)")).toBe("f(│a,│b, c)");
  });

  it("leaves numbers, ellipses and empty brackets whole", () => {
    expect(withBreaks("x = 1.5 + rest... + f() + a[]")).toBe("x = 1.5 + rest... + f() + a[]");
  });

  it("breaks a path after each slash", () => {
    expect(withBreaks('"/api/v1/devices"')).toBe('"/│api/│v1/│devices"');
  });
});
