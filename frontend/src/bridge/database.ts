import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type * as DatabaseModels from "../../bindings/devkit/internal/database/models";

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
  sshProfileId?: string;
  engine?: string;
  driverOptions?: Record<string, string>;
  /**
   * PEM bundle to verify the server certificate against, instead of the
   * machine's trust store. Managed databases sign with their own authority —
   * an RDS certificate chains to an Amazon RDS CA that a desktop does not have
   * — so without this, verify-full is unreachable for them.
   */
  caCert?: string;
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
  engine?: string;
  /** Set at creation so a tunnelled profile never exists with a direct path. */
  sshProfileId?: string;
  /**
   * PEM bundle to verify the server certificate against, instead of the
   * machine's trust store. Managed databases sign with their own authority —
   * an RDS certificate chains to an Amazon RDS CA that a desktop does not have
   * — so without this, verify-full is unreachable for them.
   */
  caCert?: string;
};

export type UpdateProfileOptionsInput = {
  env: string;
  readOnly: boolean;
  sshProfileId?: string;
  /**
   * PEM bundle to verify the server certificate against, instead of the
   * machine's trust store. Managed databases sign with their own authority —
   * an RDS certificate chains to an Amazon RDS CA that a desktop does not have
   * — so without this, verify-full is unreachable for them.
   */
  caCert?: string;
};

export type QueryResult = {
  columns: string[];
  /** Each cell as text, null for SQL NULL (#47) — never the four letters. */
  rows: (string | null)[][];
  rowCount: number;
  truncated: boolean;
  /** What an UPDATE, INSERT or DELETE changed; -1 when the driver cannot say. */
  rowsAffected?: number;
};

export type TableInfo = {
  schema: string;
  name: string;
  kind: string;
};

export type ColumnInfo = {
  name: string;
  dataType: string;
  nullable: boolean;
  position: number;
};

export type IndexInfo = {
  schema: string;
  table: string;
  name: string;
  columns: string[];
  unique: boolean;
  primary: boolean;
};

export type ForeignKey = {
  schema: string;
  table: string;
  column: string;
  referencedSchema: string;
  referencedTable: string;
  referencedColumn: string;
};

export type SchemaNamespace = Record<string, Record<string, string[]>>;

/** Same three levels as SchemaNamespace, carrying the whole column. */
export type SchemaColumns = Record<string, Record<string, ColumnInfo[]>>;

export type SchemaSnapshot = {
  /** Names only — the shape @codemirror/lang-sql consumes. */
  namespace: SchemaNamespace;
  columns: SchemaColumns;
  indexedAt: string;
};

function toSnapshot(snap: DatabaseModels.SchemaSnapshot): SchemaSnapshot {
  const namespace: SchemaNamespace = {};
  for (const [schema, tables] of Object.entries(snap.namespace ?? {})) {
    if (!tables) continue;
    namespace[schema] = {};
    for (const [table, cols] of Object.entries(tables)) {
      namespace[schema][table] = (cols ?? []).map((c) => c ?? "");
    }
  }
  return {
    namespace,
    columns: toSchemaColumns(snap.columns),
    indexedAt: snap.indexed_at,
  };
}

/**
 * The index already reads every column's type, so the tree has it without
 * asking again. Empty for a snapshot written before the cache carried types —
 * the backend reports one of those as a miss and re-indexes, so this is only
 * ever empty for the moment before that lands.
 */
function toSchemaColumns(raw: DatabaseModels.SchemaColumns): SchemaColumns {
  const out: SchemaColumns = {};
  for (const [schema, tables] of Object.entries(raw ?? {})) {
    if (!tables) continue;
    out[schema] = {};
    for (const [table, columns] of Object.entries(tables)) {
      out[schema][table] = (columns ?? []).map((c) => ({
        name: c.name,
        dataType: c.data_type,
        nullable: c.nullable,
        position: c.position,
      }));
    }
  }
  return out;
}

export type HistoryEntry = {
  id: string;
  profileId: string;
  sql: string;
  createdAt: string;
};

export type SavedQuery = {
  id: string;
  profileId: string;
  name: string;
  sql: string;
};

export type SessionRow = {
  pid: string;
  state: string;
  duration: string;
  user: string;
  client: string;
  waitEvent: string;
  blockedBy: string[];
  query: string;
  application: string;
  startedAt: string;
};

