import { create } from "zustand";

/**
 * Where the caret is in the file in front, as a line of the SAVED file.
 *
 * The breadcrumb names the symbol the caret is in and the Outline highlights it;
 * both read the index, which describes the saved file, so the line is mapped back
 * through the unsaved edits (codeIntel's baseline) before it is stored. Written
 * only by the front file document, and only when the line changes — typing on a
 * line does not wake anything that is looking at it.
 */
export const useCursorStore = create<{ key: string; line: number }>(() => ({ key: "", line: 0 }));

export function setCursorLine(key: string, line: number) {
  const s = useCursorStore.getState();
  if (s.key === key && s.line === line) return;
  useCursorStore.setState({ key, line });
}

/** The caret's line, as a saved-file line, for the document with this key; 0 for any other. */
export function useCursor(key: string): number {
  return useCursorStore((s) => (s.key === key ? s.line : 0));
}
