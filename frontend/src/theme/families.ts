import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { MixToken, ThemeToken, TintToken } from "./tokens";

/**
 * One palette, in one mode: the tokens it overrides and nothing else.
 *
 * Partial on purpose. A family says only what differs from index.css, so
 * Ayu Light does not have to restate the nine note-folder colours to change a
 * background. Everything it omits falls through to the base theme, which is
 * the same cascade light mode has always relied on.
 */
export type ThemePalette = Partial<Record<TintToken | ThemeToken | MixToken, string>>;

/**
 * A family supplies BOTH modes.
 *
 * Not optional, and it is the one rule that keeps the rest of the system free
 * of branches: with both modes guaranteed, "family × mode" is a total function
 * and nothing has to answer "what happens when the system goes light and the
 * chosen family has no light". Dracula ships Alucard, its own light palette,
 * for exactly this reason.
 */
export type ThemeFamilyDefinition = {
  id: ThemeFamilyPreference;
  label: string;
  dark: ThemePalette;
  light: ThemePalette;
};

/**
 * The base palette is the absence of an override.
 *
 * index.css already declares every token for both modes, so "devkit" is two
 * empty maps rather than a copy of them — a copy would be a second place to
 * change a colour, and the second place is always the one that gets missed.
 */
export const DEVKIT_FAMILY: ThemeFamilyDefinition = {
  id: "devkit",
  label: "DevKit",
  dark: {},
  light: {},
};

/** Every family the app can paint with. Phase 2 fills in the other four. */
export const FAMILIES: Record<ThemeFamilyPreference, ThemeFamilyDefinition | null> = {
  devkit: DEVKIT_FAMILY,
  catppuccin: null,
  dracula: null,
  ayu: null,
  github: null,
};

/** The family to paint with, falling back to the base for one not yet built. */
export function familyOf(id: ThemeFamilyPreference): ThemeFamilyDefinition {
  return FAMILIES[id] ?? DEVKIT_FAMILY;
}
