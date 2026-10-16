import Ajv, { type ErrorObject, type ValidateFunction } from "ajv";
import Ajv2020 from "ajv/dist/2020";
import type { SchemaChoice, YamlPath } from "./types";

export type Schema = Record<string, any>;
export type SchemaMatch = { root: Schema; schema: Schema; id: string; label: string; notice?: string };
const bundles = new Map<string, Promise<Schema>>();
const validators = new Map<string, ValidateFunction>();
const draft7 = new Ajv({ strict: false, allErrors: true, validateFormats: false, validateSchema: false });
const draft2020 = new Ajv2020({ strict: false, allErrors: true, validateFormats: false });
function bundle(id: string): Promise<Schema> {
  let promise = bundles.get(id);
  if (!promise) {
    promise = (id === "compose" ? import("./schemas/compose.json") : id === "liquibase" ? import("./schemas/liquibase.json") : import("./schemas/kubernetes.json")).then((m) => {
      const schema: Schema = structuredClone(m.default);
      if (id === "liquibase") {
        // Liquibase's YAML parser accepts the map shorthand used by hand-
        // authored changelogs, as well as the serializer's sequence form.
        const pre = schema.definitions.preConditions;
        const properties = Object.assign({}, ...pre.items.anyOf.map((item: Schema) => item.properties ?? {}));
        schema.definitions.preConditions = { ...pre, type: ["array", "object"], properties };
        if (properties.sqlCheck?.properties?.expectedResult) properties.sqlCheck.properties.expectedResult.type = ["string", "number"];
      }
      return schema;
    });
    bundles.set(id, promise);
  }
  return promise;
}
export async function schemaFor(file: string, data: any, choice: SchemaChoice): Promise<SchemaMatch | null> {
  let id: string = choice;
  if (id === "auto") {
    const name = file.replace(/\\/g, "/").split("/").pop() ?? "";
    if (data && typeof data === "object" && "databaseChangeLog" in data || /(?:^|[\/.-])(?:liquibase|changelog)(?:[\/.-]|$)/i.test(file)) id = "liquibase";
    else if (data && (typeof data.apiVersion === "string" || typeof data.kind === "string")) id = "kubernetes";
    else if (/^(?:docker-)?compose(?:[.-].*)?\.ya?ml$/i.test(name) || /^(?:docker-)?compose\.ya?ml$/i.test(name)) id = "compose";
    else return null;
  }
  if (id === "none") return null;
  const root = await bundle(id);
  if (id !== "kubernetes") return { root, schema: root, id, label: id === "compose" ? "Docker Compose" : "Liquibase · SchemaStore" };
  const kind = root.kinds[`${data?.apiVersion}|${data?.kind}`];
  if (kind) return { root, schema: { $ref: `#/definitions/${kind}` }, id: `kubernetes:${kind}`, label: `Kubernetes 1.35 · ${data.kind}` };
  const versions = [...new Set(Object.keys(root.kinds).map((key) => key.split("|")[0]))];
  const kinds = [...new Set(Object.keys(root.kinds).filter((key) => !data?.apiVersion || key.startsWith(`${data.apiVersion}|`)).map((key) => key.split("|")[1]))];
  return {
    root,
    schema: { type: "object", required: ["apiVersion", "kind"], properties: {
      apiVersion: { type: "string", examples: versions },
      kind: { type: "string", examples: kinds },
      metadata: { $ref: "#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta" },
    } }, id: "kubernetes:generic", label: "Kubernetes 1.35",
    notice: data?.apiVersion && data?.kind ? `No bundled schema for ${data.apiVersion} / ${data.kind}. Syntax checks remain active.` : undefined,
  };
}
export function validateSchema(match: SchemaMatch, data: unknown): ErrorObject[] {
  let validate = validators.get(match.id);
  if (!validate) {
    const schema = match.id.startsWith("kubernetes:") ? { ...match.schema, definitions: match.root.definitions } : match.root;
    validate = (match.id === "compose" ? draft2020 : draft7).compile(schema);
    validators.set(match.id, validate);
  }
  validate(data);
  return (validate.errors ?? []).filter((error) => !["oneOf", "anyOf", "if"].includes(error.keyword));
}
function ref(root: Schema, value: string): Schema | null {
  if (!value.startsWith("#/")) return null;
  let node: any = root;
  for (const key of value.slice(2).split("/").map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"))) node = node?.[key];
  return node && typeof node === "object" ? node : null;
}
/** Resolve local refs and alternatives, preferring branches matching sibling discriminators. */
export function expand(schema: Schema, root: Schema, data: any, seen = new Set<string>(), depth = 0): Schema[] {
  if (depth > 24 || !schema || typeof schema !== "object") return [];
  const out: Schema[] = [schema];
  if (schema.$ref && !seen.has(schema.$ref)) {
    const target = ref(root, schema.$ref);
    if (target) out.push(...expand(target, root, data, new Set([...seen, schema.$ref]), depth + 1));
  }
  for (const child of schema.allOf ?? []) out.push(...expand(child, root, data, seen, depth + 1));
  for (const alternatives of [schema.anyOf, schema.oneOf]) {
    if (!alternatives) continue;
    const matching = alternatives.filter((item: Schema) => {
      const variants = expand(item, root, data, seen, depth + 1);
      return variants.every((variant) => {
        if (variant.type && data != null) {
          const type = Array.isArray(data) ? "array" : typeof data;
          if (typeof variant.type === "string" && variant.type !== type && !(variant.type === "integer" && type === "number")) return false;
        }
        return Object.entries(variant.properties ?? {}).every(([key, def]: [string, any]) => data?.[key] === undefined ||
          (def.const === undefined || def.const === data[key]) && (!def.enum || def.enum.includes(data[key])));
      });
    });
    for (const child of matching.length ? matching : alternatives) out.push(...expand(child, root, data, seen, depth + 1));
  }
  return out;
}
export function schemasAt(match: SchemaMatch, path: YamlPath, data: any): Schema[] {
  let list = expand(match.schema, match.root, data), value = data;
  for (const part of path) {
    const next: Schema[] = [];
    for (const schema of list) {
      if (typeof part === "number") { if (schema.items && !Array.isArray(schema.items)) next.push(schema.items); }
      else {
        if (schema.properties?.[part]) next.push(schema.properties[part]);
        for (const [pattern, child] of Object.entries(schema.patternProperties ?? {})) {
          if (new RegExp(pattern).test(part)) next.push(child as Schema);
        }
        if (!schema.properties?.[part] && schema.additionalProperties && typeof schema.additionalProperties === "object") next.push(schema.additionalProperties);
      }
    }
    value = value?.[part];
    list = next.flatMap((schema) => expand(schema, match.root, value));
  }
  return list;
}
