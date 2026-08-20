import type { Extension } from "@codemirror/state";
import { java } from "@codemirror/lang-java";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { markdown } from "@codemirror/lang-markdown";

/**
 * Mirrors internal/codegraph/service.go's languageForPath exactly (same 4
 * extensions, same default-to-none for anything else) — kept in sync by
 * inspection since Go and TS can't share this switch directly.
 *
 * Shared by the file editor and the read-only node snippet panel. The snippet
 * bridge also returns a `language` string, but it is derived from the same
 * file extension on the Go side, so deriving it once here keeps one code path
 * instead of two that can disagree.
 */
export function languageExtensionForPath(path: string): Extension[] {
  const ext = path.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "java":
      return [java()];
    case "json":
      return [json()];
    case "yaml":
    case "yml":
      return [yaml()];
    case "md":
      return [markdown()];
    default:
      return [];
  }
}
