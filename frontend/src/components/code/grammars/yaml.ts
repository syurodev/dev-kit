import { LanguageSupport, indentUnit, indentService, getIndentUnit, syntaxTree } from "@codemirror/language";
import { yamlLanguage } from "@codemirror/lang-yaml";
import { sql } from "@codemirror/lang-sql";
import { EditorView } from "@codemirror/view";
import { parseMixed } from "@lezer/common";

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
    indentService.of((context, position) => {
      const line = context.state.doc.lineAt(position);
      const before = line.text.slice(0, position - line.from);
      const colon = /:[ \t]*(?:#.*)?$/.exec(before);
      if (!colon || line.text.slice(position - line.from).trim()) return null;
      const node = syntaxTree(context.state).resolveInner(line.from + colon.index + 1, -1);
      // Only an actual mapping separator, never a colon in a string, comment
      // or multiline scalar (where changing indentation changes its value).
      if (node.name !== ":" || node.parent?.name !== "Pair") return null;
      const pair = node.parent;
      if (pair.lastChild?.name !== ":") return null;
      const key = pair.getChild("Key");
      return key ? context.column(key.from) + getIndentUnit(context.state) : null;
    }),
    EditorView.editorAttributes.of({ class: "cm-yaml-source" }),
    EditorView.theme({ ".cm-scroller": { lineHeight: "1.6" } }),
  ]);
}
