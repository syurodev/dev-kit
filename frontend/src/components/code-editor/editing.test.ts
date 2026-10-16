import { describe, expect, it } from "bun:test";
import { Compartment, EditorSelection, EditorState, type StateCommand } from "@codemirror/state";
import { getIndentUnit, indentUnit } from "@codemirror/language";
import { history, indentLess, insertNewlineAndIndent, undo } from "@codemirror/commands";
import { editingExtensions, indentSize, insertIndent } from "./editing";
import { yamlSupport } from "@/components/code/grammars/yaml";

function target(initial: EditorState) {
  const editor = {
    state: initial,
    dispatch: (transaction: Parameters<Parameters<StateCommand>[0]["dispatch"]>[0]) => { editor.state = transaction.state; },
  };
  return editor;
}

describe("editor indentation", () => {
  it("inserts spaces at the caret to the next stop, without indenting the line", () => {
    const editor = target(EditorState.create({ doc: "ab", selection: { anchor: 2 }, extensions: editingExtensions(4, "spaces") }));
    insertIndent(editor);
    expect(editor.state.doc.toString()).toBe("ab  ");
    expect(editor.state.selection.main.head).toBe(4);
    insertIndent(editor);
    expect(editor.state.doc.toString()).toBe("ab      ");
  });
  it("indents and outdents a selection of whole lines by the chosen width", () => {
    const editor = target(EditorState.create({ doc: "one\ntwo\nthree", selection: { anchor: 0, head: 8 }, extensions: editingExtensions(4, "spaces") }));
    insertIndent(editor);
    expect(editor.state.doc.toString()).toBe("    one\n    two\nthree");
    indentLess(editor);
    expect(editor.state.doc.toString()).toBe("one\ntwo\nthree");
  });
  it("applies Tab at every caret with different columns", () => {
    const editor = target(EditorState.create({ doc: "a\nabc", selection: EditorSelection.create([EditorSelection.cursor(1), EditorSelection.cursor(5)]), extensions: [EditorState.allowMultipleSelections.of(true), editingExtensions(4, "spaces")] }));
    insertIndent(editor);
    expect(editor.state.doc.toString()).toBe("a   \nabc ");
  });
  it("inserts real tabs for Makefile recipes with the configured display width", () => {
    const editor = target(EditorState.create({ doc: "all:\n", selection: { anchor: 5 }, extensions: editingExtensions(4, "tabs") }));
    insertIndent(editor);
    expect(editor.state.doc.toString()).toBe("all:\n\t");
    expect(editor.state.tabSize).toBe(4);
  });
  it("overrides YAML's default indent and uses the width on Enter", () => {
    const editor = target(EditorState.create({ doc: "root:", selection: { anchor: 5 }, extensions: [yamlSupport(), editingExtensions(4, "spaces")] }));
    expect(getIndentUnit(editor.state)).toBe(4);
    insertNewlineAndIndent(editor);
    expect(editor.state.doc.toString()).toBe("root:\n    ");
  });
  it("preserves source, caret and undo when reconfiguring indentation", () => {
    const compartment = new Compartment();
    const editor = target(EditorState.create({ doc: "a", selection: { anchor: 1 }, extensions: [history(), compartment.of(editingExtensions(2, "spaces"))] }));
    insertIndent(editor);
    editor.dispatch(editor.state.update({ effects: compartment.reconfigure(editingExtensions(8, "spaces")) }));
    expect(editor.state.doc.toString()).toBe("a ");
    expect(editor.state.selection.main.head).toBe(2);
    expect(editor.state.facet(indentUnit)).toBe("        ");
    expect(undo(editor)).toBe(true);
    expect(editor.state.doc.toString()).toBe("a");
  });
  it("never modifies a read-only editor", () => {
    const editor = target(EditorState.create({ doc: "data", extensions: [EditorState.readOnly.of(true), editingExtensions(4, "spaces")] }));
    expect(insertIndent(editor)).toBe(false);
    expect(indentLess(editor)).toBe(false);
    expect(editor.state.doc.toString()).toBe("data");
  });
  it("validates stored indentation widths", () => {
    expect(indentSize(4)).toBe(4);
    expect(indentSize(8)).toBe(8);
    for (const value of [0, -1, 1000, "4", null, undefined]) expect(indentSize(value)).toBe(2);
  });
});

it("keeps literal YAML scalar and comment indentation when their text ends with a colon", () => {
  for (const [source, indent] of [["root: |\n    sql:", 4], ["    # comment:", 4], ['root: "url:', 0]] as const) {
    const editor = target(EditorState.create({ doc: source, selection: { anchor: source.length }, extensions: [yamlSupport(), editingExtensions(4, "spaces")] }));
    insertNewlineAndIndent(editor);
    expect(editor.state.doc.toString()).toBe(`${source}\n${" ".repeat(indent)}`);
  }
});
