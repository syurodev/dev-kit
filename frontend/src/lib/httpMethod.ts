/**
 * How an HTTP method is coloured, everywhere it is drawn: the API client's
 * rail and method picker, CodeGraph's entry points and flow header.
 *
 * One convention, Postman's: GET green, POST yellow, PUT/PATCH blue, DELETE
 * red, and anything else (HEAD, OPTIONS, a Kong phase, a Kafka topic, CRON)
 * neutral. The colour says which method it is, not how safe it is, so it is a
 * Color palette hue (`--hue-*`), never `--status-*`.
 *
 * A chip is tinted, not filled: the hue mixed into `--surface-base` (the
 * codebase's opaque tint recipe, `.agenthub-status` in app.css) with the hue
 * itself as the text. How much hue is `--method-tint`, set per mode in
 * index.css: the most that keeps every palette's hue at 4.5:1 on its own tint.
 * `themeContrast.test.ts` reads these classes and that token back and checks
 * it, so a change here or there is checked there.
 *
 * Every class is written out whole: Tailwind finds classes by reading the
 * source, so one assembled from a variable would never be generated.
 */

export type MethodHue = "--hue-green" | "--hue-yellow" | "--hue-blue" | "--hue-red";

/** The hue a method is drawn in, or null for a neutral one. */
export function methodHue(method: string): MethodHue | null {
  switch (method.trim().toUpperCase()) {
    case "GET":
      return "--hue-green";
    case "POST":
      return "--hue-yellow";
    case "PUT":
    case "PATCH":
      return "--hue-blue";
    case "DELETE":
      return "--hue-red";
    default:
      return null;
  }
}

const CHIP: Record<MethodHue, string> = {
  "--hue-green": "bg-[color-mix(in_srgb,var(--hue-green)_var(--method-tint),var(--surface-base))] text-(--hue-green)",
  "--hue-yellow": "bg-[color-mix(in_srgb,var(--hue-yellow)_var(--method-tint),var(--surface-base))] text-(--hue-yellow)",
  "--hue-blue": "bg-[color-mix(in_srgb,var(--hue-blue)_var(--method-tint),var(--surface-base))] text-(--hue-blue)",
  "--hue-red": "bg-[color-mix(in_srgb,var(--hue-red)_var(--method-tint),var(--surface-base))] text-(--hue-red)",
};

/** The neutral chip: the raised surface every other chip in the app uses. */
export const NEUTRAL_METHOD_CHIP = "bg-surface-raised text-text-primary";

const TEXT: Record<MethodHue, string> = {
  "--hue-green": "text-(--hue-green)",
  "--hue-yellow": "text-(--hue-yellow)",
  "--hue-blue": "text-(--hue-blue)",
  "--hue-red": "text-(--hue-red)",
};

/** Background and text of a method chip. Shape and type are the caller's. */
export function methodChipClass(method: string): string {
  const hue = methodHue(method);
  return hue ? CHIP[hue] : NEUTRAL_METHOD_CHIP;
}

/** Text colour only, for a method written without a chip. */
export function methodTextClass(method: string): string {
  const hue = methodHue(method);
  return hue ? TEXT[hue] : "text-text-secondary";
}

/** Every hued chip class, for the contrast test to read back. */
export const METHOD_CHIP_CLASSES: Readonly<Record<MethodHue, string>> = CHIP;
