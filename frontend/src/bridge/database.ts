import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type ConnectionProfile = {
  id: string;
  name: string;
  host: string;
  port: number;
  database: string;
  username: string;
  sslMode: string;
  env: string;
  readOnly: boolean;
};

export type CreateProfileInput = {
  name: string;
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
  sslMode: string;
  env: string;
};

export type UpdateProfileOptionsInput = {
  env: string;
  readOnly: boolean;
};

export type QueryResult = {
  columns: string[];
  rows: string[][];
  rowCount: number;
  truncated: boolean;
};

function toProfile(p: {
  id: string;
  name: string;
  host: string;
  port: number;
  database: string;
  username: string;
  ssl_mode: string;
  env: string;
  read_only?: boolean;
}): ConnectionProfile {
  return {
    id: p.id,
    name: p.name,
    host: p.host,
    port: p.port,
    database: p.database,
    username: p.username,
    sslMode: p.ssl_mode,
    env: p.env,
    readOnly: Boolean(p.read_only),
  };
}

export function listProfiles(): Promise<ConnectionProfile[]> {
  return callBridge(async () => {
    const list = await AppService.DBListProfiles();
    return (list ?? []).map(toProfile);
  });
}

export function setDBActiveProfile(id: string): Promise<void> {
  return callBridge(() => AppService.DBSetActiveProfile(id) as Promise<void>);
}

export function getDBActiveProfile(): Promise<string> {
  return callBridge(async () => {
    const active = await AppService.DBActiveProfile();
    return active.profileId ?? "";
  });
}

export function createProfile(input: CreateProfileInput): Promise<ConnectionProfile> {
  return callBridge(async () => {
    const created = await AppService.DBCreateProfile({
      name: input.name,
      host: input.host,
      port: input.port,
      database: input.database,
      username: input.username,
      password: input.password,
      ssl_mode: input.sslMode,
      env: input.env,
    });
    return toProfile(created);
  });
}

export function updateProfileOptions(
  id: string,
  opts: UpdateProfileOptionsInput
): Promise<ConnectionProfile> {
  return callBridge(async () => {
    const updated = await AppService.DBUpdateProfileOptions(id, {
      env: opts.env,
      read_only: opts.readOnly,
    });
    return toProfile(updated);
  });
}

export function deleteProfile(id: string): Promise<void> {
  return callBridge(() => AppService.DBDeleteProfile(id) as Promise<void>);
}

export function testConnection(id: string): Promise<void> {
  return callBridge(() => AppService.DBTestConnection(id) as Promise<void>);
}

export function runQuery(id: string, sql: string): Promise<QueryResult> {
  return callBridge(async () => {
    const result = await AppService.DBQuery(id, sql);
    return {
      columns: result.columns ?? [],
      rows: (result.rows ?? []).map((r) => r ?? []),
      rowCount: result.row_count,
      truncated: result.truncated,
    };
  });
}

export function saveExportFile(suggestedName: string, content: string): Promise<string> {
  return callBridge(() => AppService.DBSaveExportFile(suggestedName, content) as Promise<string>);
}
