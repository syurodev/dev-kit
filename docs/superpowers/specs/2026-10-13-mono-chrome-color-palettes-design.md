# Mono chrome + Color palettes — design

Status: approved in conversation 2026-10-13, awaiting spec review.

## Goal

Split the theme into two independent axes:

1. **Chrome** — the shell, sidebar, tabs, buttons, inputs, cards, dialogs. One
   design only: **Mono** (white / greys / black, no accent hue), in light and
   dark. The old DevKit chrome (lime `#c6ff34`, candy-blue, lavender surfaces)
   is removed.
2. **Color palette** — what the existing theme family (`devkit`, Catppuccin,
   Dracula, …) now means. It no longer paints the chrome. It supplies the
   app's *hues*: code syntax, terminal ANSI colours and decorative details
   (note folder colours, category chips, CodeGraph layers, badges, later chart
   series).

Mode (System / Light / Dark) stays a third, orthogonal preference, as today.

Reference look: monochrome "quiet UI" — surfaces separated by grey steps rather
than borders or colour, a single solid black/white primary action, colour only
where it carries information.

## Non-goals

- Typography, font sizes, radii, pill buttons, spacing. Colour only.
- Changing the Go preference: key `theme_family`, `preferences.ThemeFamily`,
  its values and the bridge methods stay as they are (no migration). Only doc
  comments change.
- Giving palettes a code background. Code surfaces (editor, terminal, code
  blocks) sit on the Mono grounds; a palette colours text only.
- Mermaid. It reads chrome tokens and becomes Mono by itself.

## 1. Chrome: Mono (`frontend/src/index.css`)

`index.css` is the single source of chrome tokens. Families may no longer set
any of them.

Starting values; `themeContrast.test.ts` is the arbiter and a value that misses
its floor moves in lightness, not hue.

| Token | Light | Dark |
|---|---|---|
| `--background-app` | `#f7f7f7` | `#000000` |
| `--background-canvas` | `#fafafa` | `#0a0a0a` |
| `--surface-base` | `#ffffff` | `#141414` |
| `--surface-raised` | `#efefef` | `#1c1c1c` |
| `--surface-tinted` | `#ebebeb` | `#262626` |
| `--text-primary` | `#0a0a0a` | `#fafafa` |
| `--text-secondary` | `#525252` | `#a3a3a3` |
| `--text-tertiary` | `#666666` | `#8f8f8f` |
| `--icon-secondary` | = text-secondary | = text-secondary |
| `--action-primary` | `#0a0a0a` | `#fafafa` |
| `--action-primary-foreground` | `#ffffff` | `#0a0a0a` |
| `--action-primary-hover` / `-pressed` | 85% / 75% mix of the fill with white | 88% / 78% mix of the fill with black |
| `--brand-text` | = text-primary | = text-primary |
| `--border-focus` | = text-primary | = text-primary |
| `--tint-veil` / `--tint-border` | `#0a0a0a` | `#ffffff` |
| `--tint-specular` | unchanged | unchanged |

Hover and pressed move **toward the page** (a `color-mix` of `--action-primary`
with the page colour, not the `states()` rule that lifts or recesses a
surface), so the solid fill stays the strongest thing on the screen: dark mixes
with black (88% / 78%), light with white (85% / 75%).

Status keeps its hues, desaturated, and is **not** part of a palette: an error
must look like an error whichever palette is chosen.

| Token | Light | Dark |
|---|---|---|
| `--status-danger` | `#b3363f` | `#e07b7f` |
| `--status-warning` | `#8a6100` | `#d9b25c` |
| `--status-success` | `#2b7350` | `#7cc59a` |
| `--status-recovery` | `#86508a` | `#c39bd0` |
| `--status-info` | = text-secondary (grey) | = text-secondary (grey) |

Consequences, accepted:

- `--feedback-selected` derives from `--status-info`, so selected grid cells
  become a grey wash.
- The raw DevKit brand tokens (`--palette-onyx` … `--palette-lime`) are deleted;
  their few uses in `index.css` are rewritten onto the tokens above.
