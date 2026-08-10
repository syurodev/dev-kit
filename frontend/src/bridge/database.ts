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
}): ConnectionProfile {
  return {
    id: p.id,
    name: p.name,
    host: p.host,
    port: p.port,
    database: p.database,
    username: p.username,
    sslMode: p.ssl_mode,
  };
}

export function listProfiles(): Promise<ConnectionProfile[]> {
  return callBridge(async () => {
    const list = await AppService.DBListProfiles();
    return (list ?? []).map(toProfile);
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