export type PlanNode = {
  operation: string;
  depth: number;
  estimatedRows: number;
  actualRows: number;
  selfMillis: number;
  detail: string;
  children?: PlanNode[];
};

export type QueryPlan = {
  nodes: PlanNode[];
  raw: string;
  analyzed: boolean;
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
  ssh_profile_id?: string;
  ca_cert?: string;
  engine?: string;
  driver_options?: { [_ in string]?: string } | null;
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
    sshProfileId: p.ssh_profile_id || undefined,
    caCert: p.ca_cert || undefined,
    engine: p.engine || "postgres",
    driverOptions: p.driver_options
      ? (Object.fromEntries(
          Object.entries(p.driver_options).filter(([, v]) => v !== undefined)
        ) as Record<string, string>)
      : undefined,
  };
}

function toPlanNode(n: {
  operation: string;
  depth: number;
  estimated_rows: number;
  actual_rows: number;
  self_millis: number;
  detail: string;
  children?: any[] | null;
}): PlanNode {
  return {
    operation: n.operation,
    depth: n.depth,
    estimatedRows: n.estimated_rows,
    actualRows: n.actual_rows,
    selfMillis: n.self_millis,
    detail: n.detail,
    children: (n.children ?? []).map(toPlanNode),
  };
}

function toQueryPlan(p: {
  nodes: any[] | null;
  raw: string;
  analyzed: boolean;
}): QueryPlan {
  return {
    nodes: (p.nodes ?? []).map(toPlanNode),
    raw: p.raw,
    analyzed: p.analyzed,
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
      engine: input.engine || "postgres",
      name: input.name,
      host: input.host,
      port: input.port,
      database: input.database,
      username: input.username,
      password: input.password,
      ssl_mode: input.sslMode,
      env: input.env,
      // Sent at creation, not patched afterwards: a tunnelled profile must
      // never exist with a direct connection path, however briefly.
      ssh_profile_id: input.sshProfileId ?? "",
      ca_cert: input.caCert ?? "",
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
      ssh_profile_id: opts.sshProfileId || "",
      ca_cert: opts.caCert ?? "",
    });
    return toProfile(updated);
  });
}

export function deleteProfile(id: string): Promise<void> {
  return callBridge(() => AppService.DBDeleteProfile(id) as Promise<void>);
}

/** Dials a profile that has not been saved yet. */
export function testNewConnection(input: CreateProfileInput): Promise<void> {
  return callBridge(
    () =>
      AppService.DBTestNewConnection({
        engine: input.engine ?? "",
        name: input.name,
        host: input.host,
        port: input.port,
        database: input.database,
        username: input.username,
        password: input.password,
        ssl_mode: input.sslMode,
        env: input.env,
        ssh_profile_id: input.sshProfileId ?? "",
      ca_cert: input.caCert ?? "",
      }) as Promise<void>,
  );
}

export function testConnection(id: string): Promise<void> {
  return callBridge(() => AppService.DBTestConnection(id) as Promise<void>);
}

/**
 * Cancellable: DBQuery takes a context all the way down to the driver, so
 * stopping the promise stops the statement (see callBridgeCancellable).
 *
 * `maxRows` is the caller's row cap, for the table browser's page size. 0 —
 * the default — is the service's own cap, which is what the console and every
 * catalogue read want: they have no page to size.
 */
export function runQuery(id: string, sql: string, maxRows = 0): CancellableCall<QueryResult> {
  return callBridgeCancellable(
    () => AppService.DBQuery(id, sql, maxRows),
    (result) => ({
      columns: result.columns ?? [],
      rows: (result.rows ?? []).map((r) => (r ?? []).map((c) => c ?? null)),
      rowCount: result.row_count,
      truncated: result.truncated,
      rowsAffected: result.rows_affected,
    }),
  );
}

/**
 * Manual-commit mode.
 *
 * A transaction belongs to the CONNECTION, not to a tab: DevKit keeps one
 * connection per profile, so a console and a table tab pointed at the same
 * profile are inside the same transaction. That is what every database tool
 * does, and the alternative — a connection per tab — would multiply the locks
 * taken against the server by however many tabs are open.
 *
 * Choosing the mode opens nothing; the first statement does. An idle
 * transaction holds a metadata lock on every table it has touched and stops
 * old row versions being cleaned up, so browsing in Auto — the default — takes
 * nothing at all.
 */
