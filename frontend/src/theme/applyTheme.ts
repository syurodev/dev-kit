import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { ThemePalette } from "./families";

/** The one element the whole palette lives in. */
const STYLE_ID = "devkit-theme";

/** Styled in index.css; kills transitions for the duration of the swap. */
const SWITCHING_CLASS = "theme-switching";

/**
 * The selector a family's tokens are written under.
 *
 * Two attributes, and the count is the point. index.css declares the base at
 * `:root` — specificity (0,1,0) — and light mode at `:root[data-theme="light"]`
 * — (0,2,0). A family written at plain `:root` would lose to light mode and
 * apply in dark only, silently. Pinning both attributes puts it at (0,3,0),
 * above everything in the stylesheet, without an `!important` anywhere.
 *
 * It also means the rule only matches while the document actually carries that
 * family and mode, so a stale block cannot paint the wrong theme for a frame.
 */
export function themeSelector(family: ThemeFamilyPreference, mode: "light" | "dark"): string {
  return `:root[data-theme-family="${family}"][data-theme="${mode}"]`;
}

/**
 * The stylesheet for one family, both modes.
 *
 * Both, not just the active one: the mode can change without the family
 * changing (the system flipping at sunset), and re-authoring CSS on a mode
 * flip would make a free attribute swap into a style recalc plus a parse.
 *
 * Pure, so the escaping and the empty-palette case are testable without a DOM.
 */
export function themeCss(
  family: ThemeFamilyPreference,
  palettes: { dark: ThemePalette; light: ThemePalette },
): string {
  const blocks: string[] = [];
  for (const mode of ["dark", "light"] as const) {
    const entries = Object.entries(palettes[mode]);
    if (entries.length === 0) {
      continue;
    }
    const body = entries.map(([token, value]) => `${token}:${value}`).join(";");
    blocks.push(`${themeSelector(family, mode)}{${body}}`);
  }
  return blocks.join("\n");
}

/**
 * Puts a family's palette on the document.
 *
 * One `textContent` write, not one `setProperty` per token. The tokens are a
 * single state — half of a palette is not a palette — and forty-odd separate
 * writes are forty chances for a style recalc to land between them. The
 * smoothness doc's rule for theming (§8.4) is that every colour is a token so
 * a switch is ONE frame; that only holds if the write is one thing too.
 *
 * The <style> is created once and reused. Replacing the element would detach
 * and re-attach a stylesheet, which invalidates more than editing its text.
 *
 * Transitions are suppressed across the swap. Every colour on every element
 * changes at once, and a great many of them carry `transition-colors`, so
 * without this the swap is a 150 ms colour animation on the whole tree —
 * measured, not assumed. Two frames of suppression, because one rAF only
 * guarantees the style has been recalculated, not that it has been painted.
 */
export function applyThemeFamily(
  family: ThemeFamilyPreference,
  palettes: { dark: ThemePalette; light: ThemePalette },
): void {
  if (typeof document === "undefined") {
    return;
  }
  let style = document.getElementById(STYLE_ID) as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement("style");
    style.id = STYLE_ID;
    document.head.append(style);
  }
  const css = themeCss(family, palettes);
  const root = document.documentElement;
  const changed = style.textContent !== css || root.dataset.themeFamily !== family;
  if (!changed) {
    return;
  }
  // Nothing to suppress on the first application: there is no previous colour
  // to animate away from, and the class would only cost a recalc.
  const suppress = root.dataset.themeFamily !== undefined;
  if (suppress) {
    root.classList.add(SWITCHING_CLASS);
  }
  style.textContent = css;
  root.dataset.themeFamily = family;
  if (suppress) {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => root.classList.remove(SWITCHING_CLASS));
    });
  }
}
