import type { CSSProperties } from "react";
import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { ResolvedTheme } from "@/theme/ThemeProvider";
import { FAMILIES, resolvePalette } from "@/theme/families";
import { HUE_TOKENS } from "@/theme/tokens";

/**
 * A Color palette, shown as what it changes: its eight hues and a line of
 * code in its syntax colours, on the Mono ground of the mode you are in.
 *
 * The resolved palette goes on the swatch as inline custom properties —
 * literal values, because a `var()` declared on :root has already resolved
 * by the time it is inherited here.
 */
export function PaletteSwatch({ family, mode }: { family: ThemeFamilyPreference; mode: ResolvedTheme }) {
  return (
    <span
      style={resolvePalette(FAMILIES[family][mode]) as CSSProperties}
      className="flex flex-col gap-1.5 rounded-sm border border-border-subtle bg-surface-base p-1.5"
    >
      <span className="flex gap-0.5">
        {HUE_TOKENS.map((token) => (
          <span key={token} data-hue={token} className="h-2 flex-1 rounded-xs" style={{ backgroundColor: `var(${token})` }} />
        ))}
      </span>
      <span className="truncate font-mono text-code-11">
        <span className="text-(--syntax-keyword) font-(--syntax-keyword-weight)">const</span>{" "}
        <span className="text-(--syntax-definition)">id</span>{" "}
        <span className="text-text-secondary">=</span>{" "}
        <span className="text-(--syntax-string)">"x"</span>
      </span>
    </span>
  );
}
