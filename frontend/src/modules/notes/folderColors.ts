/**
 * The preset colours: names the theme maps to `--note-folder-<name>`, so they
 * follow light and dark mode. They are presets, not the whole set — a folder
 * or note may also carry any "#rrggbb" (see ValidNoteFolderColor in
 * internal/vaultitem), which renders as that exact colour in both themes.
 */
export const FOLDER_COLORS = [
  "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray",
] as const;
export type FolderColor = (typeof FOLDER_COLORS)[number];

export function isPresetColor(color: string): color is FolderColor {
  return FOLDER_COLORS.includes(color as FolderColor);
}

/** The stored hex form, and only that: the value goes straight into a style. */
export function isHexColor(color: string): boolean {
  return /^#[0-9a-f]{6}$/.test(color);
}

/**
 * The CSS colour for a stored note/folder colour, or undefined when there is
 * none (or it is not a form this build knows — never passed through to CSS
 * unchecked).
 */
export function noteColorValue(color: string): string | undefined {
  if (isPresetColor(color)) return `var(--note-folder-${color})`;
  if (isHexColor(color)) return color;
  return undefined;
}

export function folderDotVar(color: string): string {
  return noteColorValue(color) ?? "var(--note-folder-gray)";
}
