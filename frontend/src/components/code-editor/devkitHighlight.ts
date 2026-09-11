import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

/**
 * Syntax highlighting in the app's own colours.
 *
 * Replaces the oneDark/nothing swap, which was two hard-coded palettes: a
 * single fixed dark theme, and the browser's defaults for light. Neither
 * followed a theme family, so a Dracula app had a One Dark editor in it.
 *
 * Written as `var(--token)` rather than resolved values, which is the whole
 * trick: CodeMirror renders to the DOM, so a rule referencing a custom
 * property re-resolves on its own the instant the palette changes. No
 * per-family editor theme, no dependency per family, no recomputation on a
 * switch, and a family added later is covered without touching this file.
 *
 * Tags map onto the status ramp rather than onto new colours, because a
 * palette that has to name a syntax colour is a palette most families do not
 * publish — and the ramp is already contrast-checked against the surfaces.
 */
const highlightStyle = HighlightStyle.define([
  // Keywords and control flow: the accent the theme reserves for "this is the
  // structure", which every family gives us as its brand text colour.
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword], color: "var(--brand-text)" },
  { tag: [t.operator, t.operatorKeyword, t.punctuation], color: "var(--text-secondary)" },

  // Names. The definition is the primary text; a reference to something the
  // language owns is toned down, and a type sits with the informational hue.
  { tag: [t.variableName, t.propertyName], color: "var(--text-primary)" },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: "var(--status-info)" },
  { tag: [t.typeName, t.className, t.namespace], color: "var(--status-recovery)" },
  { tag: [t.definition(t.variableName), t.definition(t.propertyName)], color: "var(--text-primary)" },

  // Literals.
  { tag: [t.string, t.special(t.string), t.regexp], color: "var(--status-success)" },
  { tag: [t.number, t.bool, t.null, t.atom], color: "var(--status-warning)" },

  // Everything that is not the code itself.
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: "var(--text-tertiary)", fontStyle: "italic" },
  { tag: [t.meta, t.annotation], color: "var(--text-tertiary)" },
  { tag: t.invalid, color: "var(--status-danger)" },

  // Markdown and the like, which the notes editor renders.
  { tag: t.heading, color: "var(--text-primary)", fontWeight: "600" },
  { tag: t.strong, fontWeight: "600" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: t.link, color: "var(--status-info)", textDecoration: "underline" },
  { tag: t.url, color: "var(--status-info)" },
]);

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
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "var(--surface-tinted)",
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
export const devkitHighlight = [editorChrome, syntaxHighlighting(highlightStyle)];
