import { isMap, isScalar } from "yaml";
import { applySql, documentAt, entryAt, formatYaml, nodeAtPath, parseYaml, structure, type Parsed } from "./model";
import { expand, schemaFor, schemasAt, validateSchema } from "./schema";
import type { Analysis, Problem, Request, Response, Suggestions, Suggestion, YamlPath } from "./types";

const cache = new Map<string, Parsed>();
function parsed(text: string): Parsed {
  let result = cache.get(text);
  if (!result) {
    result = parseYaml(text);
    cache.set(text, result);
    if (cache.size > 4) cache.delete(cache.keys().next().value!);
  }
  return result;
}
function dataOf(doc: Parsed["docs"][number]): any { return doc.toJS({ maxAliasCount: 100 }); }
function problemRange(model: Parsed, document: number, path: YamlPath, key = false) {
  let entry = nodeAtPath(model, document, path);
  while (!entry && path.length) entry = nodeAtPath(model, document, path = path.slice(0, -1));
  const range = (key ? entry?.key?.range : undefined) ?? entry?.node.range;
  return { from: range?.[0] ?? model.docs[document].range?.[0] ?? 0, to: range?.[1] ?? range?.[0] ?? 0 };
}
async function analyze(model: Parsed, request: Extract<Request, { type: "analyze" }>): Promise<Analysis> {
  const problems: Problem[] = [], labels = new Set<string>(), notices = new Set<string>();
  for (let index = 0; index < model.docs.length; index++) {
    const doc = model.docs[index];
    for (const error of [...doc.errors, ...doc.warnings]) problems.push({ from: error.pos[0], to: error.pos[1], severity: doc.errors.includes(error as any) ? "error" : "warning", message: error.message, source: "YAML" });
    if (doc.errors.length) continue;
    let data;
    try { data = dataOf(doc); } catch (error) { problems.push({ from: doc.range?.[0] ?? 0, to: doc.range?.[0] ?? 0, severity: "error", message: String(error), source: "YAML" }); continue; }
    const match = await schemaFor(request.file, data, request.schema);
    if (!match) continue;
    labels.add(match.label);
    if (match.notice) notices.add(match.notice);
    const errors = validateSchema(match, data);
    const seen = new Set<string>();
    // oneOf schemas may report failures from many alternatives. Report the
    // closest errors and cap repeated messages at the same source location.
    for (const error of errors.sort((a, b) => b.instancePath.length - a.instancePath.length)) {
      const path = error.instancePath.split("/").slice(1).map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
      let current: any = data;
      const typedPath = path.map((part) => { const key = Array.isArray(current) ? Number(part) : part; current = current?.[key]; return key; });
      if (error.keyword === "additionalProperties") typedPath.push(error.params.additionalProperty);
      if (error.keyword === "unevaluatedProperties") typedPath.push(error.params.unevaluatedProperty);
      const range = problemRange(model, index, typedPath, true);
      const message = error.keyword === "required" ? `Missing required key: ${error.params.missingProperty}` : ["additionalProperties", "unevaluatedProperties"].includes(error.keyword) ? `Unknown key: ${error.params.additionalProperty ?? error.params.unevaluatedProperty}` : `${error.message ?? "Invalid value"}${error.keyword === "enum" ? ` (${error.params.allowedValues.join(", ")})` : ""}`;
      const key = `${range.from}:${message}`;
      if (seen.has(key) || seen.size >= 30) continue;
      seen.add(key);
      problems.push({ ...range, severity: "error", message, source: match.label });
    }
  }
  return { ...structure(model, request.position), problems: problems.slice(0, 60), schema: [...labels].join(" / ") || "Syntax only", notice: [...notices].join(" ") || undefined };
}
function quote(value: unknown): string {
  if (typeof value !== "string") return JSON.stringify(value);
  // JSON strings are valid YAML strings, avoiding YAML booleans or punctuation.
  return /^[a-zA-Z_][\w./-]*$/.test(value) && !/^(?:true|false|null|yes|no|on|off)$/i.test(value) ? value : JSON.stringify(value);
}
async function complete(model: Parsed, request: Extract<Request, { type: "complete" }>): Promise<Suggestions> {
  const { position } = request;
  const indentSize = [2, 4, 8].includes(request.indentSize ?? 2) ? request.indentSize ?? 2 : 2;
  const document = documentAt(model, position), doc = model.docs[document];
  let data;
  try { data = dataOf(doc); } catch { return null; }
  const match = await schemaFor(request.file, data, request.schema);
  if (!match) return null;
  const lineStart = model.text.lastIndexOf("\n", position - 1) + 1;
  const before = model.text.slice(lineStart, position);
  if (/^\s*#/.test(before)) return null;
  const entry = entryAt(model, position);
  if (entry && isScalar(entry.node) && entry.node.type?.startsWith("BLOCK_") && position >= (entry.node.range?.[0] ?? position)) return null;
  const valueMode = entry?.key?.range && entry.key.range[0] >= lineStart && position > entry.key.range[1] && /:/.test(model.text.slice(entry.key.range[1], position));
  const word = before.match(/[\w.$/-]*$/)?.[0] ?? "";
  if (!request.explicit && !word && !valueMode) return null;
  let from = position - word.length, to = position;
  const quoted = valueMode && entry && isScalar(entry.node) && (entry.node.type === "QUOTE_DOUBLE" || entry.node.type === "QUOTE_SINGLE");
  let quotedPrefix = "";
  if (quoted && entry.node.range) {
    from = entry.node.range[0];
    const lineEnd = model.text.indexOf("\n", position);
    to = Math.min(entry.node.range[1], lineEnd < 0 ? model.text.length : lineEnd);
    quotedPrefix = model.text.slice(from + 1, position).replace(/["']$/, "");
  }
  const options = new Map<string, Suggestion>();
  if (valueMode && entry) {
    const schemas = schemasAt(match, entry.path, data);
    const field = entry.path[entry.path.length - 1];
    const hints = match.id === "compose" && field === "restart" ? ["no", "always", "on-failure", "unless-stopped"] : match.id.startsWith("kubernetes:") && field === "imagePullPolicy" ? ["Always", "IfNotPresent", "Never"] : [];
    for (const hint of hints) options.set(hint, { label: hint, insert: quote(hint), kind: "constant" });
    for (const schema of schemas) {
      const values = schema.enum ?? (schema.const !== undefined ? [schema.const] : schema.type === "boolean" ? [true, false] : schema.examples ?? []);
      for (const value of values) options.set(String(value), { label: String(value), insert: quote(value), kind: "constant", info: schema.description });
    }
  } else {
    const indent = (before.match(/^ */)?.[0].length ?? 0) + (/^\s*-\s/.test(before) ? 2 : 0);
    // Recover a partial key on a new line even while the YAML parser is
    // recovering a temporarily incomplete map.
    const parents = model.entries.filter((item) => item.document === document && item.path.length &&
      (item.key?.range?.[0] ?? item.node.range?.[0] ?? Infinity) < lineStart && item.indent < indent &&
      (item.node.range?.[2] ?? 0) >= lineStart - 1);
    const parent = parents.sort((a, b) => b.path.length - a.path.length || (b.node.range?.[0] ?? 0) - (a.node.range?.[0] ?? 0))[0];
    const path = parent?.path ?? (entry?.key ? entry.path.slice(0, -1) : []);
    const schemas = schemasAt(match, path, data);
    const container = nodeAtPath(model, document, path)?.node;
    const existing = isMap(container) ? new Set(container.items.filter((pair) => isScalar(pair.key)).map((pair) => String((pair.key as any).value))) : new Set<string>();
    const suffix = model.text.slice(position, model.text.indexOf("\n", position) < 0 ? model.text.length : model.text.indexOf("\n", position));
    for (const schema of schemas) for (const [key, property] of Object.entries(schema.properties ?? {}) as [string, any][]) {
      if (existing.has(key) && key !== (entry?.key?.value ?? "")) continue;
      const defs = expand(property, match.root, undefined);
      const definition = defs.find((item) => item.type || item.enum) ?? property;
      const rawType = definition.type ?? (definition.properties ? "object" : undefined);
      const type = Array.isArray(rawType) ? (rawType.includes("object") ? "object" : rawType[0]) : rawType;
      let insert = quote(key);
      if (!/^\s*:/.test(suffix)) insert += type === "string" && (key === "sql" || key.endsWith(".sql")) ? `: |\n${" ".repeat(indent + indentSize)}` : type === "object" ? `:\n${" ".repeat(indent + indentSize)}` : type === "array" ? `:\n${" ".repeat(indent + indentSize)}- ` : ": ";
      options.set(key, { label: key, insert, kind: "property", detail: (schema.required ?? []).includes(key) ? "required" : type, info: defs.map((item) => item.description).find(Boolean) });
    }
  }
  return { from, to, options: [...options.values()].filter((option) => !quoted || option.label.startsWith(quotedPrefix)).slice(0, 200), ...(quoted ? { filter: false } : {}) };
}
export async function handleYaml(request: Request): Promise<Response> {
  const model = parsed(request.text);
  if (request.type === "format") return formatYaml(model, request.selection, request.indentSize);
  if (request.type === "applySql") return applySql(model, request.block, request.value);
  if (request.type === "complete") return complete(model, request);
  return analyze(model, request);
}
