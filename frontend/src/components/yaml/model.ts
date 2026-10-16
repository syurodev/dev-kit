import { isMap, isSeq, isScalar, parseAllDocuments, parseDocument, type Document, type Node, type Scalar } from "yaml";
import type { Analysis, Edit, SqlBlock, YamlPath } from "./types";
import { YAML_LIMIT } from "./types";

export type Entry = { path: YamlPath; node: Node; key?: Scalar; document: number; indent: number };
export type Parsed = { text: string; docs: Document[]; entries: Entry[] };
export function parseYaml(text: string): Parsed {
  if (text.length > YAML_LIMIT) throw new Error("YAML assistance supports files up to 512 KiB.");
  const docs = parseAllDocuments(text, { keepSourceTokens: true, uniqueKeys: true, prettyErrors: false, merge: true });
  if (!docs.length) docs.push(parseDocument(text, { keepSourceTokens: true, uniqueKeys: true, prettyErrors: false, merge: true }));
  const entries: Entry[] = [];
  const walk = (node: unknown, path: YamlPath, document: number, key?: Scalar) => {
    if (!isMap(node) && !isSeq(node) && !isScalar(node)) return;
    const at = key?.range?.[0] ?? node.range?.[0] ?? 0;
    const start = text.lastIndexOf("\n", at - 1) + 1;
    const prefix = text.slice(start, at);
    const indent = prefix.match(/^ */)?.[0].length ?? 0;
    entries.push({ path, node, key, document, indent: key ? prefix.length : indent });
    if (isMap(node)) for (const pair of node.items) {
      if (!isScalar(pair.key)) continue;
      if (pair.value == null) {
        const empty = new (pair.key.constructor as typeof Scalar)(null);
        empty.range = [pair.key.range?.[1] ?? at, pair.key.range?.[2] ?? at, pair.key.range?.[2] ?? at];
        walk(empty, [...path, String(pair.key.value)], document, pair.key);
      } else walk(pair.value, [...path, String(pair.key.value)], document, pair.key);
    }
    if (isSeq(node)) node.items.forEach((item, index) => walk(item, [...path, index], document));
  };
  docs.forEach((doc, index) => walk(doc.contents, [], index));
  return { text, docs, entries };
}
export function documentAt(parsed: Parsed, pos: number): number {
  let index = 0;
  parsed.docs.forEach((doc, i) => { if ((doc.range?.[0] ?? 0) <= pos) index = i; });
  return index;
}
export function entryAt(parsed: Parsed, pos: number): Entry | undefined {
  const doc = documentAt(parsed, pos);
  const contains = (range: readonly number[] | null | undefined, key = false) => !!range && range[0] <= pos &&
    (pos < range[key ? 1 : 2] || range[0] === pos || pos === parsed.text.length && pos === range[2]);
  return parsed.entries.filter((entry) => entry.document === doc && (contains(entry.node.range) || contains(entry.key?.range, true)))
    .sort((a, b) => Number(contains(b.key?.range, true)) - Number(contains(a.key?.range, true)) || b.path.length - a.path.length)[0];
}
export function nodeAtPath(parsed: Parsed, document: number, path: YamlPath): Entry | undefined {
  return parsed.entries.find((entry) => entry.document === document && JSON.stringify(entry.path) === JSON.stringify(path));
}
export function keyPath(parsed: Parsed, pos: number): YamlPath {
  return entryAt(parsed, pos)?.path ?? [];
}
export function sqlBlock(parsed: Parsed, pos: number): SqlBlock | null {
  const entry = entryAt(parsed, pos);
  if (!entry || !isScalar(entry.node)) return null;
  const node = entry.node;
  const key = String(entry.path[entry.path.length - 1] ?? "").toLowerCase();
  if (key !== "sql" && !key.endsWith(".sql")) return null;
  if (node.type !== "BLOCK_LITERAL" && node.type !== "BLOCK_FOLDED") return null;
  if (!node.range || typeof node.value !== "string") return null;
  return { document: entry.document, path: entry.path, from: node.range[0], to: node.range[2], raw: parsed.text.slice(node.range[0], node.range[2]), value: node.value, label: pathLabel(entry.path) };
}
export function pathLabel(path: YamlPath): string {
  return path.map((part) => typeof part === "number" ? `[${part}]` : part).join(" › ") || "Document";
}
export function structure(parsed: Parsed, pos: number): Pick<Analysis, "path" | "active" | "sql"> {
  const entry = entryAt(parsed, pos);
  const parent = entry && nodeAtPath(parsed, entry.document, entry.path.slice(0, -1));
  const range = (parent?.node ?? entry?.node)?.range;
  return { path: entry?.path ?? [], active: range ? { from: range[0], to: range[1] } : null, sql: sqlBlock(parsed, pos) };
}