- `--vendor-claude` stays (a vendor's identity, not decoration).
- Glass materials keep their mechanism; their tints already derive from
  `--tint-*` and `--surface-base`, so they turn neutral without changes.

## 2. Color palette (`frontend/src/theme/families.ts`, `tokens.ts`)

A palette supplies three layers per mode. Each layer defaults to the one
before it, so a palette that declares only its 8 hues is complete.

1. **Hues** — new `HUE_TOKENS`: `--hue-red`, `--hue-orange`, `--hue-yellow`,
   `--hue-green`, `--hue-teal`, `--hue-blue`, `--hue-violet`, `--hue-pink`.
   Required for every palette, both modes. Taken from the family's official
   palette (its light variant for light, dark variant for dark), adjusted in
   lightness only where the floor demands.
2. **Syntax** — existing `SYNTAX_TOKENS`. `index.css` carries the `devkit`
   palette's Mono syntax (keyword/type/function/definition `--text-primary`,
   keyword weight 600, string/number `--text-secondary`, comment/meta
   `--text-tertiary`). Every other palette that does not declare a token gets
   it from its hues, derived in TypeScript by `resolvePalette`
   (`SYNTAX_FROM_HUE` in `families.ts`; written out as literals so the Settings
   swatch works on any element):
   - keyword ← `--hue-violet`
   - string ← `--hue-green`
   - number ← `--hue-orange`
   - type ← `--hue-yellow`
   - function ← `--hue-blue`
   - definition ← `--text-primary`
   - comment, meta ← `--text-tertiary`

   A palette with its own mapping (Dracula, Xcode, …) overrides individual
   tokens. Optional.
3. **Terminal** — new `TERMINAL_TOKENS`: `--ansi-red`, `--ansi-green`,
   `--ansi-yellow`, `--ansi-blue`, `--ansi-magenta`, `--ansi-cyan`, defaulting
   in `index.css` to red / green / yellow / blue / violet / teal hues.
   `terminalTheme.ts` reads these instead of `--status-*` and `--brand-text`.
   Black, white, foreground, cursor and selection come from chrome tokens;
   bright variants keep the existing "lift toward the foreground" rule.
   Optional per palette.

`ThemePalette` becomes `Partial<Record<HueToken | SyntaxToken | TerminalToken, string>>`
(hues required by test, not by type). `TINT_TOKENS`, `THEME_TOKENS` and
`MIX_TOKENS` are no longer accepted from a family.

`--note-folder-*` leave `THEME_TOKENS` and derive from hues in `index.css`
(red→red, orange→orange, …, gray → `--text-tertiary`). Saved notes keep their
colour *names*; only the rendered value follows the palette.

### The `devkit` palette = Mono

Id and label stay `devkit` / "DevKit"; it remains the default.

- Hues: the desaturated set, same saturation as status
  (light: red `#b3363f`, orange `#a14f1a`, yellow `#8a6100`, green `#2b7350`,
  teal `#1f7470`, blue `#3a62a8`, violet `#6e55a8`, pink `#a4467a`;
  dark: red `#e07b7f`, orange `#e0945e`, yellow `#d9b25c`, green `#7cc59a`,
  teal `#6cc1bb`, blue `#8aaee6`, violet `#b29ce6`, pink `#de8fb8`).
- Syntax: near-monochrome, the one palette that does not take syntax from its
  hues — keyword and type/function `--text-primary` (keyword bold via the
  highlighter's existing weight hook, added if missing), string
  `--text-secondary`, number `--text-secondary`, comment/meta
  `--text-tertiary` italic.
- Terminal: default (the muted hues), so `git diff` and errors keep red/green.

### Who may use `--hue-*`

Decorative or categorical detail only — things whose colour distinguishes
*which*, never *how safe*: note folders, category/kind chips, CodeGraph
layers, avatars and badges, future chart series. Danger/warning/success always
use `--status-*`.

### Audit of current status uses

About 180 places use `--status-info` / `--status-recovery`. With info turning
grey, each is classified:

- semantic (an info/recovery state) → keep the status token;
- decorative (a colour picked to tell kinds apart) → move to a `--hue-*`;
- unsure → keep the status token (grey is the safe default).

The implementation plan lists the files; the classification is reviewed in
the PR.

## 3. Settings (`modules/settings/SettingsView.tsx`)

- Mode picker unchanged.
- The family picker is labelled **"Color palette"**.
- Each card shows the palette's 8 hues as a strip and one line of sample code
  in its syntax colours, on the Mono ground of the resolved mode. Cards no
  longer paint themselves in the family's surfaces (`PaletteSwatch` applies
  only hue/syntax tokens inline).

