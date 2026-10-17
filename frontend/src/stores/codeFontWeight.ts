/**
 * The weight of the code view's text (CodeMirror). Two steps, not a slider: Geist
 * Mono is a variable font and could take any weight, but a system font the person
 * picks (JetBrains Mono, SF Mono…) usually has Regular and Medium installed and
 * nothing in between, so only those two are offered.
 */
export const codeFontWeights = [400, 500] as const;
export type CodeFontWeight = (typeof codeFontWeights)[number];

export const DEFAULT_CODE_FONT_WEIGHT: CodeFontWeight = 400;

export const codeFontWeightLabel: Record<CodeFontWeight, string> = { 400: "Regular", 500: "Medium" };

/** A stored value that is not one of the steps (an old or hand-edited file) reads as Regular. */
export function normalizeCodeFontWeight(value: unknown): CodeFontWeight {
  return codeFontWeights.find((weight) => weight === value) ?? DEFAULT_CODE_FONT_WEIGHT;
}
