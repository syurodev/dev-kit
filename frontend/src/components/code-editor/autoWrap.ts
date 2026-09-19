import type { EditorView } from "@codemirror/view";

/**
 * An editor showing fewer columns than this wraps; conventional code is written
 * to 80–120, so below it nearly every line would need a horizontal scroll.
 */
export const WRAP_BELOW_COLUMNS = 80;

/**
 * Columns past the threshold an already-wrapped editor must reach before it
 * unwraps. Wrapping changes the content's height, which can add or remove a
 * vertical scrollbar, which changes the width being measured — without a gap, a
 * pane sitting on the threshold would flip on every measurement.
 */
export const UNWRAP_HYSTERESIS_COLUMNS = 4;

/** Whether an editor `columns` wide should wrap, given whether it does now. */
export function shouldWrap(columns: number, wrapped: boolean): boolean {
  // A pane that is hidden or not laid out yet measures zero. That says nothing
  // about its width, so it changes nothing.
  if (!Number.isFinite(columns) || columns <= 0) return wrapped;
  return columns < (wrapped ? WRAP_BELOW_COLUMNS + UNWRAP_HYSTERESIS_COLUMNS : WRAP_BELOW_COLUMNS);
}

/**
 * How many characters fit across the editor's text area.
 *
 * In characters, not pixels, because the code font size is a user setting: the
 * same 600px pane is roomy at 11px and cramped at 16px.
 */
export function visibleColumns(view: EditorView): number {
  const gutters = view.dom.querySelector<HTMLElement>(".cm-gutters");
  const width = view.scrollDOM.clientWidth - (gutters?.offsetWidth ?? 0);
  return Math.floor(width / view.defaultCharacterWidth);
}
