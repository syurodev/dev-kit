/**
 * Deterministic hue (0-359) for a module id, used to give every tab in
 * that module's cluster a consistent background color without touching
 * every module's manifest (unlike `icon`, this isn't module-authored —
 * two authors picking clashing colors independently isn't a risk worth
 * taking on for a purely decorative grouping cue).
 */
export function moduleHue(moduleId: string): number {
  let hash = 0;
  for (let i = 0; i < moduleId.length; i++) {
    hash = (hash * 31 + moduleId.charCodeAt(i)) | 0;
  }
  return Math.abs(hash) % 360;
}
