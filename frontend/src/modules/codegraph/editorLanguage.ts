import type { Extension } from "@codemirror/state";
import { StreamLanguage } from "@codemirror/language";
import { java } from "@codemirror/lang-java";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { markdown } from "@codemirror/lang-markdown";
import { lua } from "@codemirror/legacy-modes/mode/lua";

/**
 * Mirrors internal/codegraph/files.go's languageForPath exactly (same
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
    case "lua":
      // No @codemirror/lang-lua exists; the legacy stream mode is what
      // CodeMirror 6 ships for it, and it is what the Agent Hub's transcript
      // already uses for the languages in the same position.
      return [StreamLanguage.define(lua)];
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
