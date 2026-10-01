import type { KV, RequestInput } from "@/bridge/apiclient";

/**
 * The editor's arithmetic, kept out of the components so it can be tested
 * without a DOM: the URL bar and the Params table are two views of one
 * request, and keeping them in step is most of what makes it feel like a
 * client rather than a form.
 */

export const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"] as const;

/** A fresh request, in a collection or none. */
export function emptyRequest(collectionId = ""): RequestInput {
  return {
    id: "",
    collectionId,
    name: "Untitled request",
    method: "GET",
    url: "",
    query: [],
    headers: [],
    pathVars: [],
    bodyMode: "none",
    body: "",
    form: [],
    auth: { kind: "", accountId: "" },
    originRef: "",
  };
}

/** Rows that go out: enabled and named. */
export function activeRows(rows: readonly KV[] | null | undefined): KV[] {
  return (rows ?? []).filter((row) => !row.disabled && row.key.trim() !== "");
}

/**
 * What the URL bar shows: the address and its enabled query rows, written the
 * way a person types them rather than percent-encoded — `?q=a b`, not
 * `?q=a%20b`. The backend encodes on the way out.
 */
export function urlBarText(url: string, query: readonly KV[] | null | undefined): string {
  const rows = activeRows(query);
  if (rows.length === 0) return url;
  const joined = rows.map((row) => (row.value === "" ? row.key : `${row.key}=${row.value}`)).join("&");
  return `${url}${url.includes("?") ? "&" : "?"}${joined}`;
}

function decode(part: string): string {
  try {
    return decodeURIComponent(part.replace(/\+/g, " "));
  } catch {
    return part;
  }
}

/**
 * Reads what was typed in the URL bar back into an address and query rows.
 *
 * The rows the bar shows are the enabled ones, so they are replaced by what
 * was typed; a disabled row is not in the bar at all and is kept as it was —
 * otherwise editing the address would silently delete every parameter a
 * person had switched off.
 */
export function parseUrlBar(text: string, previous: readonly KV[] | null | undefined): { url: string; query: KV[] } {
  const hash = text.indexOf("#");
  const withoutHash = hash >= 0 ? text.slice(0, hash) : text;
  const at = withoutHash.indexOf("?");
  const disabled = (previous ?? []).filter((row) => row.disabled);
  if (at < 0) return { url: text, query: disabled };
  const url = withoutHash.slice(0, at);
  const query: KV[] = [];
  for (const pair of withoutHash.slice(at + 1).split("&")) {
    if (pair === "") continue;
    const eq = pair.indexOf("=");
    const key = eq >= 0 ? pair.slice(0, eq) : pair;
    const value = eq >= 0 ? pair.slice(eq + 1) : "";
    query.push({ key: decode(key), value: decode(value) });
  }
  return { url, query: [...query, ...disabled] };
}

/** The :name segments of an address's path, in order. */
export function pathVarNames(url: string): string[] {
  let path = url;
  const scheme = path.indexOf("://");
  if (scheme >= 0) {
    path = path.slice(scheme + 3);
    const slash = path.indexOf("/");
    if (slash < 0) return [];
    path = path.slice(slash);
  }
  const cut = path.search(/[?#]/);
  if (cut >= 0) path = path.slice(0, cut);
  const names: string[] = [];
  for (const segment of path.split("/")) {
    if (segment.length > 1 && segment.startsWith(":") && !names.includes(segment.slice(1))) {
      names.push(segment.slice(1));
    }
  }
  return names;
}

/**
 * One row per :name in the address, keeping each value already typed. A row
 * whose segment was deleted goes with it — it can no longer fill anything.
 */
export function syncPathVars(url: string, rows: readonly KV[] | null | undefined): KV[] {
  const byKey = new Map((rows ?? []).map((row) => [row.key, row]));
  return pathVarNames(url).map((name) => byKey.get(name) ?? { key: name, value: "" });
}

/** Two editor states that would save the same request. */
export function sameRequest(a: RequestInput, b: RequestInput): boolean {
  return JSON.stringify(normalizeForCompare(a)) === JSON.stringify(normalizeForCompare(b));
}

function normalizeForCompare(request: RequestInput) {
  const rows = (list: readonly KV[] | null | undefined) =>
    (list ?? [])
      .filter((row) => row.key !== "" || row.value !== "")
      .map((row) => ({ key: row.key, value: row.value, disabled: !!row.disabled }));
  return {
    collectionId: request.collectionId ?? "",
    name: request.name,
    method: request.method.toUpperCase(),
    url: request.url,
    query: rows(request.query),
    headers: rows(request.headers),
    pathVars: rows(request.pathVars),
    bodyMode: request.bodyMode || "none",
    body: request.bodyMode === "none" ? "" : (request.body ?? ""),
    form: rows(request.form),
    auth: { kind: request.auth?.kind ?? "", accountId: request.auth?.accountId ?? "", header: request.auth?.header ?? "" },
  };
}

/** {{name}} references anywhere a request can hold one. */
export function referencedVars(request: RequestInput): string[] {
  const texts: string[] = [request.url, request.body ?? ""];
  for (const list of [request.query, request.headers, request.pathVars, request.form]) {
    for (const row of activeRows(list)) texts.push(row.key, row.value);
  }
  const names = new Set<string>();
  for (const text of texts) {
    for (const match of text.matchAll(/\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}/g)) names.add(match[1]);
  }
  return [...names];
}

/** The colour a method is drawn in, Postman's convention. */
export function methodTone(method: string): string {
  switch (method.toUpperCase()) {
    case "GET":
      return "text-status-success";
    case "POST":
      return "text-status-warning";
    case "PUT":
    case "PATCH":
      return "text-status-info";
    case "DELETE":
      return "text-status-danger";
    default:
      return "text-text-secondary";
  }
}

/** A response's size the way people read one. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** JSON indented, anything else as it came. */
export function prettyBody(body: string, language: string | undefined): string {
  if (language !== "json") return body;
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}

/** The status line's tone: green for 2xx, amber for 3xx/4xx, red for 5xx. */
export function statusTone(status: number): string {
  if (status >= 500) return "text-status-danger";
  if (status >= 300) return "text-status-warning";
  if (status >= 200) return "text-status-success";
  return "text-text-secondary";
}
