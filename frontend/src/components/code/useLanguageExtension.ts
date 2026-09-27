import { useEffect, useState } from "react";
import type { Extension } from "@codemirror/state";
import { loadedLanguage, loadLanguage, normalizeLanguage } from "./languages";

const NONE: Extension[] = [];

/**
 * A language for a CodeEditor, from the shared registry: pass the result in
 * its `extensions`. CodeEditor itself stays headless — it knows no language;
 * this is how a caller names one.
 *
 * `[]` until the grammar has loaded (the text shows plain for that frame),
 * then the language's support. The array is stable per language, so a caller
 * that memoises its extensions on it reconfigures the editor once, on load.
 * Keep any stateful extension (codeIntel) in its own memo rather than
 * rebuilding it alongside this one.
 */
export function useLanguageExtension(language?: string | null): Extension[] {
  const id = normalizeLanguage(language);
  const [support, setSupport] = useState(() => (id ? loadedLanguage(id) : undefined));
  const [supportId, setSupportId] = useState(id);

  // Language changed under the same editor: start from what is cached for it.
  if (supportId !== id) {
    setSupportId(id);
    setSupport(id ? loadedLanguage(id) : undefined);
  }

  useEffect(() => {
    if (!id || loadedLanguage(id)) return;
    let cancelled = false;
    loadLanguage(id)
      .then((loaded) => {
        if (!cancelled) setSupport(loaded);
      })
      .catch(() => {
        // Unknown or failed to fetch: the editor works as plain text.
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  return support ? extensionsFor(support) : NONE;
}

const wrapped = new WeakMap<object, Extension[]>();

function extensionsFor(support: Extension & object): Extension[] {
  let extensions = wrapped.get(support);
  if (!extensions) {
    extensions = [support];
    wrapped.set(support, extensions);
  }
  return extensions;
}
