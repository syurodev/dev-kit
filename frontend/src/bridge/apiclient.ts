import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";

/**
 * How a credential is presented. There is no field for a value: the request
 * stores an account id and the backend resolves it inside Go at send time,
 * which is what makes rotation a one-place edit and Copy as cURL safe.
 */
export type Auth = {
  kind: "" | "bearer" | "basic" | "header";
  accountId: string;
  header?: string;
};

/**
 * One row of a key/value table. `disabled` rather than `enabled` so a row
 * written without the flag — by an agent, an import, an older build — counts.
 */
export type KV = { key: string; value: string; disabled?: boolean };

export type BodyMode = "none" | "json" | "text" | "xml" | "urlencoded" | "multipart";

export const BODY_MODES: { id: BodyMode; label: string }[] = [
  { id: "none", label: "None" },
  { id: "json", label: "JSON" },
  { id: "text", label: "Text" },
  { id: "xml", label: "XML" },
  { id: "urlencoded", label: "Form URL-encoded" },
  { id: "multipart", label: "Multipart form" },
];

/** A request as an editor holds it — internal/apiclient.RequestInput. */
export type RequestInput = {
  id: string;
  collectionId: string;
  name: string;
  method: string;
  /** The address without its query; the query is `query`. */
  url: string;
  query?: KV[] | null;
  headers?: KV[] | null;
  /** Values for the URL's :name segments. */
  pathVars?: KV[] | null;
  bodyMode?: BodyMode | "";
  body?: string;
  /** The body for urlencoded and multipart. */
  form?: KV[] | null;
  auth: Auth;
  /** Points back at the Code Graph endpoint this was drafted from. */
  originRef?: string;
};

export type ApiRequest = RequestInput & {
  createdAt: string;
  updatedAt: string;
};

export type ApiEnvironment = {
  id: string;
  name: string;
  /** The env tag. Required: it is what decides whether sending asks first. */
  env: string;
  vars: Record<string, string> | null;
  /** Names of the encrypted variables. Their values are never returned. */
  secretVars: string[] | null;
};

/** One scope's variables: plain values, and the names of the sealed ones. */
export type VarSet = {
  vars: Record<string, string> | null;
  secretVars: string[] | null;
};

/**
 * An edit to one scope's variables. Secrets travel one way: `keepSecrets`
 * names the ones to leave as they are, `secrets` carries new values, and any
 * other existing secret is removed.
 */
export type VarsUpdate = {
  vars: Record<string, string>;
  secrets: Record<string, string>;
  keepSecrets: string[];
};

export type VarScope = "global" | "collection" | "environment";

/** A variable as a request would see it, after precedence. */
export type ResolvedVar = {
  name: string;
  /** Empty for a secret. */
  value: string;
  secret: boolean;
  scope: VarScope;
  scopeName: string;
};

export type ApiResponse = {
  status: number;
  statusText: string;
  headers: Record<string, string> | null;
  /** Every header line as it came — two Set-Cookie lines stay two. */
  headerList?: KV[] | null;
  body: string;
  truncated: boolean;
  bytes: number;
  durationMs: number;
  /** Where a redirect pointed when it was not followed for leaving the origin. */
  redirectBlocked?: string;
};

export type ImportResult = {
  format: string;
  collectionId?: string;
  environmentId?: string;
  name: string;
  folders: number;
  requests: number;
  variables: number;
  warnings: string[] | null;
};

/** The fields a request carries to the bridge — no timestamps. */
export function toInput(request: RequestInput): RequestInput {
  return {
    id: request.id ?? "",
    collectionId: request.collectionId ?? "",
    name: request.name,
    method: request.method,
    url: request.url,
    query: request.query ?? null,
    headers: request.headers ?? null,
    pathVars: request.pathVars ?? null,
    bodyMode: request.bodyMode ?? "",
    body: request.body ?? "",
    form: request.form ?? null,
    auth: { kind: request.auth?.kind ?? "", accountId: request.auth?.accountId ?? "", header: request.auth?.header ?? "" },
    originRef: request.originRef ?? "",
  };
}

// The generated bindings type a KVList as `KV[] | null` with optional fields
// and the request as a class; the shapes match field for field, so the casts
// below are the boundary and nothing past it.
type Bound = Parameters<typeof AppService.ApiSaveRequest>[0];

export function listRequests(): Promise<ApiRequest[]> {
  return callBridge(async () => ((await AppService.ApiRequests()) ?? []) as ApiRequest[]);
}

export function getRequest(id: string): Promise<ApiRequest> {
  return callBridge(async () => (await AppService.ApiRequest(id)) as ApiRequest);
}

export function saveRequest(request: RequestInput): Promise<ApiRequest> {
  return callBridge(async () => (await AppService.ApiSaveRequest(toInput(request) as Bound)) as ApiRequest);
}

