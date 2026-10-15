import { describe, expect, test } from "bun:test";
import { METHOD_CHIP_CLASSES, NEUTRAL_METHOD_CHIP, methodChipClass, methodHue, methodTextClass } from "./httpMethod";

describe("HTTP method colour (Postman's convention)", () => {
  test.each([
    ["GET", "--hue-green"],
    ["POST", "--hue-yellow"],
    ["PUT", "--hue-blue"],
    ["PATCH", "--hue-blue"],
    ["DELETE", "--hue-red"],
  ] as const)("%s is %s", (method, hue) => {
    expect(methodHue(method)).toBe(hue);
    expect(methodChipClass(method)).toBe(METHOD_CHIP_CLASSES[hue]);
    expect(methodChipClass(method)).toContain(`text-(${hue})`);
    expect(methodTextClass(method)).toBe(`text-(${hue})`);
  });

  test("case and stray whitespace do not change the colour", () => {
    expect(methodHue("get")).toBe("--hue-green");
    expect(methodHue(" Delete ")).toBe("--hue-red");
  });

  // HEAD/OPTIONS, and CodeGraph's non-HTTP entry verbs, are not a kind the
  // palette distinguishes.
  test.each(["HEAD", "OPTIONS", "TRACE", "MESSAGE", "ACCESS", "CRON", "GRPC", "ALL", ""])("%s is neutral", (verb) => {
    expect(methodHue(verb)).toBeNull();
    expect(methodChipClass(verb)).toBe(NEUTRAL_METHOD_CHIP);
    expect(methodTextClass(verb)).toBe("text-text-secondary");
  });

  test("a chip is tinted, never a solid fill with dark text", () => {
    for (const [hue, cls] of Object.entries(METHOD_CHIP_CLASSES)) {
      expect(cls).toMatch(new RegExp(String.raw`^bg-\[color-mix\(in_srgb,var\(${hue}\)_\d+%,var\(--surface-base\)\)\] text-\(${hue}\)$`));
      expect(cls).not.toContain("action-primary-foreground");
    }
  });

  test("every chip uses one tint percentage", () => {
    const tints = new Set(Object.values(METHOD_CHIP_CLASSES).map((cls) => cls.match(/_(\d+)%/)?.[1]));
    expect(tints.size).toBe(1);
  });
});
