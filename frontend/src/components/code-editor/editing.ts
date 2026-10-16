import { EditorSelection, EditorState, Prec, countColumn, type Extension, type StateCommand } from "@codemirror/state";
import { keymap, type EditorView } from "@codemirror/view";
import { indentUnit, getIndentUnit } from "@codemirror/language";
import { indentLess, indentMore } from "@codemirror/commands";
import { acceptCompletion } from "@codemirror/autocomplete";

export type IndentStyle = "spaces" | "tabs";
export const INDENT_SIZES = [2, 4, 8] as const;
export type IndentSize = (typeof INDENT_SIZES)[number];
export function indentSize(value: unknown): IndentSize {
  return INDENT_SIZES.includes(value as IndentSize) ? value as IndentSize : 2;
}

/** A caret inserts to the next stop; a selection indents whole lines. */
export const insertIndent: StateCommand = ({ state, dispatch }) => {
  if (state.readOnly) return false;
  if (state.selection.ranges.some((range) => !range.empty)) return indentMore({ state, dispatch });
  const size = getIndentUnit(state), tabs = state.facet(indentUnit) === "\t";
  dispatch(state.update(state.changeByRange((range) => {
    const line = state.doc.lineAt(range.head);
    const column = countColumn(line.text.slice(0, range.head - line.from), state.tabSize);
    const insert = tabs ? "\t" : " ".repeat(size - column % size);
    return { changes: { from: range.head, insert }, range: EditorSelection.cursor(range.head + insert.length) };
  }), { scrollIntoView: true, userEvent: "input.indent" }));
  return true;
};

export const tabIndent = (view: EditorView) => !view.state.readOnly && (acceptCompletion(view) || insertIndent(view));

export function editingExtensions(size: IndentSize, style: IndentStyle): Extension {
  return [
    Prec.highest(indentUnit.of(style === "tabs" ? "\t" : " ".repeat(size))),
    EditorState.tabSize.of(size),
    Prec.high(keymap.of([
      { key: "Tab", run: tabIndent },
      { key: "Shift-Tab", run: indentLess },
    ])),
  ];
}