export function deleteRequest(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiDeleteRequest(id);
  });
}

// ---- environments and variables ----------------------------------------

export function listEnvironments(): Promise<ApiEnvironment[]> {
  return callBridge(async () => ((await AppService.ApiEnvironments()) ?? []) as ApiEnvironment[]);
}

export function createEnvironment(
  name: string,
  env: string,
  vars: Record<string, string>,
  secrets: Record<string, string>,
): Promise<ApiEnvironment> {
  return callBridge(
    async () => (await AppService.ApiCreateEnvironment(name, env, vars, secrets)) as ApiEnvironment,
  );
}

export function updateEnvironment(id: string, name: string, env: string, update: VarsUpdate): Promise<ApiEnvironment> {
  return callBridge(async () => (await AppService.ApiUpdateEnvironment(id, name, env, update)) as ApiEnvironment);
}

export function duplicateEnvironment(id: string, name = ""): Promise<ApiEnvironment> {
  return callBridge(async () => (await AppService.ApiDuplicateEnvironment(id, name)) as ApiEnvironment);
}

export function deleteEnvironment(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiDeleteEnvironment(id);
  });
}

export function getGlobals(): Promise<VarSet> {
  return callBridge(async () => (await AppService.ApiGlobals()) as VarSet);
}

export function setGlobals(update: VarsUpdate): Promise<VarSet> {
  return callBridge(async () => (await AppService.ApiSetGlobals(update)) as VarSet);
}

/** What a request in `collectionId` would see under `environmentId`. */
export function variablesFor(collectionId: string, environmentId: string): Promise<ResolvedVar[]> {
  return callBridge(
    async () => ((await AppService.ApiVariables(collectionId, environmentId)) ?? []) as ResolvedVar[],
  );
}

// ---- sending -----------------------------------------------------------

/** Sends a saved request as stored. An environment is required. */
export function sendRequest(id: string, environmentId: string): Promise<ApiResponse> {
  return callBridge(async () => (await AppService.ApiSend(id, environmentId)) as ApiResponse);
}

/**
 * Sends what the editor holds, saved or not. Cancelling it stops the Go
 * context, which reaches the socket — and `sendId` lets a second window or a
 * command cancel it too.
 */
export function sendDraft(request: RequestInput, environmentId: string, sendId: string): CancellableCall<ApiResponse> {
  return callBridgeCancellable(
    () => AppService.ApiSendRequest(toInput(request) as Bound, environmentId, sendId),
    (raw) => raw as ApiResponse,
  );
}

export function cancelSend(sendId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiCancelSend(sendId);
  });
}

/** cURL with the plain variables filled and the credential left as a shell variable. */
export function curlFor(request: RequestInput, environmentId: string): Promise<string> {
  return callBridge(async () => await AppService.ApiCurl(toInput(request) as Bound, environmentId));
}

/** Reads a pasted curl command. Nothing is saved and nothing runs. */
export function parseCurl(command: string): Promise<RequestInput> {
  return callBridge(async () => (await AppService.ApiParseCurl(command)) as RequestInput);
}

/** True when pasted text is a curl command rather than an address. */
export function looksLikeCurl(text: string): boolean {
  return /^\s*curl(\.exe)?\s/i.test(text);
}

// ---- other tools' files ---------------------------------------------

export type ExportKind = "collection" | "environment" | "globals";

/**
 * A file format the API client speaks, and what it can do. The list comes
 * from Go; the window never knows a format by name, so adding one there
 * needs nothing here.
 */
export type FormatInfo = {
  id: string;
  label: string;
  extension: string;
  canImport: boolean;
  exports: ExportKind[] | null;
};

export type ExportScope = { kind: ExportKind; id: string };

export type ExportFile = { fileName: string; content: string; warnings: string[] | null };

export type ExportSaved = { path: string; fileName: string; warnings: string[] | null };

export function listFormats(): Promise<FormatInfo[]> {
  return callBridge(async () => ((await AppService.ApiFormats()) ?? []) as FormatInfo[]);
}

/** Reads a file into the vault. An empty format id detects it. */
export function importFile(formatId: string, data: string, envTag: string): Promise<ImportResult> {
  return callBridge(async () => (await AppService.ApiImport(formatId, data, { envTag })) as ImportResult);
}

/** An export as text, for copying. Never holds a secret value or a credential. */
// The bindings type the kind as a generated enum whose values are these
// strings; the cast is the boundary.
type BoundScope = Parameters<typeof AppService.ApiExport>[0];

export function exportFile(scope: ExportScope, formatId: string): Promise<ExportFile> {
  return callBridge(async () => (await AppService.ApiExport(scope as BoundScope, formatId)) as ExportFile);
}

