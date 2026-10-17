import { expect, test } from "bun:test";
import { DEFAULT_CODE_FONT_WEIGHT, codeFontWeightLabel, codeFontWeights, normalizeCodeFontWeight } from "./codeFontWeight";

test("Regular is the default and both steps keep their value", () => {
  expect(DEFAULT_CODE_FONT_WEIGHT).toBe(400);
  expect(normalizeCodeFontWeight(400)).toBe(400);
  expect(normalizeCodeFontWeight(500)).toBe(500);
});

test("anything else reads as Regular", () => {
  for (const bad of [undefined, null, "500", 300, 700, 450, NaN, {}]) expect(normalizeCodeFontWeight(bad)).toBe(400);
});

test("every step has a label", () => {
  expect(codeFontWeights.map((weight) => codeFontWeightLabel[weight])).toEqual(["Regular", "Medium"]);
});
