import type { CSSProperties } from "react";
import { FOLDER_COLORS, type FolderColor } from "./folderColors";

export function noteAccentColor(color: string): string | undefined {
  if (!FOLDER_COLORS.includes(color as FolderColor)) {
    return undefined;
  }
  return `var(--note-folder-${color})`;
}

/**
 * A note card is tinted by its folder's colour: Figma `Note Card` (798:3178)
 * fills with `note-folder/<colour>-surface` and strokes with
 * `note-folder/<colour>-border`, which in the Note Palette are the base colour
 * at 26% and 45%. Derived here from the one base token per colour rather than
 * adding eighteen more tokens, the same way tagSoftBackground already does
 * for tags. A note with no folder colour takes gray — Figma's "untagged"
 * cards do the same.
 */
export function noteCardSurface(color: string): CSSProperties {
  const base = noteAccentColor(color) ?? "var(--note-folder-gray)";
  return {
    backgroundColor: `color-mix(in srgb, ${base} 26%, transparent)`,
    borderColor: `color-mix(in srgb, ${base} 45%, transparent)`,
  };
}
