import { EditorView } from "@codemirror/view";

/**
 * How the SQL completion popup looks — the console's and the table filter's.
 *
 * An EditorView.theme rather than a rule in index.css. The popup is portalled
 * to document.body, so no class on the editor can reach it from a stylesheet,
 * and CodeMirror's own rules are `.ͼ1 .cm-tooltip.cm-tooltip-autocomplete > ul`
 * — three classes, injected by style-mod when the editor mounts, i.e. after
 * app CSS. Hand-written CSS either ties that and loses on source order (which
 * left the list in the browser's `monospace` while the padding beside it
 * applied) or wins only by stacking class names until it outnumbers whatever
 * CodeMirror happens to use this version. A theme is the supported way in: same
 * mechanism, registered later, and it follows the editor to wherever the
 * tooltip is rendered.
 */

/** A Lucide glyph as a mask, so it takes the row's own colour. */
function icon(svg: string) {
  const url = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='black' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E${svg}%3C/svg%3E")`;
  return { WebkitMaskImage: url, maskImage: url };
}

const ICONS = {
  // columns-3: a column of the table.
  property: icon("%3Crect width='18' height='18' x='3' y='3' rx='2'/%3E%3Cpath d='M9 3v18'/%3E%3Cpath d='M15 3v18'/%3E"),
  // table: a table, and the dialect's data types, which lang-sql gives the
  // same completion type.
  type: icon("%3Cpath d='M12 3v18'/%3E%3Crect width='18' height='18' x='3' y='3' rx='2'/%3E%3Cpath d='M3 9h18'/%3E%3Cpath d='M3 15h18'/%3E"),
  // database: a schema.
  class: icon("%3Cellipse cx='12' cy='5' rx='9' ry='3'/%3E%3Cpath d='M3 5V19A9 3 0 0 0 21 19V5'/%3E%3Cpath d='M3 12A9 3 0 0 0 21 12'/%3E"),
  // square-code: a word of the dialect, not of the database.
  keyword: icon("%3Cpath d='M10 9.5 8 12l2 2.5'/%3E%3Cpath d='m14 9.5 2 2.5-2 2.5'/%3E%3Crect width='18' height='18' x='3' y='3' rx='2'/%3E"),
};

export const sqlCompletionTheme = EditorView.theme({
  ".cm-tooltip.cm-tooltip-autocomplete": {
    border: "1px solid var(--border-subtle)",
    borderRadius: "var(--radius-control)",
    backgroundColor: "var(--surface-raised)",
    boxShadow: "var(--shadow-popover, 0 8px 24px rgb(0 0 0 / 0.32))",
    overflow: "hidden",
    padding: "var(--space-micro)",
  },
  // The editor's own font and size. Left to inherit, the list took the app's
  // UI font while the text it completes renders in the code face.
  ".cm-tooltip.cm-tooltip-autocomplete > ul": {
    fontFamily: "var(--code-font-family, var(--font-mono))",
    fontSize: "var(--code-font-size, var(--type-compact-size))",
    lineHeight: "var(--type-completion-leading)",
    maxHeight: "16rem",
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li": {
    display: "flex",
    alignItems: "center",
    gap: "var(--space-inline)",
    padding: "var(--space-micro) var(--space-inline)",
    borderRadius: "var(--radius-sm)",
    color: "var(--text-secondary)",
    cursor: "pointer",
  },
  // Hover and keyboard selection have to be told apart: whichever row is
  // selected is the one Enter takes, and if hovering looked identical you
  // could not see that the pointer had not moved it.
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li:hover": {
    backgroundColor: "var(--state-soft)",
    color: "var(--text-primary)",
  },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--state-soft)",
    color: "var(--text-primary)",
    boxShadow: "inset 2px 0 0 var(--status-info)",
  },
  // CodeMirror underlines the matched run. An underline inside a monospace
  // identifier reads as part of the name, so the match is weight and colour.
  ".cm-completionMatchedText": {
    textDecoration: "none",
    fontWeight: "600",
    color: "var(--status-info)",
  },
  ".cm-completionDetail": {
    marginLeft: "auto",
    paddingLeft: "var(--space-control)",
    fontStyle: "normal",
    color: "var(--text-tertiary)",
  },
  ".cm-completionIcon": {
    width: "14px",
    height: "14px",
    padding: "0",
    margin: "0",
    flex: "none",
    opacity: "1",
    backgroundColor: "currentColor",
    color: "var(--text-tertiary)",
    WebkitMaskSize: "14px 14px",
    maskSize: "14px 14px",
    WebkitMaskPosition: "center",
    maskPosition: "center",
    WebkitMaskRepeat: "no-repeat",
    maskRepeat: "no-repeat",
  },
  // CodeMirror's own icons are literal text — an emoji key for a keyword, a
  // bare "t" for a type. The mask is the icon; the glyph never renders.
  ".cm-completionIcon::after": { content: '""' },
  ".cm-completionIcon-property": ICONS.property,
  ".cm-completionIcon-type": ICONS.type,
  ".cm-completionIcon-class": ICONS.class,
  ".cm-completionIcon-keyword": ICONS.keyword,
});