function valid(parsed: Parsed) {
  const error = parsed.docs.flatMap((doc) => doc.errors)[0];
  if (error) throw new Error(`Fix YAML syntax before formatting: ${error.message}`);
}
function sameValues(before: Parsed, after: Parsed) {
  if (before.docs.length !== after.docs.length || after.docs.some((doc) => doc.errors.length)) return false;
  return before.docs.every((doc, i) => JSON.stringify(doc.toJS({ maxAliasCount: 100 })) === JSON.stringify(after.docs[i].toJS({ maxAliasCount: 100 })));
}
export function formatYaml(parsed: Parsed, selection?: { from: number; to: number }): Edit {
  valid(parsed);
  if (!parsed.text.trim()) return { from: 0, to: parsed.text.length, insert: parsed.text };
  let from = 0, to = parsed.text.length, text = parsed.text, pad = "";
  if (selection && selection.from !== selection.to) {
    from = parsed.text.lastIndexOf("\n", selection.from - 1) + 1;
    to = selection.to > 0 && parsed.text[selection.to - 1] === "\n" ? selection.to : parsed.text.indexOf("\n", selection.to);
    if (to < 0) to = parsed.text.length;
    for (const entry of parsed.entries) {
      if (!isScalar(entry.node) || !entry.node.range) continue;
      const [start, end] = entry.node.range;
      if ((start < from && end > from) || (start < to && end > to)) throw new Error("Select complete YAML fields, outside multiline strings, to format a selection.");
    }
    text = parsed.text.slice(from, to);
    const indents = text.split(/\r?\n/).filter((line) => line.trim()).map((line) => line.match(/^ */)![0].length);
    pad = " ".repeat(Math.min(...indents, 1000));
    text = text.split(/\r?\n/).map((line) => line.startsWith(pad) ? line.slice(pad.length) : line).join("\n");
  }
  const source = parseYaml(text);
  valid(source);
  let insert = source.docs.map((doc) => doc.toString({ indent: 2, lineWidth: 0, indentSeq: true })).join("");
  if (!sameValues(source, parseYaml(insert))) throw new Error("Formatting would change YAML values; the file was left unchanged.");
  if (!text.endsWith("\n")) insert = insert.replace(/\n$/, "");
  if (pad) insert = insert.split("\n").map((line) => line ? pad + line : line).join("\n");
  if (parsed.text.includes("\r\n")) insert = insert.replace(/\r?\n/g, "\r\n");
  const result = parsed.text.slice(0, from) + insert + parsed.text.slice(to);
  if (!sameValues(parsed, parseYaml(result))) throw new Error("Select a complete YAML section before formatting; the file was left unchanged.");
  return { from, to, insert };
}

export function applySql(parsed: Parsed, block: SqlBlock, value: string): Edit {
  valid(parsed);
  const entry = nodeAtPath(parsed, block.document, block.path);
  const node = entry?.node;
  if (!isScalar(node) || !node.range || parsed.text.slice(node.range[0], node.range[2]) !== block.raw) {
    throw new Error("This YAML block changed while SQL was open. Copy your draft and reopen the block before applying it.");
  }
  const token = node.srcToken;
  if (token?.type !== "block-scalar") throw new Error("The SQL block is no longer a multiline YAML string.");
  const temp = new (node.constructor as typeof Scalar)(value);
  // A literal scalar preserves SQL newlines even if the original used `>`.
  temp.type = "BLOCK_LITERAL";
  temp.comment = node.comment;
  temp.anchor = node.anchor;
  temp.tag = node.tag;
  const clone = parsed.docs[block.document].clone();
  clone.setIn(block.path, temp);
  const printed = clone.toString({ indent: 2, lineWidth: 0 });
  const replacement = nodeAtPath(parseYaml(printed), 0, block.path)?.node;
  if (!isScalar(replacement) || !replacement.range) throw new Error("Could not rebuild the SQL block.");
  let insert = value === "" && !node.anchor && !node.tag
    ? `|-${node.comment ? ` #${node.comment}` : ""}\n`
    : printed.slice(replacement.range[0], replacement.range[2]);
  const originalIndent = token.indent;
  const replacementIndent = replacement.srcToken?.type === "block-scalar" ? replacement.srcToken.indent : originalIndent;
  const lines = insert.split("\n");
  insert = lines.map((line, i) => i === 0 || !line ? line : " ".repeat(originalIndent) + line.slice(replacementIndent)).join("\n");
  if (parsed.text.includes("\r\n")) insert = insert.replace(/\r?\n/g, "\r\n");
  const edit = { from: node.range[0], to: node.range[2], insert };
  const updated = parseYaml(parsed.text.slice(0, edit.from) + edit.insert + parsed.text.slice(edit.to));
  valid(updated);
  const updatedNode = nodeAtPath(updated, block.document, block.path)?.node;
  if (!isScalar(updatedNode) || updatedNode.value !== value) throw new Error("The SQL edit could not preserve its exact text; YAML was left unchanged.");
  return edit;
}
