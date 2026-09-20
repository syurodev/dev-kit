import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

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

export type ApiRequest = {
  id: string;
  collectionId: string;
  name: string;
  method: string;
  url: string;
  query?: Record<string, string> | null;
  headers?: Record<string, string> | null;
  body?: string;
  auth: Auth;
  /** Points back at the Code Graph endpoint this was drafted from. */
  originRef?: string;
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

export type ApiResponse = {
  status: number;
  statusText: string;
  headers: Record<string, string> | null;
  body: string;
  truncated: boolean;
  bytes: number;
  durationMs: number;
};

export function listRequests(): Promise<ApiRequest[]> {
  return callBridge(async () => ((await AppService.ApiRequests()) ?? []) as ApiRequest[]);
}

export function getRequest(id: string): Promise<ApiRequest> {
  return callBridge(async () => (await AppService.ApiRequest(id)) as ApiRequest);
}

export function saveRequest(req: Partial<ApiRequest> & { name: string; method: string; url: string }) {
  return callBridge(
    async () =>
      (await AppService.ApiSaveRequest(
        req.id ?? "",
        req.collectionId ?? "",
        req.name,
        req.method,
        req.url,
        req.query ?? null,
        req.headers ?? null,
        req.body ?? "",
        req.auth?.kind ?? "",
        req.auth?.accountId ?? "",
        req.auth?.header ?? "",
        req.originRef ?? "",
      )) as ApiRequest,
  );
}

export function deleteRequest(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.ApiDeleteRequest(id);
  });
}

export function listEnvironments(): Promise<ApiEnvironment[]> {
  return callBridge(
    async () => ((await AppService.ApiEnvironments()) ?? []) as ApiEnvironment[],
  );
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

/** Sends. An environment is required — without one nothing could have gated it. */
export function sendRequest(id: string, environmentId: string): Promise<ApiResponse> {
  return callBridge(async () => (await AppService.ApiSend(id, environmentId)) as ApiResponse);
}

/** cURL with the credential left as a shell variable, safe to paste anywhere. */
export function curlFor(id: string, vars: Record<string, string>): Promise<string> {
  return callBridge(async () => await AppService.ApiCurl(id, vars));
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
 * Turns a Code Graph endpoint into a draft. Path parameters become
 * {{variables}}; the body is left to the caller, because only Java and
 * TypeScript can say what shape it is — a Kong or Lapis endpoint declares no
 * body at all, and showing an empty {} would read as "the DTO is empty".
 */
export function draftFromEndpoint(endpoint: {
  httpMethod: string;
  path: string;
  id: string;
}): Partial<ApiRequest> & { name: string; method: string; url: string } {
  const url = "{{baseUrl}}" + endpoint.path.replace(/\{(\w+)\}/g, "{{$1}}");
  return {
    name: `${endpoint.httpMethod} ${endpoint.path}`,
    method: endpoint.httpMethod,
    url,
    auth: { kind: "", accountId: "" },
    originRef: `devkit://codegraph/endpoint/${encodeURIComponent(endpoint.id)}`,
  };
}
