# Mono Chrome + Color Palettes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the app chrome a single monochrome design (light + dark), and turn the theme family into a "Color palette" that supplies only hues, code syntax colours and terminal ANSI colours.

**Architecture:** `index.css` becomes the only source of chrome tokens (Mono). A family in `theme/families.ts` declares 8 `--hue-*` per mode and may override `--syntax-*`; `resolvePalette()` fills syntax and `--ansi-*` from the hues, and `applyThemeFamily` writes that complete resolved set (never a chrome token) under `:root[data-theme-family][data-theme]`. The Go preference (`theme_family`) is unchanged.

**Tech Stack:** React 19 + TypeScript, Tailwind v4 (`text-(--token)` syntax), CSS custom properties, bun test, CodeMirror/lezer highlight, xterm.

**Spec:** `docs/superpowers/specs/2026-10-13-mono-chrome-color-palettes-design.md`

**Conventions for every task:**
- Working directory for commands is `frontend/` unless stated.
- The theme test suite (`bun test src/theme`) is expected to be red between Task 3 and Task 6 — the stylesheet, the families and the tests that tie them together change in three steps. Each of those tasks runs only the tests it names. From Task 6 on, `bun test src/theme` must be green.
- Commit message trailer on every commit:
  ```
  Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
  ```
- Never use `--no-verify`.

---

## File map

| File | Responsibility | Change |
|---|---|---|
| `frontend/src/theme/tokens.ts` | Token lists (the contract) | Add `HUE_TOKENS`, `TERMINAL_TOKENS`, `SYNTAX_STYLE_TOKENS`, `CHROME_TOKENS`, `PALETTE_TOKENS`; drop `--note-folder-*` from `THEME_TOKENS` |
| `frontend/src/theme/cssTokens.ts` | **New.** Parse `index.css` blocks and resolve `var()` chains — shared by the two contract tests | Create |
| `frontend/src/theme/families.ts` | Palette data + `resolvePalette` / `resolvedPalettes` | Rewrite |
| `frontend/src/theme/families.test.ts` | **New.** `resolvePalette` rules | Create |
| `frontend/src/index.css` | Mono chrome + base (devkit) palette | Edit `:root`, `:root[data-theme="light"]`, shell backdrop |
| `frontend/src/theme/tokenContract.test.ts` | Stylesheet ↔ contract ↔ families | Rewrite parts |
| `frontend/src/theme/themeContrast.test.ts` | Contrast floors | Rewrite |
| `frontend/src/theme/applyTheme.ts`, `ThemeProvider.tsx`, `applyTheme.test.ts` | Write resolved palettes | Edit |
| `frontend/src/dev/TreeNextGallery.tsx` | Dev gallery contrast table | Edit call site |
| `frontend/src/theme/terminalTheme.ts` | xterm palette | Read `--ansi-*` |
| `frontend/src/components/code-editor/devkitSyntax.ts`, `components/ui/code-block-node.tsx`, `components/ui/code-block-node-static.tsx` | Code colouring | Keyword weight token; links in `--hue-blue` |
| `frontend/src/modules/settings/SettingsView.tsx` | Picker | "Color palette", new swatch |
| Decorative uses (Task 11 list) | Categorical colour | `--status-*` → `--hue-*` |
| `internal/preferences/preferences.go`, `internal/bridge/preferences.go`, `docs/design/devkit-ui-ux-rules.md`, `AGENTS.md` | Docs/comments | Edit text |

---

### Task 1: Token lists

**Files:**
- Modify: `frontend/src/theme/tokens.ts`

- [ ] **Step 1: Replace the note-folder entries and add the new lists**

In `THEME_TOKENS`, delete the `// Note/folder palette` comment and the nine `"--note-folder-…"` lines (they become derived from hues in Task 3).

Replace everything from the `SYNTAX_TOKENS` doc comment to the end of the file with:

```ts
/**
 * Code colours. A family may set them; when it does not, `resolvePalette`
 * (families.ts) derives them from its hues. The base (devkit) palette sets
 * them to the text ramp — Mono code — and index.css carries those values.
 */
export const SYNTAX_TOKENS = [
  "--syntax-keyword",
  "--syntax-string",
  "--syntax-number",
  "--syntax-type",
  "--syntax-function",
  "--syntax-definition",
  "--syntax-comment",
  "--syntax-meta",
] as const;

/**
 * Not a colour, but part of how a palette draws code: Mono code has one hue,
 * so its keywords are told apart by weight. Every other palette resolves it
 * to 400.
 */
export const SYNTAX_STYLE_TOKENS = ["--syntax-keyword-weight"] as const;

/**
 * The palette proper: eight hues per mode, the one thing every family must
 * declare. Decorative and categorical colour reads these (note folders,
 * category chips, CodeGraph layers, engine marks); status never does.
 */
export const HUE_TOKENS = [
  "--hue-red",
  "--hue-orange",
  "--hue-yellow",
  "--hue-green",
  "--hue-teal",
  "--hue-blue",
  "--hue-violet",
  "--hue-pink",
] as const;

/**
 * The terminal's six chromatic ANSI slots. Black, white, foreground, cursor
 * and selection come from the chrome (terminalTheme.ts); bright variants are
 * derived there.
 */
export const TERMINAL_TOKENS = [
  "--ansi-red",
  "--ansi-green",
  "--ansi-yellow",
  "--ansi-blue",
  "--ansi-magenta",
  "--ansi-cyan",
] as const;

export type TintToken = (typeof TINT_TOKENS)[number];
export type ThemeToken = (typeof THEME_TOKENS)[number];
export type MixToken = (typeof MIX_TOKENS)[number];
export type SyntaxToken = (typeof SYNTAX_TOKENS)[number];
export type SyntaxStyleToken = (typeof SYNTAX_STYLE_TOKENS)[number];
export type HueToken = (typeof HUE_TOKENS)[number];
export type TerminalToken = (typeof TERMINAL_TOKENS)[number];

/** Owned by index.css alone. A family may not set any of these. */
export const CHROME_TOKENS = [...TINT_TOKENS, ...THEME_TOKENS, ...MIX_TOKENS] as const;

/** Everything a family's resolved palette carries, and nothing else. */
export const PALETTE_TOKENS = [
  ...HUE_TOKENS,
  ...SYNTAX_TOKENS,
  ...SYNTAX_STYLE_TOKENS,
  ...TERMINAL_TOKENS,
] as const;
export type PaletteToken = (typeof PALETTE_TOKENS)[number];

/** Every custom property index.css must declare at :root. */
export const CONTRACT = [...CHROME_TOKENS, ...PALETTE_TOKENS] as const;
```

Also update the file's header comment: replace the paragraph that begins "Three tiers, and only the third is a theme's job:" through "…index.css declares in total." with:

```ts
 * Two owners:
 *
 * - CHROME (`TINT_TOKENS`, `THEME_TOKENS`, `MIX_TOKENS`) — the Mono shell.
 *   index.css is its only author; light mode is `:root[data-theme="light"]`.
 * - PALETTE (`HUE_TOKENS`, `SYNTAX_TOKENS`, `SYNTAX_STYLE_TOKENS`,
 *   `TERMINAL_TOKENS`) — what the person picks as "Color palette". A family
 *   declares hues (and optionally syntax); families.ts resolves the rest.
 *
 * Derived tokens (`--card: var(--surface-base)`, `--note-folder-red:
 * var(--hue-red)`) follow their parent and appear in neither list.
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p . 2>&1 | head -30`
Expected: errors only in `theme/families.ts` / `theme/tokenContract.test.ts` (they still import the old shapes). No error inside `tokens.ts` itself.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/theme/tokens.ts
git commit -m "theme: split the token contract into chrome and palette

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Shared stylesheet reader for tests

**Files:**
- Create: `frontend/src/theme/cssTokens.ts`
- Create: `frontend/src/theme/cssTokens.test.ts`

- [ ] **Step 1: Write the failing test**

`frontend/src/theme/cssTokens.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { parseBlock, resolveVar, stripComments } from "./cssTokens";

const css = stripComments(`
:root {
    --a: #111111; /* trailing note --fake: #000000 */
    --b: var(--a);
    --c: var(--b);
}

:root[data-theme="light"] {
    --a: #eeeeee;
}
`);

describe("cssTokens", () => {
  test("parses one block and ignores comments", () => {
    const dark = parseBlock(css, /\n:root/);
    expect(dark.get("--a")).toBe("#111111");
    expect(dark.has("--fake")).toBe(false);
  });

  test("resolves a var chain, light falling back to dark", () => {
    const dark = parseBlock(css, /\n:root/);
    const light = parseBlock(css, /:root\[data-theme="light"\]/);
    expect(resolveVar("--c", dark, dark)).toBe("#111111");
    expect(resolveVar("--c", light, dark)).toBe("#eeeeee");
    expect(resolveVar("var(--b)", light, dark)).toBe("#eeeeee");
    expect(resolveVar("#123456", light, dark)).toBe("#123456");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/theme/cssTokens.test.ts`
Expected: FAIL — `Cannot find module './cssTokens'`.

- [ ] **Step 3: Implement**

`frontend/src/theme/cssTokens.ts`:

```ts
/**
 * Reading index.css's token blocks — for tests only.
 *
 * tokenContract.test.ts and themeContrast.test.ts both need the stylesheet's
 * declarations as data; this is the one parser they share, so a fix to it
 * (comments, var chains) reaches both.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** Comments go first: index.css documents tokens in prose that starts with
 *  the token's own name, and a parser that reads those invents tokens. */
export function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

export function readIndexCss(): string {
  return stripComments(readFileSync(join(import.meta.dir, "../index.css"), "utf8"));
}

/** The declarations inside the first `{ … }` block after `selector`. */
export function parseBlock(css: string, selector: RegExp): Map<string, string> {
  const match = css.match(new RegExp(selector.source + String.raw`\s*\{([\s\S]*?)\n\}`));
  if (!match) throw new Error(`no block for ${selector}`);
  const out = new Map<string, string>();
  for (const line of match[1].split("\n")) {
    const declaration = line.trim();
    if (!declaration.startsWith("--")) continue;
    const colon = declaration.indexOf(":");
    if (colon === -1) continue;
    out.set(declaration.slice(0, colon).trim(), declaration.slice(colon + 1).split(";")[0].trim());
  }
  return out;
}

/**
 * A token name or a value, followed through `var(--x)` until it is not one.
 * `mode` is the block for the mode being read; `base` is :root, which every
 * mode inherits from.
 */
export function resolveVar(
  tokenOrValue: string,
  mode: Map<string, string>,
  base: Map<string, string>,
): string | undefined {
  let value: string | undefined = tokenOrValue.startsWith("--")
    ? (mode.get(tokenOrValue) ?? base.get(tokenOrValue))
    : tokenOrValue;
  for (let hop = 0; hop < 8 && value?.startsWith("var("); hop += 1) {
    const name = value.slice(4, value.indexOf(")"));
    value = mode.get(name) ?? base.get(name);
  }
  return value;
}

/** index.css's dark (:root) and light blocks. */
export function themeBlocks() {
  const css = readIndexCss();
  return {
    dark: parseBlock(css, /\n:root/),
    light: parseBlock(css, /:root\[data-theme="light"\]/),
  };
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && bun test src/theme/cssTokens.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/theme/cssTokens.ts frontend/src/theme/cssTokens.test.ts
git commit -m "theme: share the index.css token reader between tests

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Mono chrome and base palette in index.css

**Files:**
- Modify: `frontend/src/index.css` (`:root` block starting line 298, `:root[data-theme="light"]` starting line 548, shell backdrop at lines 807–822)

- [ ] **Step 1: Remove the brand palette anchors**

Delete lines 299–311 (`/* 01 Foundations: Palette Anchors … */` and the twelve `--palette-*` declarations), leaving a blank line after `:root {`.

- [ ] **Step 2: Tint sources**

Replace `--tint-border: var(--palette-candy-blue);` with `--tint-border: #ffffff;`.

- [ ] **Step 3: Dark semantic tokens**

Replace the block from `/* Semantic Tokens (dark-first; …) */` through `--status-danger: #d87886;` with:

```css
    /* Semantic Tokens — Mono chrome (dark-first; light overrides under
       [data-theme="light"]). Grey steps separate surfaces; nothing in the
       chrome carries a hue except status. The person's Color palette
       (theme/families.ts) never reaches these. */
    --background-app: #000000;
    --background-canvas: #0a0a0a;
    --surface-base: #141414;
    --surface-raised: #1c1c1c;
    --surface-glass-floating: color-mix(in srgb, var(--surface-base) 72%, transparent);
    --surface-glass-modal: color-mix(in srgb, var(--surface-base) 85%, transparent);
    --surface-tinted: #262626;

    --text-primary: #fafafa;
    --text-secondary: #a3a3a3;
    --text-tertiary: #8f8f8f;
    --icon-secondary: var(--text-secondary);

    /* The one solid action: white on dark, black on light. Hover and press
       move toward the page so the fill stays the strongest thing on it. */
    --action-primary: #fafafa;
    --action-primary-foreground: #0a0a0a;
    --action-primary-hover: color-mix(in srgb, var(--action-primary) 88%, #000000);
    --action-primary-pressed: color-mix(in srgb, var(--action-primary) 78%, #000000);

    /* Status keeps its hue, desaturated, and is NOT part of a palette: an
       error looks like an error whichever palette is chosen. Info is not a
       danger level, so it is grey. */
    --status-info: var(--text-secondary);
    --status-success: #7cc59a;
    --status-warning: #d9b25c;
    --status-recovery: #c39bd0;
    --status-danger: #e07b7f;
```

Then replace:
- `--brand-text: var(--action-primary);` → `--brand-text: var(--text-primary);`
- the comment above it (`/* Agent/session states + brand, … (lime text on white is illegible). */`) → `/* Agent/session states + brand, consumed by app.css. --brand-text is the accent for text/icons; in Mono it is the primary text. */`
- `--border-focus: var(--palette-candy-blue);` → `--border-focus: var(--text-primary);`

- [ ] **Step 4: Base palette (devkit) in :root**

Replace the whole syntax block (the comment `/* Syntax colours (components/code-editor/devkitSyntax.ts), …` and the eight `--syntax-*` lines) with:

```css
    /* The base Color palette (devkit = Mono), for the first paint and as the
       values theme/families.ts mirrors. applyThemeFamily writes every one of
       these again, resolved, for whichever palette is chosen — so nothing
       below may be read as "the" colour of anything but devkit. */
    --hue-red: #e07b7f;
    --hue-orange: #e0945e;
    --hue-yellow: #d9b25c;
    --hue-green: #7cc59a;
    --hue-teal: #6cc1bb;
    --hue-blue: #8aaee6;
    --hue-violet: #b29ce6;
    --hue-pink: #de8fb8;

    /* Mono code: structure by weight and the text ramp, not by hue. */
    --syntax-keyword: var(--text-primary);
    --syntax-keyword-weight: 600;
    --syntax-string: var(--text-secondary);
    --syntax-number: var(--text-secondary);
    --syntax-type: var(--text-primary);
    --syntax-function: var(--text-primary);
    --syntax-definition: var(--text-primary);
    --syntax-comment: var(--text-tertiary);
    --syntax-meta: var(--text-tertiary);

    /* Terminal colours keep their hues even in Mono: a red line in git diff
       is information. */
    --ansi-red: var(--hue-red);
    --ansi-green: var(--hue-green);
    --ansi-yellow: var(--hue-yellow);
    --ansi-blue: var(--hue-blue);
    --ansi-magenta: var(--hue-violet);
    --ansi-cyan: var(--hue-teal);
```

- [ ] **Step 5: Note-folder colours follow the palette**

Replace the note-folder block in `:root` (comment `/* Note/folder 9-color palette …` and the nine lines) with:

```css
    /* Note/folder colours (modules/notes) are the palette's hues: a saved
       note keeps its colour NAME, the value follows the chosen palette. */
    --note-folder-red: var(--hue-red);
    --note-folder-orange: var(--hue-orange);
    --note-folder-yellow: var(--hue-yellow);
    --note-folder-green: var(--hue-green);
    --note-folder-teal: var(--hue-teal);
    --note-folder-blue: var(--hue-blue);
    --note-folder-violet: var(--hue-violet);
    --note-folder-pink: var(--hue-pink);
    --note-folder-gray: var(--text-tertiary);
```

- [ ] **Step 6: Light block**

In `:root[data-theme="light"]`, make these exact changes:

Replace the surface/text lines (`--background-app: #e9eaee;` … `--icon-secondary: #55565b;`, keeping the `--surface-glass-modal` comment and declaration where they are) so the values read:

```css
    --background-app: #f5f5f5;
    --background-canvas: #fafafa;
    --surface-base: #ffffff;
    --surface-raised: #f5f5f5;
```
```css
    --surface-tinted: #ebebeb;

    --text-primary: #0a0a0a;
    --text-secondary: #525252;
    --text-tertiary: #666666;
```
(`--icon-secondary` is deleted from light: it is `var(--text-secondary)` in dark and follows.)

Replace the lime comment and `--brand-text: #567300;` with:

```css
    --action-primary: #0a0a0a;
    --action-primary-foreground: #ffffff;
    --action-primary-hover: color-mix(in srgb, var(--action-primary) 85%, #ffffff);
    --action-primary-pressed: color-mix(in srgb, var(--action-primary) 75%, #ffffff);
```

Replace the five status lines with (info is deleted — it follows `--text-secondary`):

```css
    --status-success: #2b7350;
    --status-warning: #8a6100;
    --status-recovery: #86508a;
    --status-danger: #b3363f;
```

Replace `--tint-veil: #1d1d1f;` and `--tint-border: #1d1d1f;` with `#0a0a0a` each. Delete the line `--border-focus: #4b8cb8;`.

Replace the note-palette light pairs (comment `/* Note palette light pairs … */` and nine lines) with the light hues:

```css
    /* The base palette's light hues (theme/families.ts DEVKIT_FAMILY.light). */
    --hue-red: #b3363f;
    --hue-orange: #a14f1a;
    --hue-yellow: #8a6100;
    --hue-green: #2b7350;
    --hue-teal: #1f7470;
    --hue-blue: #3a62a8;
    --hue-violet: #6e55a8;
    --hue-pink: #a4467a;
```

- [ ] **Step 7: Neutral shell backdrop**

In the `:root` block at line ~807 replace `--shell-backdrop: #101114;` → `#050505` and `--shell-backdrop-top: #1b1d21;` → `#0f0f0f`. In the light block right after it replace `#eef0f3` → `#eeeeee`, `#f7f8fa` → `#f7f7f7`, and `--shell-shadow: 0 2px 6px rgb(24 32 48 / 0.06);` → `--shell-shadow: 0 2px 6px rgb(0 0 0 / 0.06);`.

- [ ] **Step 8: Nothing still names a removed token**

Run: `cd frontend && grep -rn "\-\-palette-" src | grep -v "\.test\."`
Expected: no output.

- [ ] **Step 9: Build**

Run: `cd frontend && bunx vite build --mode production 2>&1 | tail -5`
Expected: build succeeds (`✓ built in …`).

- [ ] **Step 10: Commit**

```bash
git add frontend/src/index.css
git commit -m "theme: Mono chrome and the base color palette in index.css

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Families become color palettes

**Files:**
- Modify: `frontend/src/theme/families.ts` (full rewrite)
- Create: `frontend/src/theme/families.test.ts`

- [ ] **Step 1: Write the failing test**

`frontend/src/theme/families.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { DEVKIT_FAMILY, FAMILIES, resolvePalette, resolvedPalettes } from "./families";
import { CHROME_TOKENS, HUE_TOKENS, PALETTE_TOKENS, SYNTAX_TOKENS, TERMINAL_TOKENS } from "./tokens";

describe("resolvePalette", () => {
  const dracula = FAMILIES.dracula.dark;

  test("fills every palette token and nothing else", () => {
    const resolved = resolvePalette(dracula);
    expect(Object.keys(resolved).sort()).toEqual([...PALETTE_TOKENS].sort());
  });

  test("derives syntax from hues when the family names none", () => {
    const r = resolvePalette(dracula);
    expect(r["--syntax-keyword"]).toBe(dracula["--hue-violet"]);
    expect(r["--syntax-string"]).toBe(dracula["--hue-green"]);
    expect(r["--syntax-number"]).toBe(dracula["--hue-orange"]);
    expect(r["--syntax-type"]).toBe(dracula["--hue-yellow"]);
    expect(r["--syntax-function"]).toBe(dracula["--hue-blue"]);
    expect(r["--syntax-definition"]).toBe("var(--text-primary)");
    expect(r["--syntax-comment"]).toBe("var(--text-tertiary)");
    expect(r["--syntax-meta"]).toBe("var(--text-tertiary)");
    expect(r["--syntax-keyword-weight"]).toBe("400");
  });

  test("a family's own syntax wins over the derivation", () => {
    const r = resolvePalette(FAMILIES.xcode.dark);
    expect(r["--syntax-keyword"]).toBe("#fc5fa3");
  });

  test("ANSI follows hues", () => {
    const r = resolvePalette(dracula);
    expect(r["--ansi-red"]).toBe(dracula["--hue-red"]);
    expect(r["--ansi-magenta"]).toBe(dracula["--hue-violet"]);
    expect(r["--ansi-cyan"]).toBe(dracula["--hue-teal"]);
  });

  test("devkit is Mono code with bold keywords", () => {
    const r = resolvePalette(DEVKIT_FAMILY.dark);
    expect(r["--syntax-keyword"]).toBe("var(--text-primary)");
    expect(r["--syntax-keyword-weight"]).toBe("600");
  });

  test("resolvedPalettes gives both modes", () => {
    const both = resolvedPalettes("nord");
    expect(both.dark["--hue-blue"]).toBe(FAMILIES.nord.dark["--hue-blue"]);
    expect(both.light["--hue-blue"]).toBe(FAMILIES.nord.light["--hue-blue"]);
  });
});

describe("every family is a palette, not a theme", () => {
  for (const family of Object.values(FAMILIES)) {
    for (const mode of ["dark", "light"] as const) {
      test(`${family.id}/${mode} declares all eight hues`, () => {
        const missing = HUE_TOKENS.filter((t) => !(t in family[mode]));
        expect(missing).toEqual([]);
      });

      test(`${family.id}/${mode} declares no chrome token`, () => {
        const chrome = new Set<string>(CHROME_TOKENS);
        expect(Object.keys(family[mode]).filter((t) => chrome.has(t))).toEqual([]);
      });

      test(`${family.id}/${mode} sets all syntax tokens or none`, () => {
        const set = SYNTAX_TOKENS.filter((t) => t in family[mode]);
        expect([0, SYNTAX_TOKENS.length]).toContain(set.length);
      });

      test(`${family.id}/${mode} leaves ANSI to the hues`, () => {
        expect(TERMINAL_TOKENS.filter((t) => t in family[mode])).toEqual([]);
      });
    }
  }
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/theme/families.test.ts`
Expected: FAIL — `resolvePalette` is not exported.

- [ ] **Step 3: Rewrite families.ts**

Replace the whole file with:

```ts
import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { HueToken, PaletteToken, SyntaxStyleToken, SyntaxToken } from "./tokens";

/**
 * One Color palette, in one mode.
 *
 * Eight hues are required; syntax colours are optional (a family with an
 * editor palette of its own — Xcode — names them; the rest get them from
 * their hues). A palette never names a chrome token: the chrome is Mono and
 * index.css is its only author.
 *
 * Every hue is the family's own colour moved in LIGHTNESS only until it
 * clears 4.5:1 on every Mono ground (themeContrast.test.ts). Where a family
 * publishes fewer than eight accents, its comment says what stands in.
 */
export type ThemePalette = Record<HueToken, string> &
  Partial<Record<SyntaxToken | SyntaxStyleToken, string>>;

/** Every palette token, resolved: what applyThemeFamily writes. */
export type ResolvedPalette = Record<PaletteToken, string>;

/** A family supplies BOTH modes, so "family × mode" is a total function. */
export type ThemeFamilyDefinition = {
  id: ThemeFamilyPreference;
  label: string;
  dark: ThemePalette;
  light: ThemePalette;
};

/** Mono code: structure by weight and the text ramp. Mirrors index.css. */
const MONO_SYNTAX = {
  "--syntax-keyword": "var(--text-primary)",
  "--syntax-keyword-weight": "600",
  "--syntax-string": "var(--text-secondary)",
  "--syntax-number": "var(--text-secondary)",
  "--syntax-type": "var(--text-primary)",
  "--syntax-function": "var(--text-primary)",
  "--syntax-definition": "var(--text-primary)",
  "--syntax-comment": "var(--text-tertiary)",
  "--syntax-meta": "var(--text-tertiary)",
} as const;

/** Which hue a syntax role takes when the family names no syntax colours. */
const SYNTAX_FROM_HUE: Record<SyntaxToken, HueToken | string> = {
  "--syntax-keyword": "--hue-violet",
  "--syntax-string": "--hue-green",
  "--syntax-number": "--hue-orange",
  "--syntax-type": "--hue-yellow",
  "--syntax-function": "--hue-blue",
  "--syntax-definition": "var(--text-primary)",
  "--syntax-comment": "var(--text-tertiary)",
  "--syntax-meta": "var(--text-tertiary)",
};

/**
 * Hues to the full palette.
 *
 * Written out rather than left to `var(--hue-x)` in the stylesheet: the
 * Settings swatch sets a palette inline on one element, and a `var()`
 * declared on :root has already resolved by the time it is inherited there.
 * Literal values work on any element.
 */
export function resolvePalette(palette: ThemePalette): ResolvedPalette {
  const hue = (token: string) =>
    token.startsWith("--hue-") ? palette[token as HueToken] : token;
  const syntax = Object.fromEntries(
    Object.entries(SYNTAX_FROM_HUE).map(([token, source]) => [
      token,
      palette[token as SyntaxToken] ?? hue(source),
    ]),
  ) as Record<SyntaxToken, string>;
  return {
    "--hue-red": palette["--hue-red"],
    "--hue-orange": palette["--hue-orange"],
    "--hue-yellow": palette["--hue-yellow"],
    "--hue-green": palette["--hue-green"],
    "--hue-teal": palette["--hue-teal"],
    "--hue-blue": palette["--hue-blue"],
    "--hue-violet": palette["--hue-violet"],
    "--hue-pink": palette["--hue-pink"],
    ...syntax,
    "--syntax-keyword-weight": palette["--syntax-keyword-weight"] ?? "400",
    "--ansi-red": palette["--hue-red"],
    "--ansi-green": palette["--hue-green"],
    "--ansi-yellow": palette["--hue-yellow"],
    "--ansi-blue": palette["--hue-blue"],
    "--ansi-magenta": palette["--hue-violet"],
    "--ansi-cyan": palette["--hue-teal"],
  };
}

/** Mono: the muted hues the chrome's status ramp is drawn in, and code in the text ramp. */
export const DEVKIT_FAMILY: ThemeFamilyDefinition = {
  id: "devkit",
  label: "DevKit",
  dark: {
    "--hue-red": "#e07b7f",
    "--hue-orange": "#e0945e",
    "--hue-yellow": "#d9b25c",
    "--hue-green": "#7cc59a",
    "--hue-teal": "#6cc1bb",
    "--hue-blue": "#8aaee6",
    "--hue-violet": "#b29ce6",
    "--hue-pink": "#de8fb8",
    ...MONO_SYNTAX,
  },
  light: {
    "--hue-red": "#b3363f",
    "--hue-orange": "#a14f1a",
    "--hue-yellow": "#8a6100",
    "--hue-green": "#2b7350",
    "--hue-teal": "#1f7470",
    "--hue-blue": "#3a62a8",
    "--hue-violet": "#6e55a8",
    "--hue-pink": "#a4467a",
    ...MONO_SYNTAX,
  },
};

/** Mocha and Latte accents (peach is orange, mauve violet). */
const CATPPUCCIN: ThemeFamilyDefinition = {
  id: "catppuccin",
  label: "Catppuccin",
  dark: {
    "--hue-red": "#f38ba8",
    "--hue-orange": "#fab387",
    "--hue-yellow": "#f9e2af",
    "--hue-green": "#a6e3a1",
    "--hue-teal": "#94e2d5",
    "--hue-blue": "#89b4fa",
    "--hue-violet": "#cba6f7",
    "--hue-pink": "#f5c2e7",
  },
  light: {
    "--hue-red": "#d20f39",
    "--hue-orange": "#b94401",
    "--hue-yellow": "#955f13",
    "--hue-green": "#307820",
    "--hue-teal": "#12747a",
    "--hue-blue": "#125ef4",
    "--hue-violet": "#8839ef",
    "--hue-pink": "#c01d95",
  },
};

/** Dracula and Alucard. Dracula has no blue; its comment colour, lifted, stands in. */
const DRACULA: ThemeFamilyDefinition = {
  id: "dracula",
  label: "Dracula",
  dark: {
    "--hue-red": "#ff5555",
    "--hue-orange": "#ffb86c",
    "--hue-yellow": "#f1fa8c",
    "--hue-green": "#50fa7b",
    "--hue-teal": "#8be9fd",
    "--hue-blue": "#7c89b3",
    "--hue-violet": "#bd93f9",
    "--hue-pink": "#ff79c6",
  },
  light: {
    "--hue-red": "#c33828",
    "--hue-orange": "#a34d14",
    "--hue-yellow": "#7d6914",
    "--hue-green": "#14710a",
    "--hue-teal": "#036a96",
    "--hue-blue": "#3a4a8a",
    "--hue-violet": "#644ac9",
    "--hue-pink": "#a3144d",
  },
};

/** Ayu Dark and Ayu Light syntax colours. */
const AYU: ThemeFamilyDefinition = {
  id: "ayu",
  label: "Ayu",
  dark: {
    "--hue-red": "#f07178",
    "--hue-orange": "#ff8f40",
    "--hue-yellow": "#e6b450",
    "--hue-green": "#aad94c",
    "--hue-teal": "#95e6cb",
    "--hue-blue": "#59c2ff",
    "--hue-violet": "#d2a6ff",
    "--hue-pink": "#f29668",
  },
  light: {
    "--hue-red": "#d31616",
    "--hue-orange": "#b14d05",
    "--hue-yellow": "#955d0b",
    "--hue-green": "#567300",
    "--hue-teal": "#2a765d",
    "--hue-blue": "#166ead",
    "--hue-violet": "#8751bc",
    "--hue-pink": "#b44a15",
  },
};

/** GitHub's Primer prettylights, dark and light. */
const GITHUB: ThemeFamilyDefinition = {
  id: "github",
  label: "GitHub",
  dark: {
    "--hue-red": "#ff7b72",
    "--hue-orange": "#ffa657",
    "--hue-yellow": "#d29922",
    "--hue-green": "#7ee787",
    "--hue-teal": "#39c5cf",
    "--hue-blue": "#79c0ff",
    "--hue-violet": "#d2a8ff",
    "--hue-pink": "#f778ba",
  },
  light: {
    "--hue-red": "#cf222e",
    "--hue-orange": "#953800",
    "--hue-yellow": "#906000",
    "--hue-green": "#116329",
    "--hue-teal": "#1a767d",
    "--hue-blue": "#0550ae",
    "--hue-violet": "#7d4ade",
    "--hue-pink": "#b73783",
  },
};

/** Main and Dawn. Rosé Pine has six accents; green borrows foam and blue sits between pine and iris. */
const ROSE_PINE: ThemeFamilyDefinition = {
  id: "rosepine",
  label: "Rosé Pine",
  dark: {
    "--hue-red": "#eb6f92",
    "--hue-orange": "#ebbcba",
    "--hue-yellow": "#f6c177",
    "--hue-green": "#9ccfd8",
    "--hue-teal": "#4094b6",
    "--hue-blue": "#7aa2c8",
    "--hue-violet": "#c4a7e7",
    "--hue-pink": "#e29fc0",
  },
  light: {
    "--hue-red": "#a44f67",
    "--hue-orange": "#bc403a",
    "--hue-yellow": "#975d10",
    "--hue-green": "#42727b",
    "--hue-teal": "#286983",
    "--hue-blue": "#3d5f8f",
    "--hue-violet": "#796095",
    "--hue-pink": "#a54c7c",
  },
};

/** Kanagawa Wave (dark) and Lotus (light). */
const LOTUS: ThemeFamilyDefinition = {
  id: "lotus",
  label: "Lotus",
  dark: {
    "--hue-red": "#e46876",
    "--hue-orange": "#ffa066",
    "--hue-yellow": "#e6c384",
    "--hue-green": "#98bb6c",
    "--hue-teal": "#7aa89f",
    "--hue-blue": "#7e9cd8",
    "--hue-violet": "#9681b9",
    "--hue-pink": "#d27e99",
  },
  light: {
    "--hue-red": "#bf374a",
    "--hue-orange": "#a35700",
    "--hue-yellow": "#726c3c",
    "--hue-green": "#5b7140",
    "--hue-teal": "#52716b",
    "--hue-blue": "#4d699b",
    "--hue-violet": "#624c83",
    "--hue-pink": "#a74d6c",
  },
};

/** Aurora and Frost. Nord publishes one set for both modes; light takes it darker. */
const NORD: ThemeFamilyDefinition = {
  id: "nord",
  label: "Nord",
  dark: {
    "--hue-red": "#c6737b",
    "--hue-orange": "#d08770",
    "--hue-yellow": "#ebcb8b",
    "--hue-green": "#a3be8c",
    "--hue-teal": "#8fbcbb",
    "--hue-blue": "#81a1c1",
    "--hue-violet": "#b48ead",
    "--hue-pink": "#c7aac2",
  },
  light: {
    "--hue-red": "#b04751",
    "--hue-orange": "#a85237",
    "--hue-yellow": "#8a6418",
    "--hue-green": "#577140",
    "--hue-teal": "#447170",
    "--hue-blue": "#4c6c93",
    "--hue-violet": "#8a5c82",
    "--hue-pink": "#895b81",
  },
};

/** Solarized's eight accents, shared by both modes and moved in lightness only. */
const SOLARIZED: ThemeFamilyDefinition = {
  id: "solarized",
  label: "Solarized",
  dark: {
    "--hue-red": "#e45f5d",
    "--hue-orange": "#e86027",
    "--hue-yellow": "#b58900",
    "--hue-green": "#859900",
    "--hue-teal": "#2aa198",
    "--hue-blue": "#2a90d8",
    "--hue-violet": "#8084cc",
    "--hue-pink": "#dc5e9b",
  },
  light: {
    "--hue-red": "#cd2522",
    "--hue-orange": "#b94414",
    "--hue-yellow": "#876600",
    "--hue-green": "#627000",
    "--hue-teal": "#1f7770",
    "--hue-blue": "#1e6ea7",
    "--hue-violet": "#5c61bd",
    "--hue-pink": "#c22b74",
  },
};

/** Gruvbox dark and light, bright and faded accents. */
const GRUVBOX: ThemeFamilyDefinition = {
  id: "gruvbox",
  label: "Gruvbox",
  dark: {
    "--hue-red": "#fb4934",
    "--hue-orange": "#fe8019",
    "--hue-yellow": "#fabd2f",
    "--hue-green": "#b8bb26",
    "--hue-teal": "#8ec07c",
    "--hue-blue": "#83a598",
    "--hue-violet": "#d3869b",
    "--hue-pink": "#f28fad",
  },
  light: {
    "--hue-red": "#9d0006",
    "--hue-orange": "#af3a03",
    "--hue-yellow": "#936010",
    "--hue-green": "#726d0d",
    "--hue-teal": "#3f7654",
    "--hue-blue": "#076678",
    "--hue-violet": "#8f3f71",
    "--hue-pink": "#a04f74",
  },
};

/** Tokyo Night and Day. */
const TOKYO_NIGHT: ThemeFamilyDefinition = {
  id: "tokyonight",
  label: "Tokyo Night",
  dark: {
    "--hue-red": "#f7768e",
    "--hue-orange": "#ff9e64",
    "--hue-yellow": "#e0af68",
    "--hue-green": "#9ece6a",
    "--hue-teal": "#73daca",
    "--hue-blue": "#7aa2f7",
    "--hue-violet": "#bb9af7",
    "--hue-pink": "#ff3095",
  },
  light: {
    "--hue-red": "#8c4351",
    "--hue-orange": "#965027",
    "--hue-yellow": "#8f5e15",
    "--hue-green": "#485e30",
    "--hue-teal": "#33635c",
    "--hue-blue": "#34548a",
    "--hue-violet": "#5a4a78",
    "--hue-pink": "#9a3f6e",
  },
};

/** Everforest dark and light. */
const EVERFOREST: ThemeFamilyDefinition = {
  id: "everforest",
  label: "Everforest",
  dark: {
    "--hue-red": "#e67e80",
    "--hue-orange": "#e69875",
    "--hue-yellow": "#dbbc7f",
    "--hue-green": "#a7c080",
    "--hue-teal": "#83c092",
    "--hue-blue": "#7fbbb3",
    "--hue-violet": "#d699b6",
    "--hue-pink": "#e3a1c4",
  },
  light: {
    "--hue-red": "#d60c09",
    "--hue-orange": "#af4e08",
    "--hue-yellow": "#8b6400",
    "--hue-green": "#637101",
    "--hue-teal": "#267758",
    "--hue-blue": "#2c7096",
    "--hue-violet": "#be298f",
    "--hue-pink": "#ae408a",
  },
};

/** Xcode's default themes, with Xcode's own syntax mapping. */
const XCODE: ThemeFamilyDefinition = {
  id: "xcode",
  label: "Xcode",
  dark: {
    "--hue-red": "#ff6961",
    "--hue-orange": "#ff9f0a",
    "--hue-yellow": "#ffd60a",
    "--hue-green": "#30d158",
    "--hue-teal": "#40c8e0",
    "--hue-blue": "#409cff",
    "--hue-violet": "#da8fff",
    "--hue-pink": "#fc5fa3",
    "--syntax-keyword": "#fc5fa3",
    "--syntax-string": "#fc6a5d",
    "--syntax-number": "#d0bf69",
    "--syntax-type": "#d0a8ff",
    "--syntax-function": "#b282eb",
    "--syntax-definition": "#47a4c2",
    "--syntax-comment": "#909aa4",
    "--syntax-meta": "#c38d60",
  },
  light: {
    "--hue-red": "#d70015",
    "--hue-orange": "#c53300",
    "--hue-yellow": "#9d5900",
    "--hue-green": "#1f7935",
    "--hue-teal": "#0071a4",
    "--hue-blue": "#0067d7",
    "--hue-violet": "#8944ab",
    "--hue-pink": "#ab3ca2",
    "--syntax-keyword": "#9b2393",
    "--syntax-string": "#c41a16",
    "--syntax-number": "#1c00cf",
    "--syntax-type": "#3900a0",
    "--syntax-function": "#6c36a9",
    "--syntax-definition": "#0f68a0",
    "--syntax-comment": "#5d6c79",
    "--syntax-meta": "#815f03",
  },
};

/** Every palette the person can pick, in the order Settings shows them. */
export const FAMILIES: Record<ThemeFamilyPreference, ThemeFamilyDefinition> = {
  devkit: DEVKIT_FAMILY,
  catppuccin: CATPPUCCIN,
  dracula: DRACULA,
  ayu: AYU,
  github: GITHUB,
  rosepine: ROSE_PINE,
  lotus: LOTUS,
  nord: NORD,
  solarized: SOLARIZED,
  gruvbox: GRUVBOX,
  tokyonight: TOKYO_NIGHT,
  everforest: EVERFOREST,
  xcode: XCODE,
};

/** The family to paint with, falling back to the base for an unknown id. */
export function familyOf(id: ThemeFamilyPreference): ThemeFamilyDefinition {
  return FAMILIES[id] ?? DEVKIT_FAMILY;
}

/** Both modes of a palette, resolved — what applyThemeFamily takes. */
export function resolvedPalettes(id: ThemeFamilyPreference): {
  dark: ResolvedPalette;
  light: ResolvedPalette;
} {
  const family = familyOf(id);
  return { dark: resolvePalette(family.dark), light: resolvePalette(family.light) };
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && bun test src/theme/families.test.ts`
Expected: PASS (6 resolvePalette tests + 104 per-family tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/theme/families.ts frontend/src/theme/families.test.ts
git commit -m "theme: families are color palettes (hues, syntax, ANSI)

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Token contract test

**Files:**
- Modify: `frontend/src/theme/tokenContract.test.ts`

- [ ] **Step 1: Swap the local parser for the shared one**

Replace the imports and everything up to and including `const light = block(/:root\[data-theme="light"\]/);` with:

```ts
import { describe, expect, test } from "bun:test";
import { themeBlocks } from "./cssTokens";
import { DEVKIT_FAMILY } from "./families";
import {
  CONTRACT,
  HUE_TOKENS,
  MIX_TOKENS,
  PALETTE_TOKENS,
  SYNTAX_STYLE_TOKENS,
  SYNTAX_TOKENS,
  TERMINAL_TOKENS,
  THEME_TOKENS,
  TINT_TOKENS,
} from "./tokens";

const { dark, light } = themeBlocks();
```

- [ ] **Step 2: The orphan rule no longer exempts `--palette-*`**

In the test `"no colour token exists outside the contract without being derived"`, replace

```ts
    const shape = (name: string) =>
      /blur|saturate|radius|motion|ease|font|spring/.test(name) || name.startsWith("--palette-");
```
with
```ts
    const shape = (name: string) => /blur|saturate|radius|motion|ease|font|spring/.test(name);
```

- [ ] **Step 3: Light-mode tests**

In `"light is a working second implementation"` change the comment's "(the lime fill)" to "(a derived token such as --icon-secondary)". Keep the three tests.

- [ ] **Step 4: Tier overlap includes the new lists**

Replace the body of `test("the tiers do not overlap", …)` with:

```ts
    const lists = [TINT_TOKENS, THEME_TOKENS, MIX_TOKENS, HUE_TOKENS, SYNTAX_TOKENS, SYNTAX_STYLE_TOKENS, TERMINAL_TOKENS];
    const all = lists.flatMap((list) => [...list]);
    expect(new Set(all).size).toBe(all.length);
```

- [ ] **Step 5: Replace the two family describes**

Delete `describe("code colours follow the ramp unless a family says otherwise", …)` and `describe("the devkit family mirrors the stylesheet", …)` and add:

```ts
describe("the base palette in index.css is devkit", () => {
  // DEVKIT_FAMILY is a copy of index.css's palette values: the stylesheet
  // needs them for the first paint, families.ts for the Settings swatch and
  // for applyThemeFamily. A copy is a second place to change a colour, so
  // every value is pinned, both ways.
  for (const mode of ["dark", "light"] as const) {
    test(`${mode} hues and syntax match`, () => {
      const block = mode === "dark" ? dark : light;
      const drift: string[] = [];
      for (const [token, value] of Object.entries(DEVKIT_FAMILY[mode])) {
        const fromCss = block.get(token) ?? dark.get(token);
        if (fromCss !== value) drift.push(`${token}: family has ${value}, index.css has ${fromCss}`);
      }
      expect(drift, drift.join("\n")).toEqual([]);
    });
  }

  test("light restates only the hues", () => {
    const restated = [...PALETTE_TOKENS].filter((t) => light.has(t));
    expect(restated.sort()).toEqual([...HUE_TOKENS].sort());
  });

  test("ANSI derives from hues", () => {
    expect(dark.get("--ansi-red")).toBe("var(--hue-red)");
    expect(dark.get("--ansi-green")).toBe("var(--hue-green)");
    expect(dark.get("--ansi-yellow")).toBe("var(--hue-yellow)");
    expect(dark.get("--ansi-blue")).toBe("var(--hue-blue)");
    expect(dark.get("--ansi-magenta")).toBe("var(--hue-violet)");
    expect(dark.get("--ansi-cyan")).toBe("var(--hue-teal)");
  });

  test("note-folder colours are the hues", () => {
    for (const hue of ["red", "orange", "yellow", "green", "teal", "blue", "violet", "pink"]) {
      expect(dark.get(`--note-folder-${hue}`)).toBe(`var(--hue-${hue})`);
      expect(light.has(`--note-folder-${hue}`)).toBe(false);
    }
  });
});
```

Note `CONTRACT` remains imported and used by the first describe.

- [ ] **Step 6: Run**

Run: `cd frontend && bun test src/theme/tokenContract.test.ts`
Expected: PASS. If `"light overrides nothing it could have inherited"` fails, the failing name is a light declaration identical to dark's — delete it from the light block in `index.css` and re-run.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/theme/tokenContract.test.ts frontend/src/index.css
git commit -m "theme: contract test reads chrome and palette as separate owners

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Contrast test

**Files:**
- Modify: `frontend/src/theme/themeContrast.test.ts` (full rewrite below the colour helpers)

- [ ] **Step 1: Rewrite**

Keep the header comment, `lin`, `rgb`, `hex`, `luminance`, `contrast`, `over` exactly as they are. Replace the imports with:

```ts
import { describe, expect, it } from "bun:test";
import { readIndexCss, resolveVar, themeBlocks } from "./cssTokens";
import { FAMILIES, resolvePalette } from "./families";
import { PALETTE_TOKENS, SYNTAX_STYLE_TOKENS } from "./tokens";
```

Replace everything from `const FLOOR` to the end of the file with:

```ts
/**
 * Three tiers, not one floor: 4.5 everywhere would pass a ramp whose primary,
 * secondary and tertiary all land on 4.6 — a hierarchy with none left in it.
 */
const FLOOR: Record<string, number> = {
  "--text-primary": 7,
  "--text-secondary": 5.5,
  "--text-tertiary": 4.5,
  "--icon-secondary": 4.5,
  "--brand-text": 4.5,
  "--status-info": 4.5,
  "--status-success": 4.5,
  "--status-warning": 4.5,
  "--status-recovery": 4.5,
  "--status-danger": 4.5,
};

const blocks = themeBlocks();
const MODES = ["dark", "light"] as const;
type Mode = (typeof MODES)[number];

/** A chrome token or a `var(--x)` / hex value, as the hex it paints. */
function chrome(tokenOrValue: string, mode: Mode): string {
  const value = resolveVar(tokenOrValue, blocks[mode], blocks.dark);
  if (!value?.startsWith("#")) throw new Error(`${tokenOrValue} (${mode}) is ${value}, not a hex`);
  return value;
}

/** The shell backdrop pair, declared outside the token blocks. */
const shellCss = readIndexCss();
function shellColour(token: string, mode: Mode) {
  const values = [...shellCss.matchAll(new RegExp(`${token}:\\s*(#[a-f0-9]+)`, "g"))];
  return values[mode === "dark" ? 0 : 1][1];
}

/** Every ground text sits on, including both ends of the shell backdrop. */
function grounds(mode: Mode): Record<string, string> {
  const base = chrome("--surface-base", mode);
  return {
    base,
    raised: chrome("--surface-raised", mode),
    canvas: chrome("--background-canvas", mode),
    app: chrome("--background-app", mode),
    // index.css: --surface-well is --tint-veil at 6% in dark, 4% in light.
    well: over(chrome("--tint-veil", mode), base, mode === "dark" ? 0.06 : 0.04),
    shell: shellColour("--shell-backdrop", mode),
    shellTop: shellColour("--shell-backdrop-top", mode),
  };
}

function expectOnGrounds(colour: string, mode: Mode, floor: number) {
  for (const [name, ground] of Object.entries(grounds(mode))) {
    const ratio = contrast(colour, ground);
    expect(ratio, `${colour} on ${name} ${ground} is ${ratio.toFixed(2)}:1, floor ${floor}:1`).toBeGreaterThanOrEqual(floor);
  }
}

describe("the Mono chrome is readable on its own surfaces", () => {
  for (const mode of MODES) {
    for (const [token, floor] of Object.entries(FLOOR)) {
      it(`${mode} ${token}`, () => expectOnGrounds(chrome(token, mode), mode, floor));
    }

    it(`${mode} keeps its text ramp in order`, () => {
      const on = (token: string) => contrast(chrome(token, mode), chrome("--background-app", mode));
      expect(on("--text-primary")).toBeGreaterThan(on("--text-secondary"));
      expect(on("--text-secondary")).toBeGreaterThan(on("--text-tertiary"));
    });

    // The selected mark (active tab, selected tree row) is --status-info at
    // full opacity; non-text, 3:1 against the fill it outlines.
    it(`${mode} selected mark is visible`, () => {
      const ratio = contrast(chrome("--status-info", mode), chrome("--surface-raised", mode));
      expect(ratio).toBeGreaterThanOrEqual(3);
    });

    // A solid status fill (status pill, Stop button, git/verb chips) carries
    // a glyph in --surface-base (4.5:1) and stands off the card (3:1).
    for (const token of ["--status-success", "--status-warning", "--status-danger"]) {
      it(`${mode} ${token} as a filled pill`, () => {
        const fill = chrome(token, mode);
        expect(contrast(chrome("--surface-base", mode), fill)).toBeGreaterThanOrEqual(4.5);
        expectOnGrounds(fill, mode, 3);
      });
    }

    // The primary action: its label on its fill.
    it(`${mode} primary action label is legible`, () => {
      const ratio = contrast(chrome("--action-primary-foreground", mode), chrome("--action-primary", mode));
      expect(ratio).toBeGreaterThanOrEqual(7);
    });
  }

  // Claude's coral: a brand mark, non-text, 3:1. Read from index.css so a
  // copy here cannot pass forever after someone edits the stylesheet.
  it("a working agent's mark is visible", () => {
    for (const mode of MODES) expectOnGrounds(chrome("--vendor-claude", mode), mode, 3);
  });
});

/**
 * Every palette's colours are text on the Mono grounds: hues (chips, folder
 * names), syntax and ANSI. A family colour that misses is moved in its own
 * hue, lightness only, in families.ts.
 */
describe("every color palette is readable on the Mono grounds", () => {
  const style = new Set<string>(SYNTAX_STYLE_TOKENS);
  for (const family of Object.values(FAMILIES)) {
    for (const mode of MODES) {
      const resolved = resolvePalette(family[mode]);
      for (const token of PALETTE_TOKENS) {
        if (style.has(token)) continue;
        it(`${family.id}/${mode} ${token}`, () => {
          expectOnGrounds(chrome(resolved[token], mode), mode, 4.5);
        });
      }

      // A filled hue chip (codegraph verb chips) carries the primary action's
      // foreground as its glyph.
      it(`${family.id}/${mode} hues carry a glyph`, () => {
        const glyph = chrome("--action-primary-foreground", mode);
        for (const hue of ["--hue-red", "--hue-green", "--hue-yellow", "--hue-blue"] as const) {
          const ratio = contrast(glyph, resolved[hue]);
          expect(ratio, `${glyph} on ${resolved[hue]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5);
        }
      });
    }
  }
});
```

- [ ] **Step 2: Run**

Run: `cd frontend && bun test src/theme/themeContrast.test.ts`
Expected: PASS. If a palette hue fails, its message names the colour and ground: move that colour in `families.ts` toward darker (light mode) or lighter (dark mode), same hue, until the test passes. If `--status-*`/text fails, adjust the value in `index.css` the same way — and in the light/dark hue line of `DEVKIT_FAMILY` too if it is a devkit hue (Task 5's test pins them together).

- [ ] **Step 3: Whole theme suite is green from here on**

Run: `cd frontend && bun test src/theme`
Expected: PASS except `applyTheme.test.ts` failures caused by imports, if any — fixed in Task 7.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/theme/themeContrast.test.ts frontend/src/theme/families.ts frontend/src/index.css
git commit -m "theme: floor Mono chrome and every color palette against Mono grounds

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Apply the resolved palette

**Files:**
- Modify: `frontend/src/theme/applyTheme.ts`
- Modify: `frontend/src/theme/ThemeProvider.tsx`
- Modify: `frontend/src/dev/TreeNextGallery.tsx:318,338`
- Modify: `frontend/src/theme/applyTheme.test.ts`

- [ ] **Step 1: Failing test — only palette tokens are ever written**

Append to `frontend/src/theme/applyTheme.test.ts`:

```ts
describe("a palette never reaches the chrome", () => {
  test("the CSS for every family names only palette tokens", async () => {
    const { FAMILIES, resolvedPalettes } = await import("./families");
    const { PALETTE_TOKENS } = await import("./tokens");
    const allowed = new Set<string>(PALETTE_TOKENS);
    for (const id of Object.keys(FAMILIES) as (keyof typeof FAMILIES)[]) {
      const css = themeCss(id, resolvedPalettes(id));
      const names = [...css.matchAll(/(--[\w-]+):/g)].map((m) => m[1]);
      expect(names.filter((n) => !allowed.has(n)), id).toEqual([]);
      expect(names.length).toBe(PALETTE_TOKENS.length * 2);
    }
  });
});
```

Also in the existing `describe("themeCss", …)`: change the `palettes` fixture tokens from `--surface-base` / `--text-primary` to `--hue-red` / `--hue-blue` (values unchanged) and the matching `toContain` strings; in `"a palette with only one mode writes only that mode"` change `--surface-base` to `--hue-red`. Replace the comment above `"an empty palette writes nothing"` with `// An empty palette must produce no rule at all, or it would pin nothing at (0,3,0) for no reason.`

- [ ] **Step 2: Run to see the type failure**

Run: `cd frontend && bunx tsc --noEmit -p . 2>&1 | grep applyTheme`
Expected: errors in `applyTheme.test.ts` — the two-hue fixtures do not satisfy `ThemePalette`, which requires all eight hues. (At runtime the new test already passes; the typing is what Step 3 fixes.)

- [ ] **Step 3: applyTheme.ts takes resolved palettes**

In `frontend/src/theme/applyTheme.ts`:
- replace `import type { ThemePalette } from "./families";` with `import type { ResolvedPalette } from "./families";`
- define below the imports: `type Palettes = { dark: Partial<ResolvedPalette>; light: Partial<ResolvedPalette> };`
- change both signatures' `palettes: { dark: ThemePalette; light: ThemePalette }` to `palettes: Palettes`
- in the doc comment of `applyThemeFamily`, replace "Puts a family's palette on the document." with "Puts a Color palette on the document — hues, syntax and ANSI, resolved. Never a chrome token: the chrome is Mono and lives in index.css."

- [ ] **Step 4: ThemeProvider uses resolvedPalettes**

In `frontend/src/theme/ThemeProvider.tsx`:
- replace `import { familyOf } from "./families";` with `import { resolvedPalettes } from "./families";`
- replace
  ```ts
      const definition = familyOf(family);
      applyThemeFamily(family, { dark: definition.dark, light: definition.light });
  ```
  with
  ```ts
      applyThemeFamily(family, resolvedPalettes(family));
  ```
- in the `ThemeContextValue` comment change `/** Which palette. Orthogonal to the mode — see preferences.ThemeFamily. */` to `/** Which Color palette (hues, code, terminal). Orthogonal to the mode and to the Mono chrome. */`
- in the gate comment replace "a frame of DevKit lime before Dracula arrives" with "a frame of Mono code before Dracula's colours arrive".

- [ ] **Step 5: TreeNextGallery**

In `frontend/src/dev/TreeNextGallery.tsx` replace
```ts
      applyThemeFamily(id as ThemeFamilyPreference, { dark: definition.dark, light: definition.light });
```
with
```ts
      applyThemeFamily(id as ThemeFamilyPreference, resolvedPalettes(id as ThemeFamilyPreference));
```
and
```ts
    applyThemeFamily(current, { dark: FAMILIES[current].dark, light: FAMILIES[current].light });
```
with
```ts
    applyThemeFamily(current, resolvedPalettes(current));
```
Change the loop header `for (const [id, definition] of Object.entries(FAMILIES))` to `for (const id of Object.keys(FAMILIES))`, and add `resolvedPalettes` to the existing `@/theme/families` import.

- [ ] **Step 6: Typecheck and test**

Run: `cd frontend && bunx tsc --noEmit -p . && bun test src/theme`
Expected: no type errors; all theme tests PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/theme/applyTheme.ts frontend/src/theme/applyTheme.test.ts frontend/src/theme/ThemeProvider.tsx frontend/src/dev/TreeNextGallery.tsx
git commit -m "theme: apply the resolved color palette, never chrome

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Terminal reads ANSI tokens

**Files:**
- Modify: `frontend/src/theme/terminalTheme.ts`
- Create: `frontend/src/theme/terminalTheme.test.ts`

- [ ] **Step 1: Failing test**

`frontend/src/theme/terminalTheme.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// terminalTheme() needs a live engine to resolve colours; what it READS is
// the contract, so that is asserted against the source.
const source = readFileSync(join(import.meta.dir, "terminalTheme.ts"), "utf8")
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/\/\/.*$/gm, "");

describe("the terminal palette comes from the Color palette", () => {
  test("chromatic slots read --ansi-*", () => {
    for (const slot of ["red", "green", "yellow", "blue", "magenta", "cyan"]) {
      expect(source).toContain(`c["--ansi-${slot}"]`);
    }
  });

  test("no slot reads the status ramp", () => {
    expect(source).not.toContain("--status-");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/theme/terminalTheme.test.ts`
Expected: FAIL — source contains `--status-` and no `--ansi-`.

- [ ] **Step 3: Implement**

In `frontend/src/theme/terminalTheme.ts` replace the header comment's second paragraph ("The sixteen ANSI slots come off the status ramp … is what "bright" means on every real terminal palette.") with:

```ts
 * The six chromatic ANSI slots are the Color palette's `--ansi-*` (derived
 * from its hues, contrast-checked against the Mono grounds). Black, white and
 * the foreground come from the chrome. Bright variants lift toward the
 * foreground, which is what "bright" means on every real terminal palette.
```

Replace `TOKENS` with:

```ts
const TOKENS = [
  "--text-primary",
  "--text-secondary",
  "--text-tertiary",
  "--surface-tinted",
  "--brand-text",
  "--background-app",
  "--ansi-red",
  "--ansi-green",
  "--ansi-yellow",
  "--ansi-blue",
  "--ansi-magenta",
  "--ansi-cyan",
] as const;
```

Replace the returned object's colour lines (from `red:` through `brightCyan:`) with:

```ts
    red: c["--ansi-red"],
    green: c["--ansi-green"],
    yellow: c["--ansi-yellow"],
    blue: c["--ansi-blue"],
    magenta: c["--ansi-magenta"],
    cyan: c["--ansi-cyan"],
    white: c["--text-secondary"],
    // "Bright" is the same hue carried toward the foreground — the relationship
    // every terminal palette encodes, rather than eight more invented colours.
    brightBlack: c["--text-tertiary"],
    brightRed: lift(c["--ansi-red"], fg, 30),
    brightGreen: lift(c["--ansi-green"], fg, 30),
    brightYellow: lift(c["--ansi-yellow"], fg, 30),
    brightBlue: lift(c["--ansi-blue"], fg, 30),
    brightMagenta: lift(c["--ansi-magenta"], fg, 30),
    brightCyan: lift(c["--ansi-cyan"], fg, 30),
```

(`black`, `cursor`, `cursorAccent`, `selectionBackground`, `foreground`, `brightWhite` stay as they are.)

- [ ] **Step 4: Run**

Run: `cd frontend && bun test src/theme/terminalTheme.test.ts && bunx tsc --noEmit -p .`
Expected: PASS, no type errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/theme/terminalTheme.ts frontend/src/theme/terminalTheme.test.ts
git commit -m "terminal: ANSI colours come from the color palette

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Keyword weight and link colour in code

**Files:**
- Modify: `frontend/src/components/code-editor/devkitSyntax.ts:25,49,50`
- Modify: `frontend/src/components/ui/code-block-node.tsx:55`
- Modify: `frontend/src/components/ui/code-block-node-static.tsx:26`
- Create: `frontend/src/components/code-editor/devkitSyntax.test.ts`

- [ ] **Step 1: Failing test**

`frontend/src/components/code-editor/devkitSyntax.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (path: string) => readFileSync(join(import.meta.dir, path), "utf8");

describe("code takes its weight and links from the palette", () => {
  test("the lezer table weights keywords and colours links by hue", () => {
    const src = read("devkitSyntax.ts");
    expect(src).toContain('class: "text-(--syntax-keyword) font-(--syntax-keyword-weight)"');
    expect(src).toContain('class: "text-(--hue-blue) underline"');
    expect(src).toContain('class: "text-(--hue-blue)"');
    expect(src).not.toContain("--status-info");
  });

  test("both Plate code blocks weight hljs keywords", () => {
    for (const file of ["../ui/code-block-node.tsx", "../ui/code-block-node-static.tsx"]) {
      expect(read(file)).toContain(
        "**:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-selector-tag,.hljs-variable.language\\\\_]:font-(--syntax-keyword-weight)",
      );
    }
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/components/code-editor/devkitSyntax.test.ts`
Expected: FAIL on the first `toContain`.

- [ ] **Step 3: Implement**

In `devkitSyntax.ts`:
- line 25: `class: "text-(--syntax-keyword)"` → `class: "text-(--syntax-keyword) font-(--syntax-keyword-weight)"`
- line 49: `class: "text-(--status-info) underline"` → `class: "text-(--hue-blue) underline"`
- line 50: `class: "text-(--status-info)"` → `class: "text-(--hue-blue)"`
- In the header comment replace "Tags map onto --syntax-* tokens, and those default to the status ramp (index.css), because … floors them like the ramp." with "Tags map onto --syntax-* tokens, which the Color palette supplies (theme/families.ts resolves them from its hues unless the family names its own). Mono (devkit) draws code in the text ramp and tells keywords apart by --syntax-keyword-weight."

In `code-block-node.tsx` (line 55) and `code-block-node-static.tsx` (line 26), immediately after the substring
`**:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-selector-tag,.hljs-variable.language\\_]:text-(--syntax-keyword) `
insert
`**:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-selector-tag,.hljs-variable.language\\_]:font-(--syntax-keyword-weight) `

- [ ] **Step 4: Verify the class is generated as font-weight**

Run: `cd frontend && bunx vite build --mode production >/dev/null && grep -o "font-weight:var(--syntax-keyword-weight)" dist/assets/*.css | head -1`
Expected: one match. (If it prints nothing, Tailwind read `font-(--x)` as a font family: replace every `font-(--syntax-keyword-weight)` written in this task and in Task 10 with `font-(weight:--syntax-keyword-weight)`, update the test strings the same way, and re-run.)

- [ ] **Step 5: Run tests + lint**

Run: `cd frontend && bun test src/components/code-editor/devkitSyntax.test.ts && bunx eslint src/components/code-editor/devkitSyntax.ts src/components/ui/code-block-node.tsx src/components/ui/code-block-node-static.tsx`
Expected: PASS; eslint prints nothing.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/code-editor/ frontend/src/components/ui/code-block-node.tsx frontend/src/components/ui/code-block-node-static.tsx
git commit -m "code: keyword weight from the palette, links in the blue hue

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Settings — "Color palette"

**Files:**
- Create: `frontend/src/modules/settings/PaletteSwatch.tsx` (moved out of SettingsView so it renders in a test without the bridge)
- Modify: `frontend/src/modules/settings/SettingsView.tsx:20,355-376,434-442`
- Create: `frontend/src/modules/settings/paletteSwatch.test.tsx`

- [ ] **Step 1: Failing test**

`frontend/src/modules/settings/paletteSwatch.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { PaletteSwatch } from "./PaletteSwatch";
import { FAMILIES } from "@/theme/families";

describe("PaletteSwatch", () => {
  test("shows eight hues and a line of code in the palette's colours", () => {
    const html = renderToString(<PaletteSwatch family="dracula" mode="dark" />);
    expect(html.match(/data-hue=/g)).toHaveLength(8);
    expect(html).toContain(`--hue-red:${FAMILIES.dracula.dark["--hue-red"]}`);
    expect(html).toContain("text-(--syntax-keyword)");
    expect(html).toContain("text-(--syntax-string)");
  });

  test("never paints chrome", () => {
    const html = renderToString(<PaletteSwatch family="dracula" mode="dark" />);
    expect(html).not.toContain("bg-action-primary");
    expect(html).not.toContain("--surface-base:");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/modules/settings/paletteSwatch.test.tsx`
Expected: FAIL — `Cannot find module './PaletteSwatch'`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/settings/PaletteSwatch.tsx`:

```tsx
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
```

In `SettingsView.tsx`:
- delete the old `PaletteSwatch` doc comment and function (lines ~355–376);
- add `import { PaletteSwatch } from "./PaletteSwatch";`;
- keep the `FAMILIES` import on line 20 (the picker loop still uses it);

- in `AppearanceTab`, replace `<GroupLabel>Palette</GroupLabel>` with `<GroupLabel>Color palette</GroupLabel>`, `aria-label="Palette"` with `aria-label="Color palette"`, and the toast text `Could not save the palette:` with `Could not save the color palette:`
- replace the comment above that RadioGroup (`{/* Swatches, not a dropdown: … */}`) with:

```tsx
        {/* Swatches, not a dropdown: a palette is the one setting where the
            value IS what it looks like. The chrome stays Mono whichever you
            pick; this changes code, the terminal and coloured details. */}
```

- [ ] **Step 4: Run**

Run: `cd frontend && bun test src/modules/settings && bunx eslint src/modules/settings/SettingsView.tsx`
Expected: PASS; eslint silent.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/settings/PaletteSwatch.tsx frontend/src/modules/settings/SettingsView.tsx frontend/src/modules/settings/paletteSwatch.test.tsx
git commit -m "settings: Color palette picker shows hues and code

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Decorative colour moves to hues

Categorical colour (which kind, not how safe) moves from `--status-*` to `--hue-*`. Semantic uses (running, selection, focus, errors, recovery notices) stay on status. This is the audited list; nothing else changes.

**Files:**
- Modify: `frontend/src/modules/codegraph/graph/tokens.ts:14-17,32-39`
- Modify: `frontend/src/modules/codegraph/graph/LayerLegend.tsx:39`
- Modify: `frontend/src/modules/codegraph/graph/ConnectorEdge.tsx:87`
- Modify: `frontend/src/modules/database/plan/PlanDiagram.tsx:39-42,99`
- Modify: `frontend/src/modules/database/components/ERDView.tsx:131,169,171,180,182,195,197,241,897,1112`
- Modify: `frontend/src/modules/database/icons/engineMarks.tsx` (three `fill="var(--status-…)"`)
- Modify: `frontend/src/modules/apiclient/requestModel.ts:150-163` (`methodTone` only; the status-code colours at 186–188 stay)
- Create: `frontend/src/decorativeHues.test.ts`

- [ ] **Step 1: Failing test**

`frontend/src/decorativeHues.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * Categorical colour reads the Color palette's hues, never status: the chrome
 * is Mono, so status is reserved for how safe something is. These files were
 * audited when the split landed (docs/superpowers/specs/2026-10-13-…).
 */
const FILES = [
  "modules/codegraph/graph/tokens.ts",
  "modules/codegraph/graph/LayerLegend.tsx",
  "modules/codegraph/graph/ConnectorEdge.tsx",
  "modules/database/icons/engineMarks.tsx",
];

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

describe("decorative colour is a hue", () => {
  for (const file of FILES) {
    test(file, () => {
      const src = strip(readFileSync(join(import.meta.dir, file), "utf8"));
      expect(src).not.toMatch(/status-(info|success|warning|recovery)/);
      expect(src).toMatch(/hue-/);
    });
  }

  // requestModel.ts also colours HTTP status CODES (2xx/3xx/5xx), which is
  // semantic and stays on status; only the method tone is categorical.
  test("API client method tone", () => {
    const src = strip(readFileSync(join(import.meta.dir, "modules/apiclient/requestModel.ts"), "utf8"));
    const body = src.slice(src.indexOf("export function methodTone"), src.indexOf("}\n", src.indexOf("export function methodTone")));
    expect(body).not.toMatch(/status-/);
    expect(body).toMatch(/hue-/);
  });

  test("plan diagram steps and ERD keys are hues", () => {
    const plan = strip(readFileSync(join(import.meta.dir, "modules/database/plan/PlanDiagram.tsx"), "utf8"));
    expect(plan).not.toMatch(/cssVar: "--status-/);
    const erd = strip(readFileSync(join(import.meta.dir, "modules/database/components/ERDView.tsx"), "utf8"));
    expect(erd).not.toMatch(/status-info/);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/decorativeHues.test.ts`
Expected: FAIL for every file.

- [ ] **Step 3: CodeGraph**

`modules/codegraph/graph/tokens.ts` — `LAYERS`:
```ts
  controller: { cssVar: "--hue-blue", icon: Globe, label: "CONTROLLER" },
  service: { cssVar: "--hue-green", icon: Zap, label: "SERVICE" },
  repository: { cssVar: "--hue-yellow", icon: Database, label: "REPOSITORY" },
```
`verbChipClass` return values:
```ts
    case "GET":
      return "bg-(--hue-blue) text-action-primary-foreground";
    case "POST":
      return "bg-(--hue-green) text-action-primary-foreground";
    case "DELETE":
      return "bg-(--hue-red) text-action-primary-foreground";
    case "PUT":
    case "PATCH":
      return "bg-(--hue-yellow) text-action-primary-foreground";
```
`LayerLegend.tsx:39`: `border-status-info` → `border-(--hue-blue)`.
`ConnectorEdge.tsx:87`: `const info = "var(--status-info)";` → `const info = "var(--hue-blue)";`

- [ ] **Step 4: Database**

`PlanDiagram.tsx` `STEPS`:
```ts
  scan: { cssVar: "--hue-yellow", icon: Table2, label: "SCAN" },
  join: { cssVar: "--hue-blue", icon: GitMerge, label: "JOIN" },
  sort: { cssVar: "--hue-green", icon: ArrowDownUp, label: "SORT" },
  aggregate: { cssVar: "--hue-green", icon: Sigma, label: "AGGREGATE" },
```
and its comment `/** The code graph's layer hues, by the same logic: storage is warning, flow is info. */` → `/** The code graph's layer hues, by the same logic: storage is yellow, flow is blue. */`. Line 99: `"bg-status-info"` → `"bg-(--hue-blue)"` (the `bg-status-danger` for the hottest node stays).

`ERDView.tsx`: on lines 131, 171, 182, 197, 1112 replace `status-info` with `(--hue-blue)` in each class (`text-status-info` → `text-(--hue-blue)`, `border-status-info/40` → `border-(--hue-blue)/40`, `bg-status-info` → `bg-(--hue-blue)`); on lines 169, 180, 195 replace `status-warning` the same way with `(--hue-yellow)` (`text-status-warning` → `text-(--hue-yellow)`, `border-status-warning/40` → `border-(--hue-yellow)/40`). Lines 241 and 897: `"var(--status-info)"` → `"var(--hue-blue)"`. Line 1102 (`bg-status-warning/10 text-status-warning`, a warning notice) stays.

`engineMarks.tsx`: `fill="var(--status-info)"` → `fill="var(--hue-blue)"`, `fill="var(--status-warning)"` → `fill="var(--hue-yellow)"`, `fill="var(--status-recovery)"` → `fill="var(--hue-violet)"`. In its header comment replace "As tokens they follow the mode and the palette" with "As hues they follow the mode and the Color palette".

- [ ] **Step 5: API client**

`requestModel.ts` `methodTone`:
```ts
    case "GET":
      return "text-(--hue-green)";
    case "POST":
      return "text-(--hue-yellow)";
    case "PUT":
    case "PATCH":
      return "text-(--hue-blue)";
    case "DELETE":
      return "text-(--hue-red)";
```

- [ ] **Step 6: Run everything that could pin these classes**

Run: `cd frontend && bun test src/decorativeHues.test.ts src/modules/codegraph src/modules/database src/modules/apiclient && bunx eslint src/modules/codegraph src/modules/database src/modules/apiclient`
Expected: PASS; eslint silent. If a module test asserts an old class (e.g. `text-status-success` for GET), update that assertion to the new hue class — the test was pinning the colour, and the colour is what changed.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/decorativeHues.test.ts frontend/src/modules/codegraph frontend/src/modules/database frontend/src/modules/apiclient
git commit -m "ui: categorical colour reads palette hues, not status

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Comments and docs

**Files:**
- Modify: `internal/preferences/preferences.go:21-31`
- Modify: `internal/bridge/preferences.go:62`
- Modify: `docs/design/devkit-ui-ux-rules.md:216,370`
- Modify: `AGENTS.md` (add one paragraph)
- Modify: `frontend/src/dev/ComponentGallery.tsx:109,224,273`

- [ ] **Step 1: Go comments**

`internal/preferences/preferences.go`, replace the `ThemeFamily` doc comment with:

```go
// ThemeFamily is the person's Color palette: the hues, code syntax colours
// and terminal colours. The chrome is one Mono design and no family changes
// it. It is a second preference rather than more values on Theme: Theme is
// the MODE, which the native window appearance is set from, and a palette
// supplies both modes, so "Dracula, following the system" is expressible.
```

and the `ThemeFamilyDevKit` comment with:

```go
	// ThemeFamilyDevKit is the Mono palette in index.css: muted hues, code in
	// the text ramp. It is the default.
```

`internal/bridge/preferences.go:62`: `// PreferencesGetThemeFamily reads which palette the UI is painted with.` → `// PreferencesGetThemeFamily reads the person's Color palette (hues, code, terminal).`

- [ ] **Step 2: Design rules**

`docs/design/devkit-ui-ux-rules.md` line 216 → 
`- Khung (chrome) là một thiết kế Mono duy nhất; theme family là "Color palette": chỉ quyết định 8 hue, màu syntax và màu terminal. Màu trạng thái (status) thuộc khung, không đổi theo palette. Màu mang tính phân loại (chip, layer, thư mục note) dùng `--hue-*`, không dùng `--status-*`.`

line 370 → 
`- Nền app riêng theo dark/light: `--shell-backdrop` và `--shell-backdrop-top` (xám trung tính), gradient nhẹ theo chiều dọc.`

- [ ] **Step 3: AGENTS.md**

Add after the "### Module UI: shared sidebar and document tabs" section:

```markdown
### Theme: Mono chrome + Color palette
The chrome is one monochrome design (`frontend/src/index.css`, light and dark); nothing
the person picks changes it. The theme family (`theme_family`, Settings → "Color palette")
supplies only `--hue-*` (8 per mode), `--syntax-*` and `--ansi-*`: `theme/families.ts`
declares hues (+ optional syntax) and `resolvePalette` derives the rest; `applyThemeFamily`
writes only palette tokens. Status (`--status-*`) belongs to the chrome — use it for how
safe something is; use `--hue-*` for which kind it is. `bun test src/theme` floors every
palette colour at 4.5:1 on every Mono ground. Design:
`docs/superpowers/specs/2026-10-13-mono-chrome-color-palettes-design.md`.
```

- [ ] **Step 4: Gallery literals**

`frontend/src/dev/ComponentGallery.tsx`: line 109 `"#c6ff34"` → `"var(--hue-green)"`, `"#99e1d9"` → `"var(--hue-teal)"`; line 224 `dotColor="#c6ff34"` → `dotColor="var(--hue-green)"`; line 273 label text `On #c6ff34` → `On action-primary`.

- [ ] **Step 5: Verify**

Run: `cd /home/ptvu3/Downloads/dev-kit && gofmt -l internal/preferences internal/bridge && go test ./internal/preferences/ ./internal/bridge/ -count=1`
Expected: gofmt prints nothing; tests `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/preferences/preferences.go internal/bridge/preferences.go docs/design/devkit-ui-ux-rules.md AGENTS.md frontend/src/dev/ComponentGallery.tsx
git commit -m "docs: describe Mono chrome and the Color palette

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Full verification

- [ ] **Step 1: Whole frontend suite + build**

Run: `cd frontend && bun test 2>&1 | tail -5 && bun run build 2>&1 | tail -5`
Expected: `0 fail`; build succeeds (includes `tsc` and the performance budget). Any failure that pins an old colour (`#c6ff34`, `border-focus`, `status-info` in a decorative role) is fixed by updating the assertion only if the new value is what the spec asks for; anything else is a regression — fix the code.

- [ ] **Step 2: Lint**

Run: `cd frontend && bunx eslint src`
Expected: no errors.

- [ ] **Step 3: Go regression**

Run: `cd /home/ptvu3/Downloads/dev-kit && go test ./internal/arch/ ./internal/preferences/ ./internal/bridge/ -count=1`
Expected: all `ok`.

- [ ] **Step 4: Visual check in server mode**

```bash
cd /home/ptvu3/Downloads/dev-kit && go build -tags server -o bin/dev-kit-server . && WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=34115 ./bin/dev-kit-server
```
Open `http://127.0.0.1:34115` (Playwright or a browser), create/unlock the vault, then capture screenshots in light and dark for palettes `devkit`, `dracula`, `solarized`:
1. the workbench with the sidebar open,
2. a code file in CodeGraph's editor,
3. a Notes code block,
4. Settings → Appearance (the Color palette cards).

Check: no lime or candy-blue anywhere in the chrome; the chrome is identical across the three palettes; code colours change with the palette; devkit code is monochrome with bold keywords; focus rings are visible (Tab through Settings).

- [ ] **Step 5: Report**

Summarise test counts, build result and the screenshots to the user. Do not merge or push without being asked.
