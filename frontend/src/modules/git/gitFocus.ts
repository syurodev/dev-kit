import { create } from "zustand";

/**
 * The changed file the person is looking at: the one open in the Changes view
 * and lit in the rail. Both read it and both set it, so choosing a file in the
 * rail opens it in the view and opening one in the view lights it in the rail,
 * without either knowing the other exists.
 *
 * A path, not a row index: the lists are read again after every stage, and a
 * path is still the same file when the file has moved between them.
 */
export const useGitFocus = create<{ path: string }>()(() => ({ path: "" }));

export function focusGitPath(path: string) {
  useGitFocus.setState({ path });
}
