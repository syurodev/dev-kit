import { RangeSetBuilder } from "@codemirror/state";
import { Decoration, ViewPlugin, type DecorationSet, type EditorView } from "@codemirror/view";

/**
 * Extra characters a wrapped row is indented past its own line's indentation,
 * so a continuation is visibly subordinate rather than merely deeper.
 */
export const HANGING_INDENT = 2;

/**
 * Width, in characters, of a line's leading whitespace.
 *
 * Monospace only — which is the whole point: in a monospace editor a character
 * count IS a pixel offset, expressible as `ch`, so a wrapped row can line up
 * under the code without measuring any DOM.
 */
export function leadingIndentWidth(text: string, tabSize = 4): number {
  let width = 0;
  for (const ch of text) {
    if (ch === " ") width += 1;
    else if (ch === "\t") width += tabSize - (width % tabSize);
    else break;
  }
  return width;
}

function buildIndents(view: EditorView): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>();
  const docLength = view.state.doc.length;
  for (const { from, to } of view.visibleRanges) {
    let pos = from;
    while (pos <= to && pos <= docLength) {
      const line = view.state.doc.lineAt(pos);
      const width = leadingIndentWidth(line.text) + HANGING_INDENT;
      builder.add(
        line.from,
        line.from,
        Decoration.line({ attributes: { style: `--cm-wrap-indent:${width}ch` } }),
      );
      pos = line.to + 1;
    }
  }
  return builder.finish();
}

/**
 * Indents wrapped rows under the line they continue.
 *
 * CodeMirror wraps to column zero, which puts a continuation further left than
 * the statement it belongs to — it reads as a new line rather than the same
 * one. This sets a per-line `--cm-wrap-indent`; index.css turns that into a
 * hanging indent (padding-left plus a matching negative text-indent, so only
 * the wrapped rows move).
 *
 * Only meaningful alongside EditorView.lineWrapping, and only added with it.
 */
export const wrapIndent = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = buildIndents(view);
    }

    update(update: { docChanged: boolean; viewportChanged: boolean; view: EditorView }) {
      if (update.docChanged || update.viewportChanged) {
        this.decorations = buildIndents(update.view);
      }
    }
  },
  { decorations: (v) => v.decorations },
);
