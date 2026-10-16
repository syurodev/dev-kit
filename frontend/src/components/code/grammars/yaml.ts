import { LanguageSupport, indentUnit } from "@codemirror/language";
import { yamlLanguage } from "@codemirror/lang-yaml";
import { sql } from "@codemirror/lang-sql";
import { EditorView } from "@codemirror/view";
import { parseMixed } from "@lezer/common";
import { indentGuides } from "@/components/code-editor/indentGuides";

/** Keep YAML's structure and folding, parsing only explicit SQL scalar bodies. */
export function yamlSupport() {
  const sqlSupport = sql();
  const language = yamlLanguage.configure({
    wrap: parseMixed((node, input) => {
      if (node.name !== "BlockLiteralContent") return null;
      const pair = node.node.parent?.parent;
      const key = pair?.getChild("Key");
      if (!key) return null;
      const name = input.read(key.from, key.to).replace(/^(['"])(.*)\1$/, "$2").toLowerCase();
      // `query` or `script` alone could mean GraphQL, shell, or anything else.
      if (name !== "sql" && !name.endsWith(".sql")) return null;
      return { parser: sqlSupport.language.parser };
    }),
  });
  return new LanguageSupport(language, [
    sqlSupport.support,
    indentUnit.of("  "),
    indentGuides,
    EditorView.editorAttributes.of({ class: "cm-yaml-source" }),
    EditorView.theme({ ".cm-scroller": { lineHeight: "1.6" } }),
  ]);
}
