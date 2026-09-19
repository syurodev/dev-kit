// frontend/src/components/code-editor/CodeEditor.tsx
import { useEffect, useRef, useState } from "react";
import { EditorView, basicSetup } from "codemirror";
import { EditorState, Compartment, type Extension } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import { shouldWrap, visibleColumns } from "./autoWrap";
import { devkitHighlight } from "./devkitHighlight";
import { wrapBreaks } from "./wrapBreaks";
import { wrapIndent } from "./wrapIndent";

// Settings > Appearance drives these two custom properties (stores/
// appearance.ts writes them on <html>). CodeMirror renders in the DOM, where
// var() resolves live — so font changes apply to every open editor without a
// remount or a Compartment.
const fontTheme = EditorView.theme({
  ".cm-scroller": {
    fontFamily: "var(--code-font-family, var(--font-mono))",
    fontSize: "var(--code-font-size, var(--type-compact-size))",
  },
});

// Everything wrapping needs, as one unit: wrapped rows without the hanging
// indent read as new lines, and without the break opportunities they cut
// identifiers in half.
const wrapExtensions: Extension[] = [EditorView.lineWrapping, wrapIndent, wrapBreaks];

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
   *
   * "auto" is for an editor whose width is not its caller's to know — a pane
   * the user can split or drag to any size. It scrolls while there is room for
   * a conventional line and wraps once there is not (see autoWrap.ts).
   *
   * Whatever this says, ⌥Z flips it for that editor until it unmounts.
   */
  wrap?: boolean | "auto";
  revealLine?: number | null;
};

// Headless CodeMirror 6 wrapper — no language or schema knowledge of its
// own, per base-doc code-editor.mdx. Every prop that can change after mount
// is applied through a Compartment (or, for onChange, a ref) instead of
// remounting the EditorView, which would lose undo history and cursor
// position — the doc's explicit requirement, and CodeMirror's own
// documented pattern for dynamic reconfiguration, not a DevKit invention.
export function CodeEditor({ value, onChange, extensions, readOnly, wrap, revealLine }: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const languageCompartment = useRef(new Compartment()).current;
  const readOnlyCompartment = useRef(new Compartment()).current;
  const wrapCompartment = useRef(new Compartment()).current;

  // onChange is very likely a new function identity on every parent render
  // in React — a ref sidesteps needing another Compartment (or a stale
  // closure) just to keep the update listener current.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  // What "auto" currently measures, and what the user last asked for with ⌥Z.
  // The user's word outranks both the prop and the measurement: someone who
  // turned wrapping off in a narrow pane does not want it back on the next
  // resize.
  const [narrow, setNarrow] = useState(false);
  const [wrapOverride, setWrapOverride] = useState<boolean | null>(null);
  const wrapped = wrapOverride ?? (wrap === "auto" ? narrow : !!wrap);
  const wrappedRef = useRef(wrapped);
  wrappedRef.current = wrapped;
  const autoWrapRef = useRef(wrap === "auto");
  autoWrapRef.current = wrap === "auto";

  const measureWidth = useRef(() => {
    const view = viewRef.current;
    if (!view || !autoWrapRef.current) return;
    const columns = visibleColumns(view);
    setNarrow((current) => shouldWrap(columns, current));
  }).current;

  useEffect(() => {
    if (!containerRef.current) return;
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          // Ahead of basicSetup so no default binding can claim the key first.
          keymap.of([
            {
              key: "Alt-z",
              run: () => {
                setWrapOverride(!wrappedRef.current);
                return true;
              },
            },
          ]),
          basicSetup,
          fontTheme,
          languageCompartment.of(extensions ?? []),
          readOnlyCompartment.of(EditorState.readOnly.of(!!readOnly)),
          // Not in a Compartment, and not keyed on the resolved theme: the
          // highlight style is written in CSS custom properties, so it
          // re-resolves itself when the palette changes. Reconfiguring it on a
          // theme switch would be work with no output.
          devkitHighlight,
          wrapCompartment.of(wrapped ? wrapExtensions : []),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onChangeRef.current(update.state.doc.toString());
            } else if (update.geometryChanged) {
              // The container did not resize, but what fits in it may have: the
              // code font size is a live CSS variable (see fontTheme above).
              measureWidth();
            }
          }),
        ],
      }),
      parent: containerRef.current,
    });
    viewRef.current = view;
    // Fires once on observe, which is also the first measurement.
    const resizeObserver = new ResizeObserver(measureWidth);
    resizeObserver.observe(containerRef.current);
    return () => {
      resizeObserver.disconnect();
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
    const view = viewRef.current;
    if (!view || !revealLine || revealLine < 1) return;
    const line = view.state.doc.line(Math.min(revealLine, view.state.doc.lines));
    view.dispatch({ effects: EditorView.scrollIntoView(line.from, { y: "start" }) });
  }, [revealLine]);

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
      effects: wrapCompartment.reconfigure(wrapped ? wrapExtensions : []),
    });
  }, [wrapped, wrapCompartment]);

  // A caller switching to "auto" after mount has no measurement yet.
  useEffect(() => {
    if (wrap === "auto") measureWidth();
  }, [wrap, measureWidth]);

  // cm-surfaceless is styled in index.css, not through EditorView.theme: a
  // theme extension ties oneDark on specificity (.cm-editor.<theme-class>) and
  // then depends on stylesheet order to win, which it does not reliably. App
  // CSS is the supported way — style-mod deliberately injects CodeMirror's
  // styles as head's first child so later stylesheets can override them.
  return <div ref={containerRef} className="cm-surfaceless h-full w-full overflow-auto" />;
}
