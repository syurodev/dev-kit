import { tagHighlighter, tags as t } from "@lezer/highlight";

/**
 * Syntax colours in the app's own tokens, as classes.
 *
 * One table for every place that colours code: the CodeMirror editors take it
 * through syntaxHighlighting (devkitHighlight.ts), and the Agent Hub transcript
 * runs it over a static parse with highlightCode. Before, the editor's colours
 * lived in a HighlightStyle and a transcript would have needed a second copy.
 *
 * Classes rather than inline styles: Tailwind emits them from this file, they
 * work without CodeMirror injecting a stylesheet, and `var(--token)` still
 * re-resolves the instant the palette changes.
 *
 * Tags map onto --syntax-* tokens, which the Color palette supplies
 * (theme/families.ts resolves them from its hues unless the family names its
 * own). Mono (devkit) draws code in the text ramp and tells keywords apart by
 * --syntax-keyword-weight.
 */
export const devkitSyntax = tagHighlighter([
  // Keywords and control flow. By default the accent the theme reserves for
  // "this is the structure"; a family with an editor palette of its own sets
  // --syntax-keyword (index.css declares the defaults, families.ts the rest).
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword], class: "text-(--syntax-keyword) font-(--syntax-keyword-weight)" },
  { tag: [t.operator, t.operatorKeyword, t.punctuation], class: "text-(--text-secondary)" },

  // Names. A reference is primary text, a definition may be marked (Xcode
  // colours declarations), a call takes the function colour, a type its own.
  { tag: [t.variableName, t.propertyName], class: "text-(--text-primary)" },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], class: "text-(--syntax-function)" },
  { tag: [t.typeName, t.className, t.namespace], class: "text-(--syntax-type)" },
  { tag: [t.definition(t.variableName), t.definition(t.propertyName)], class: "text-(--syntax-definition)" },

  // Literals.
  { tag: [t.string, t.special(t.string), t.regexp], class: "text-(--syntax-string)" },
  { tag: [t.number, t.bool, t.null, t.atom], class: "text-(--syntax-number)" },

  // Everything that is not the code itself.
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], class: "text-(--syntax-comment) italic" },
  { tag: [t.meta, t.annotation], class: "text-(--syntax-meta)" },
  { tag: t.invalid, class: "text-(--status-danger)" },

  // Markdown and the like, which the notes editor renders.
  { tag: t.heading, class: "text-(--text-primary) font-semibold" },
  { tag: t.strong, class: "font-semibold" },
  { tag: t.emphasis, class: "italic" },
  { tag: t.strikethrough, class: "line-through" },
  { tag: t.link, class: "text-(--hue-blue) underline" },
  { tag: t.url, class: "text-(--hue-blue)" },
]);
