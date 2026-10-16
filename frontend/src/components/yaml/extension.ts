import { EditorState, Prec, StateEffect, StateField, Transaction, type Extension, type Range } from "@codemirror/state";
import { Decoration, EditorView, keymap, type DecorationSet } from "@codemirror/view";
import { indentLess, indentMore, insertNewlineAndIndent, isolateHistory } from "@codemirror/commands";
import { syntaxTree } from "@codemirror/language";
import { acceptCompletion, type CompletionSource } from "@codemirror/autocomplete";
import { requestYaml } from "./client";
import type { Edit, SchemaChoice } from "./types";
import { YAML_LIMIT } from "./types";

export const activeYamlRange = StateEffect.define<{ from: number; to: number } | null>();
const activeBlock = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(value, transaction) {
    value = value.map(transaction.changes);
    for (const effect of transaction.effects) if (effect.is(activeYamlRange)) {
      const range = effect.value;
      if (!range) value = Decoration.none;
      else {
        const marks: Range<Decoration>[] = [];
        for (let pos = transaction.state.doc.lineAt(range.from).from; pos <= range.to && pos <= transaction.state.doc.length;) {
          marks.push(Decoration.line({ class: "cm-yaml-active-block" }).range(pos));
          const line = transaction.state.doc.lineAt(pos);
          pos = line.to + 1;
          if (marks.length >= 300) break;
        }
        value = Decoration.set(marks);
      }
    }
    return value;
  },
  provide: (field) => EditorView.decorations.from(field),
});
export function applyYamlEdit(view: EditorView, edit: Edit) {
  if (view.state.doc.sliceString(edit.from, edit.to) === edit.insert) return;
  view.dispatch({ changes: edit, annotations: [Transaction.userEvent.of("input.format"), isolateHistory.of("full")] });
  view.focus();
}
/** YAML snippets keep spaces as spaces; Tab never inserts YAML-invalid tabs. */
export const yamlEditingKeys: Extension = Prec.highest(keymap.of([
  { key: "Tab", run: (view) => acceptCompletion(view) || indentMore(view) },
  { key: "Shift-Tab", run: indentLess },
  { key: "Enter", run: (view) => acceptCompletion(view) || insertNewlineAndIndent(view) },
]));
export function yamlAssistance(config: { file: string; schema: SchemaChoice }): Extension {
  const source: CompletionSource = async (context) => {
    if (context.state.doc.length > YAML_LIMIT) return null;
    const result = await requestYaml({ type: "complete", text: context.state.doc.toString(), file: config.file, schema: config.schema, position: context.pos, explicit: context.explicit }).catch(() => null);
    if (context.aborted || !result) return null;
    return { from: result.from, to: result.to, filter: result.filter, options: result.options.map((option) => ({ ...option, type: option.kind,
      apply(view: EditorView, _completion: unknown, from: number, to: number) {
        view.dispatch({ changes: { from, to, insert: option.insert }, selection: { anchor: from + option.insert.length }, annotations: Transaction.userEvent.of("input.complete") });
      },
    })) };
  };
  return [activeBlock, yamlEditingKeys,
    EditorView.contentAttributes.of({ "data-yaml-editor": "true" }),
    // Completion sources are a facet; scoped by syntax so SQL owns its region.
    yamlCompletion(source),
  ];
}
function yamlCompletion(source: CompletionSource): Extension {
  return EditorState.languageData.of((state, pos) => {
    let node = syntaxTree(state).resolveInner(pos, -1);
    while (node.parent) { if (node.name === "Script") return []; node = node.parent; }
    return [{ autocomplete: source }];
  });
}