export function setTransactionMode(id: string, manual: boolean): Promise<void> {
  return callBridge(() => AppService.DBSetTxMode(id, manual));
}

export function commitTransaction(id: string): Promise<void> {
  return callBridge(() => AppService.DBCommitTx(id));
}

export function rollbackTransaction(id: string): Promise<void> {
  return callBridge(() => AppService.DBRollbackTx(id));
}

/**
 * The mode and whether a transaction is open — asked on mount, since both
 * belong to the connection and outlive the window that chose them.
 */
export function transactionState(id: string): Promise<{ manual: boolean; open: boolean }> {
  return callBridge(async () => ({
    manual: await AppService.DBTxMode(id),
    open: await AppService.DBTxOpen(id),
  }));
}

/**
 * The statement that defines a table.
 *
 * MySQL and MariaDB answer with SHOW CREATE TABLE — their own words. Postgres
 * keeps no such text, so its definition is rebuilt from the catalogues, with
 * the hard parts (a check expression, a foreign key's ON DELETE, a partial
 * index's WHERE) printed by the server rather than reassembled.
 */
export function tableDDL(id: string, schema: string, table: string): Promise<string> {
  return callBridge(() => AppService.DBTableDDL(id, schema, table));
}

export function saveExportFile(suggestedName: string, content: string): Promise<string> {
  return callBridge(() => AppService.DBSaveExportFile(suggestedName, content) as Promise<string>);
}

export function listSchemas(id: string): Promise<string[]> {
  return callBridge(async () => {
    const list = await AppService.DBListSchemas(id);
    return list ?? [];
  });
}

export function listTables(id: string, schema: string): Promise<TableInfo[]> {
  return callBridge(async () => {
    const list = await AppService.DBListTables(id, schema);
    return (list ?? []).map((t) => ({
      schema: t.schema,
      name: t.name,
      kind: t.kind,
    }));
  });
}

export function listColumns(id: string, schema: string, table: string): Promise<ColumnInfo[]> {
  return callBridge(async () => {
    const list = await AppService.DBListColumns(id, schema, table);
    return (list ?? []).map((c) => ({
      name: c.name,
      dataType: c.data_type,
      nullable: c.nullable,
      position: c.position,
    }));
  });
}

export function listIndexes(id: string, schema: string): Promise<IndexInfo[]> {
  return callBridge(async () => {
    const list = await AppService.DBListIndexes(id, schema);
    return (list ?? []).map((i) => ({
      schema: i.schema,
      table: i.table,
      name: i.name,
      columns: i.columns ?? [],
      unique: i.unique,
      primary: i.primary,
    }));
  });
}

export function listForeignKeys(id: string, schema: string): Promise<ForeignKey[]> {
  return callBridge(async () => {
    const list = await AppService.DBListForeignKeys(id, schema);
    return (list ?? []).map((fk) => ({
      schema: fk.schema,
      table: fk.table,
      column: fk.column,
      referencedSchema: fk.referenced_schema,
      referencedTable: fk.referenced_table,
      referencedColumn: fk.referenced_column,
    }));
  });
}

export function indexSchema(id: string): Promise<SchemaSnapshot> {
  return callBridge(async () => {
    return toSnapshot(await AppService.DBIndexSchema(id));
  });
}

export function getSchemaCache(id: string): Promise<SchemaSnapshot> {
  return callBridge(async () => {
    return toSnapshot(await AppService.DBGetSchemaCache(id));
  });
}

export function listQueryHistory(id: string): Promise<HistoryEntry[]> {
  return callBridge(async () => {
    const list = await AppService.DBListQueryHistory(id);
    return (list ?? []).map((h) => ({
      id: h.id,
      profileId: h.profile_id,
      sql: h.sql,
      createdAt: h.created_at,
    }));
  });
}

export function saveQuery(id: string, name: string, sql: string): Promise<SavedQuery> {
  return callBridge(async () => {
    const saved = await AppService.DBSaveQuery(id, name, sql);
    return {
      id: saved.id,
      profileId: saved.profile_id,
      name: saved.name,
      sql: saved.sql,
    };
  });
}

