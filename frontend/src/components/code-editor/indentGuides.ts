import { getIndentUnit, indentUnit } from "@codemirror/language";
import { RangeSetBuilder } from "@codemirror/state";
import { Decoration, ViewPlugin, type DecorationSet, type EditorView, type ViewUpdate } from "@codemirror/view";
import { leadingIndentWidth } from "./wrapIndent";

function guides(view: EditorView): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>();
  let last = -1;
  for (const range of view.visibleRanges) {
    for (let pos = range.from; pos <= range.to;) {
      const line = view.state.doc.lineAt(pos);
      if (line.from > last) {
        const width = leadingIndentWidth(line.text, view.state.tabSize);
        if (width > 0) builder.add(line.from, line.from, Decoration.line({
          attributes: { class: "cm-indent-guides", style: `--cm-indent-width:${width}ch;--cm-indent-step:${getIndentUnit(view.state)}ch` },
        }));
        last = line.from;
      }
      pos = line.to + 1;
    }
  }
  return builder.finish();
}

/** Paint only the visible indentation; never insert characters into the file. */
export const indentGuides = ViewPlugin.fromClass(class {
  decorations: DecorationSet;
  constructor(view: EditorView) { this.decorations = guides(view); }
  update(update: ViewUpdate) {
    if (update.docChanged || update.viewportChanged || update.startState.tabSize !== update.state.tabSize || update.startState.facet(indentUnit) !== update.state.facet(indentUnit)) this.decorations = guides(update.view);
  }
}, { decorations: (plugin) => plugin.decorations });
