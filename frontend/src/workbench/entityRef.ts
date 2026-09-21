/**
 * The string form of an EntityRef (docs/dev-kit/cross-module.mdx § Primitive
 * 1): "devkit://<module>/<kind>/<id>?label=&project=&line=". Small and
 * serializable on purpose — it is what crosses the bridge into Go as an
 * `agent.attach` ref, and what a transcript or a pasted link carries. DevKit
 * never resolves what it points at; the agent reads it fresh through the
 * owning module's own MCP tool.
 */
import type { EntityRef } from "./intents";

export function encodeEntityRef(ref: EntityRef): string {
  const params = new URLSearchParams();
  if (ref.label) params.set("label", ref.label);
  if (ref.project) params.set("project", ref.project);
  if (ref.line !== undefined) params.set("line", String(ref.line));
  const query = params.toString();
  return (
    `devkit://${encodeURIComponent(ref.module)}/${encodeURIComponent(ref.kind)}/${encodeURIComponent(ref.id)}` +
    (query ? `?${query}` : "")
  );
}

export function decodeEntityRefUri(uri: string): EntityRef | null {
  let url: URL;
  try {
    url = new URL(uri);
  } catch {
    return null;
  }
  if (url.protocol !== "devkit:" || !url.hostname) return null;
  const [kind, id] = url.pathname.replace(/^\/+/, "").split("/", 2).map((part) => {
    try {
      return decodeURIComponent(part);
    } catch {
      return part;
    }
  });
  if (!kind || !id) return null;
  const module = decodeURIComponent(url.hostname);
  const label = url.searchParams.get("label") ?? "";
  const project = url.searchParams.get("project") ?? undefined;
  const lineParam = url.searchParams.get("line");
  const line = lineParam !== null && lineParam !== "" ? Number(lineParam) : undefined;
  return { module, kind, id, label: label || id, project, line };
}
