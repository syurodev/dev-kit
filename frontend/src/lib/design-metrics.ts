/** Numeric adapters for positioning APIs that cannot accept CSS var().
 * CSS remains authoritative in the browser; defaults support SSR and tests.
 */
const spacingDefaults = { inline: 8, control: 12 } as const;

export function spacingPx(name: keyof typeof spacingDefaults): number {
  if (typeof document === "undefined") return spacingDefaults[name];
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--space-${name}`).trim();
  // These API-facing tokens are intentionally px, not rem/em.
  return /^\d+(\.\d+)?px$/.test(value)
    ? Number.parseFloat(value)
    : spacingDefaults[name];
}

/** Word needs literal inline point values, not browser CSS custom properties. */
export const DOCX_CODE_METRICS = {
  margin: "8pt 0",
  padding: "12pt",
  fontSize: "10pt",
} as const;