/** An export written to a file the person picks; `path` is empty if they cancelled. */
export function exportToFile(scope: ExportScope, formatId: string): Promise<ExportSaved> {
  return callBridge(async () => (await AppService.ApiExportToFile(scope as BoundScope, formatId)) as ExportSaved);
}

/** {{name}} placeholders a set of variables cannot fill. */
export function missingVars(template: string, vars: Record<string, string>): string[] {
  const missing = new Set<string>();
  for (const match of template.matchAll(/\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}/g)) {
    if (!(match[1] in vars)) {
      missing.add(match[1]);
    }
  }
  return [...missing];
}

/**
 * Fills {{name}} placeholders from an environment, for working out which host
 * a request actually goes to.
 *
 * It is not the sender — the backend substitutes for real, including the
 * secret variables this never sees. It exists so the UI can ask "which account
 * covers this host" against the server the request would reach rather than
 * against the template, and it leaves an unfillable placeholder in place so a
 * caller can tell the URL is not resolved yet.
 */
export function resolveUrl(template: string, vars: Record<string, string>): string {
  return template.replace(/\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}/g, (whole, name: string) =>
    name in vars ? vars[name] : whole,
  );
}

/**
 * A request drafted from a Code Graph endpoint, not saved.
 *
 * `bodyInferred` false is not a failure: a Lua endpoint declares no body at
 * all, and a Java handler may take one whose type is outside this project.
 * `note` says which, because an empty `{}` for every case would read as "the
 * DTO is empty" — the one thing none of them means.
 */
export type ApiDraft = ApiRequest & { bodyInferred: boolean; note: string };

/**
 * A draft request from an endpoint of the index of `servicePath` — the folder it
 * was listed in ("" is the focused project, kept for callers with no folder).
 */
export function draftFromEndpoint(endpointId: string, servicePath = ""): Promise<ApiDraft> {
  return callBridge(async () => (await AppService.ApiDraftFromEndpoint(endpointId, servicePath)) as ApiDraft);
}

// ---- collections ------------------------------------------------------

/**
 * A collection (no parent) or a folder in one. Both carry variables, and a
 * nearer folder's win over its parents'.
 */
export type ApiCollection = {
  id: string;
  name: string;
  parentId?: string;
  order: number;
  vars?: Record<string, string> | null;
  secretVars?: string[] | null;
};

export function listCollections(): Promise<ApiCollection[]> {
  return callBridge(async () => ((await AppService.ApiCollections()) ?? []) as ApiCollection[]);
}

/** Creates, renames or moves a folder. Its variables are left as they are. */
export function saveCollection(collection: Partial<ApiCollection> & { name: string }): Promise<ApiCollection> {
  return callBridge(
    async () =>
      (await AppService.ApiSaveCollection(
        collection.id ?? "",
        collection.name,
        collection.parentId ?? "",
        collection.order ?? 0,
      )) as ApiCollection,
  );
}

export function setCollectionVariables(id: string, update: VarsUpdate): Promise<ApiCollection> {
  return callBridge(async () => (await AppService.ApiSetCollectionVariables(id, update)) as ApiCollection);
}

/**
 * Removes an empty folder. A folder still holding anything is refused, and the
 * error names what is inside — tidying is not the same action as losing every
 * saved request in it, and a vault item has no undo.
 */
export function deleteCollection(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiDeleteCollection(id);
  });
}

/** Removes a folder with everything in it, after the person has confirmed. */
export function deleteCollectionTree(id: string): Promise<number> {
  return callBridge(async () => await AppService.ApiDeleteCollectionTree(id));
}

// ---- history ----------------------------------------------------------

/**
 * One send. There is no body and no headers here on purpose: either side can
 * carry a token, a body can be megabytes, and re-sending is one click.
 */
export type ApiHistoryEntry = {
  id: string;
  requestId: string;
  environmentId: string;
  method: string;
  /** Sealed at rest; empty when the vault is locked, which is not an error. */
  url: string;
  status: number;
  durationMs: number;
  bytes: number;
  /** Why nothing came back. Empty when there is a status. */
  failure: string;
  sentAt: string;
};

export function requestHistory(requestId: string, limit = 0): Promise<ApiHistoryEntry[]> {
  return callBridge(
    async () => ((await AppService.ApiHistory(requestId, limit)) ?? []) as ApiHistoryEntry[],
  );
}

export function clearRequestHistory(requestId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiClearHistory(requestId);
  });
}

export const API_CLIENT_CHANGED_EVENT = "apiclient:changed";

/**
 * Fires after any write to a request, collection, environment or the globals
 * — an agent building a collection over MCP, a person in another window — so
 * the sidebar refreshes without being asked. Carries nothing: a refresh is one
 * list call per kind, cheaper than describing what changed. Returns an
 * unsubscribe function.
 */
export function onApiClientChanged(cb: () => void): () => void {
  return Events.On(API_CLIENT_CHANGED_EVENT, () => cb());
}