export function listSavedQueries(id: string): Promise<SavedQuery[]> {
  return callBridge(async () => {
    const list = await AppService.DBListSavedQueries(id);
    return (list ?? []).map((q) => ({
      id: q.id,
      profileId: q.profile_id,
      name: q.name,
      sql: q.sql,
    }));
  });
}

export function deleteSavedQuery(id: string): Promise<void> {
  return callBridge(() => AppService.DBDeleteSavedQuery(id) as Promise<void>);
}

export function listActivity(id: string): Promise<SessionRow[]> {
  return callBridge(async () => {
    const list = await AppService.DBListActivity(id);
    return (list ?? []).map((s) => ({
      pid: s.pid,
      state: s.state,
      duration: s.duration,
      user: s.user,
      client: s.client,
      waitEvent: s.wait_event,
      blockedBy: (s.blocked_by ?? []).map((b) => b ?? ""),
      query: s.query,
      application: s.application,
      startedAt: s.started_at,
    }));
  });
}

export function cancelQuery(id: string, pid: string): Promise<void> {
  return callBridge(() => AppService.DBCancelQuery(id, pid) as Promise<void>);
}

export function terminateSession(id: string, pid: string): Promise<void> {
  return callBridge(() => AppService.DBTerminateSession(id, pid) as Promise<void>);
}

export function explain(id: string, sql: string): Promise<QueryPlan> {
  return callBridge(async () => {
    const plan = await AppService.DBExplain(id, sql);
    return toQueryPlan(plan);
  });
}

export function explainAnalyze(id: string, sql: string): Promise<QueryPlan> {
  return callBridge(async () => {
    const plan = await AppService.DBExplainAnalyze(id, sql);
    return toQueryPlan(plan);
  });
}

/**
 * A query console's text as it is kept on disk — DataGrip's model: one .sql
 * per console, written as you type, with the connection it belongs to on the
 * first line of the file.
 *
 * Not the same thing as a SavedQuery, and it must not become one. A saved
 * query is named, deliberate, and a synced vault item; a console is the
 * scratch pad you left open and expect to find again.
 */
export type StoredConsole = {
  id: string;
  profileId: string;
  /** The schema or database it was last pointed at. */
  database: string;
  sql: string;
  updatedAt: string;
};

export function listConsoles(): Promise<StoredConsole[]> {
  return callBridge(async () => {
    const list = await AppService.DBListConsoles();
    return (list ?? []).map((c) => ({
      id: c.id,
      profileId: c.profile_id,
      database: c.database,
      sql: c.sql,
      updatedAt: c.updated_at,
    }));
  });
}

/**
 * Four arguments rather than the Console object, because the object carries
 * an `updated_at` the window does not have. Wails parses every argument
 * before the method runs, so a time in the signature made a timestamp
 * mandatory here; sending "" failed in the binding layer and nothing was
 * ever written. The stamp belongs to whoever writes the bytes.
 */
export function saveConsole(console: Omit<StoredConsole, "updatedAt">): Promise<void> {
  return callBridge(
    () =>
      AppService.DBSaveConsole(
        console.id,
        console.profileId,
        console.database,
        console.sql,
      ) as Promise<void>,
  );
}

export function deleteConsole(id: string): Promise<void> {
  return callBridge(() => AppService.DBDeleteConsole(id) as Promise<void>);
}

/** Where the console files are kept, and where they would go by default. */
export type ConsoleDirectory = {
  path: string;
  /** The folder they return to when the setting is cleared. */
  fallback: string;
  /** Whether `path` is a choice somebody made. */
  custom: boolean;
};

const asDirectory = (d: { path: string; default: string; custom: boolean }): ConsoleDirectory => ({
  path: d.path,
  fallback: d.default,
  custom: d.custom,
});

export function consoleDirectory(): Promise<ConsoleDirectory> {
  return callBridge(async () => asDirectory(await AppService.DBConsoleDirectory()));
}

/** Moves the console files. An empty path means "back to the default". */
export function setConsoleDirectory(path: string): Promise<ConsoleDirectory> {
  return callBridge(async () => asDirectory(await AppService.DBSetConsoleDirectory(path)));
}

/** The native folder picker. "" when the user cancelled, which is not an error. */
export function chooseConsoleDirectory(): Promise<string> {
  return callBridge(() => AppService.DBChooseConsoleDirectory() as Promise<string>);
}