## 4. Runtime (`applyTheme.ts`, `ThemeProvider.tsx`)

Unchanged mechanism: one `<style id="devkit-theme">` written under
`:root[data-theme-family][data-theme]`, transitions suppressed across the
swap. It now carries only hue/syntax/terminal tokens, so a palette switch never
recolours chrome. The terminal re-reads its palette on palette change as it
does today.

Comments in `ThemeProvider.tsx` and `internal/preferences/preferences.go`
that describe the family as "which palette the UI is painted with" are updated
to "which color palette (hues, code, terminal)".

## 5. Tests

- `themeContrast.test.ts`:
  - chrome text and status tokens floored against every Mono ground, both
    modes (existing tiers: 7 / 5.5 / 4.5);
  - for every palette and mode: each hue, each syntax token (declared or
    derived) and each ANSI colour ≥ 4.5:1 on every Mono code ground.
- `tokenContract.test.ts`:
  - `DEVKIT_FAMILY` matches the hue/syntax defaults in `index.css`;
  - every family declares all 8 hues in both modes;
  - no family declares a chrome token (`TINT_TOKENS`, `THEME_TOKENS`,
    `MIX_TOKENS`).
- Update `applyTheme.test.ts`, `step1Components.test.tsx` (lime fixture),
  `dev/ComponentGallery.tsx`, `dev/TreeNextGallery.tsx`.
- Gate: `cd frontend && bun test && bun run build`; Go:
  `go test ./internal/preferences/ ./internal/bridge/ -count=1` (comments only,
  must stay green).
- Visual check: `task run:server`, Playwright screenshots of the workbench,
  a code editor, the terminal and Settings in light and dark with `devkit`,
  Dracula and Solarized.

## Decisions made after the plan

- **Running agents** use `--status-running` = `--text-primary`. With info
  grey, the old running dot was ~1.2:1 from idle (`--text-tertiary`);
  `themeContrast.test.ts` now requires at least 1.5:1 between them.
- **Diffs keep their status colours**: added is `--status-success` (tint
  `--feedback-success-soft`), removed `--status-danger`, in the shared diff,
  the file diff pane, code blocks and the terminal. (`brand-lime` was never a
  defined utility; its uses read `text-status-success`.)
- **Focus**: rings and borders use `--border-focus` (= `--text-primary`);
  rings are drawn at 50% (`ring-border-focus/50`), borders at full strength.
  `focusRing.test.ts` forbids translucent status-info rings and status-info
  focus borders. The link button is primary text, always underlined.
- **Light grounds**: app `#f7f7f7` and raised `#efefef`, so a raised surface
  stands off both the app background and the base (>= 1.06:1, tested).
- **More categorical moves** (colour tells *which*, not *how safe*): git
  untracked `U` and renamed `R` chips (`--hue-blue`, `--hue-violet`), Files
  tab `R` (`--hue-violet`), CodeGraph data-flow read (`--hue-blue`) and the
  result grid's key icon (`--hue-yellow`, like the ERD primary key).
  `decorativeHues.test.ts` pins them. The glyph-on-fill check covers
  red, green, yellow, blue and violet.
- **Near-duplicate hues**: pink is nudged away from red in Everforest, Gruvbox,
  Nord and Ayu dark (and Gruvbox light), staying above 4.5:1 on the grounds.

## Risks

- Some family hues will not clear 4.5:1 on a white or near-black ground they
  were not designed for (e.g. Dracula yellow on white). Handled by the
  lightness-only adjustment rule; the test names each one.
- Losing candy-blue focus rings and lime makes focus/selection rely on
  contrast alone. `--border-focus` = text-primary gives the strongest ring
  available; verified in the visual check.
- Decorative uses missed by the audit turn grey rather than wrong — acceptable
  and fixable later.
