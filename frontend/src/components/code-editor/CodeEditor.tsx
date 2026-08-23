// frontend/src/components/code-editor/CodeEditor.tsx
import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { EditorState, Compartment, type Extension } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { useTheme } from "@/theme/ThemeProvider";

export type CodeEditorProps = {
  value: string;
  onChange: (value: string) => void;
  extensions?: Extension[];
  readOnly?: boolean;
  /**
   * Wrap long lines instead of scrolling horizontally. Off by default, which
   * is what a full-width file editor wants; a narrow preview wants it on,
   * because a horizontal scrollbar under two visible lines is worse than a
   * wrapped line.
   */
  wrap?: boolean;
};

// Headless CodeMirror 6 wrapper — no language or schema knowledge of its
// own, per base-doc code-editor.mdx. Every prop that can change after mount
// is applied through a Compartment (or, for onChange, a ref) instead of
// remounting the EditorView, which would lose undo history and cursor
// position — the doc's explicit requirement, and CodeMirror's own
// documented pattern for dynamic reconfiguration, not a DevKit invention.
export function CodeEditor({ value, onChange, extensions, readOnly, wrap }: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const languageCompartment = useRef(new Compartment()).current;
  const readOnlyCompartment = useRef(new Compartment()).current;
  const themeCompartment = useRef(new Compartment()).current;
  const wrapCompartment = useRef(new Compartment()).current;

  // onChange is very likely a new function identity on every parent render
  // in React — a ref sidesteps needing another Compartment (or a stale
  // closure) just to keep the update listener current.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const { resolvedTheme } = useTheme();

  useEffect(() => {
    if (!containerRef.current) return;
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          languageCompartment.of(extensions ?? []),
          readOnlyCompartment.of(EditorState.readOnly.of(!!readOnly)),
          themeCompartment.of(resolvedTheme === "dark" ? [oneDark] : []),
          wrapCompartment.of(wrap ? EditorView.lineWrapping : []),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onChangeRef.current(update.state.doc.toString());
            }
          }),
        ],
      }),
      parent: containerRef.current,
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // Deliberately empty — see the function's doc comment. Every prop this
    // effect would otherwise need to depend on is instead applied by the
    // effects below, without remounting.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // External value changes (e.g. the consumer opened a different file) —
  // guarded by an equality check so this doesn't fight the update listener
  // above during normal typing (onChange already updated the parent's
  // value to match what's on screen; dispatching again here would be a
  // harmless no-op change, but skipping it avoids the extra work).
  useEffect(() => {
    const view = viewRef.current;
    if (!view || view.state.doc.toString() === value) return;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
  }, [value]);

  useEffect(() => {
    viewRef.current?.dispatch({ effects: languageCompartment.reconfigure(extensions ?? []) });
  }, [extensions, languageCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: readOnlyCompartment.reconfigure(EditorState.readOnly.of(!!readOnly)),
    });
  }, [readOnly, readOnlyCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: themeCompartment.reconfigure(resolvedTheme === "dark" ? [oneDark] : []),
    });
  }, [resolvedTheme, themeCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: wrapCompartment.reconfigure(wrap ? EditorView.lineWrapping : []),
    });
  }, [wrap, wrapCompartment]);

  // cm-surfaceless is styled in index.css, not through EditorView.theme: a
  // theme extension ties oneDark on specificity (.cm-editor.<theme-class>) and
  // then depends on stylesheet order to win, which it does not reliably. App
  // CSS is the supported way — style-mod deliberately injects CodeMirror's
  // styles as head's first child so later stylesheets can override them.
  return <div ref={containerRef} className="cm-surfaceless h-full w-full overflow-auto" />;
}
