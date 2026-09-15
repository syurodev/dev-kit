import { syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { devkitSyntax } from "./devkitSyntax";

/**
 * Syntax highlighting in the app's own colours.
 *
 * Replaces the oneDark/nothing swap, which was two hard-coded palettes: a
 * single fixed dark theme, and the browser's defaults for light. Neither
 * followed a theme family, so a Dracula app had a One Dark editor in it.
 *
 * The colours themselves are devkitSyntax (devkitSyntax.ts): `var(--token)`
 * classes, so a rule re-resolves on its own the instant the palette changes —
 * no per-family editor theme, no recomputation on a switch. The same table
 * colours code in the Agent Hub transcript, which has no editor to lean on.
 */

/**
 * The editor's own chrome, in the same tokens.
 *
 * Background is transparent on purpose — .cm-surfaceless in index.css already
 * hands the editor its surrounding surface, and painting one here would fight
 * it. What is left is the parts CSS cannot reach: the caret, the selection
 * layer and the gutter.
 */
const editorChrome = EditorView.theme({
  "&": { color: "var(--text-primary)" },
  ".cm-content": { caretColor: "var(--brand-text)" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--brand-text)" },
  /*
   * The selection, in the app's own tint — and written the long way ON
   * PURPOSE.
   *
   * CodeMirror's default light theme carries
   * `.ͼ2.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground`,
   * five classes of specificity, for a pale lavender mixed to sit under
   * dark-on-light syntax. The short selector below it lost every time, so
   * every editor in this app has been selecting text in #d7d4f0 with
   * colours mixed for a dark ground on top of it — a band you cannot read,
   * in the one control where selecting is how you choose what to run.
   *
   * Matching the shape ties the specificity, and a tie goes to whoever is
   * registered later: this theme, which is added after basicSetup.
   *
   * --editor-selection, not --surface-tinted, which is what this rule asked
   * for before anyone could see it apply: measured 1.06:1 against the
   * editor's own ground, so the selection was there and invisible. The new
   * one is the selected-cell wash at a strength each mode can carry — a
   * tint, so every token keeps the colour that says what it is, and
   * selecting rows and selecting SQL are one gesture in one colour.
   */
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground": {
    backgroundColor: "var(--editor-selection)",
  },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "var(--editor-selection)",
  },
  ".cm-activeLine": { backgroundColor: "var(--state-soft)" },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "var(--text-tertiary)",
    borderRight: "1px solid var(--border-subtle)",
  },
  ".cm-activeLineGutter": { backgroundColor: "var(--state-soft)", color: "var(--text-secondary)" },
  ".cm-selectionMatch": { backgroundColor: "var(--state-soft)" },
  "&.cm-focused .cm-matchingBracket": {
    backgroundColor: "var(--state-soft)",
    outline: "1px solid var(--border-default)",
  },
  ".cm-foldPlaceholder": {
    backgroundColor: "var(--surface-raised)",
    border: "1px solid var(--border-subtle)",
    color: "var(--text-secondary)",
  },
});

/** One extension, both modes, every family. */
export const devkitHighlight = [editorChrome, syntaxHighlighting(devkitSyntax)];
