import { toolNameOf, toolPayloadOf, type ToolCallLike } from "@/workbench/toolPayload";
import { createToolRegistry } from "@/workbench/toolResultViews";
import type { ToolCall, TranscriptItem } from "../types";

/**
 * A database table or Kafka topic this session's own tool results named,
 * found again in the agent's prose. Nothing is looked up: the names are what
 * the session already read, so a reference is never a guess about what exists.
 */
export type EntityHit =
  | {
      kind: "table";
      /** `database.reveal`'s id: `schema.table` (modules/database/manifest.ts). */
      id: string;
      /** What the reference is called: the table's own name. */
      name: string;
      schema: string;
      table: string;
      /**
       * The connection the call that named it read it on (`rawInput.profileId`):
       * `database.reveal` opens it there, never on whichever is selected.
       */
      profileId?: string;
      /** That connection's name, when the session's own `db.listProfiles` said it. */
      profileName?: string;
    }
  | { kind: "topic"; id: string; name: string };

export type SessionEntities = {
  /**
   * `schema.table` always; the bare `table` too when no other schema has one
   * by that name. A table is its profile + schema + table: the same name read
   * on two connections (staging and prod) is neither, since a link would have
   * to pick one.
   */
  tables: ReadonlyMap<string, EntityHit>;
  topics: ReadonlyMap<string, EntityHit>;
};

export const NO_ENTITIES: SessionEntities = { tables: new Map(), topics: new Map() };

/**
 * The table or topic a code span names exactly, or null. A name that is both
 * a table and a topic is neither: a wrong link is worse than none.
 */
export function entityOf(entities: SessionEntities | undefined, span: string): EntityHit | null {
  if (!entities) return null;
  const table = entities.tables.get(span);
  const topic = entities.topics.get(span);
  if (table && topic) return null;
  return table ?? topic ?? null;
}

/**
 * `database.reveal`'s table id. The id splits at its first dot, so a schema
 * holding a dot — or opening with a quote — is written in SQL double quotes,
 * with `""` for a quote inside.
 */
export function tableId(schema: string, table: string): string {
  const quoted = schema.includes(".") || schema.startsWith('"');
  return `${quoted ? `"${schema.replace(/"/g, '""')}"` : schema}.${table}`;
}

/**
 * What one call contributes. `key` is its content, so a call that comes back
 * as a new object naming the same things (a non-material update) changes
 * nothing downstream.
 */
type Contribution = Found & { key: string };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const nonEmpty = (value: unknown): value is string => typeof value === "string" && value !== "";

/** A `{result: [...]}` payload, whether or not the payload layer already unwrapped it. */
function rows(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  return isRecord(payload) && Array.isArray(payload.result) ? payload.result : [];
}

/** A table as one call named it: on the connection its input named, when it named one. */
type TableName = { schema: string; table: string; profileId?: string };

type Found = {
  tables: TableName[];
  topics: string[];
  /** Connection names by id, from `db.listProfiles`. */
  profiles?: { id: string; name: string }[];
};

/** The connection a database call read, from its input. */
function profileOf(rawInput: unknown): string | undefined {
  const id = isRecord(rawInput) ? rawInput.profileId : undefined;
  return nonEmpty(id) ? id : undefined;
}

/** Each table on the call's connection. */
function onProfile(tables: { schema: string; table: string }[], rawInput: unknown): TableName[] {
  const profileId = profileOf(rawInput);
  return profileId ? tables.map((table) => ({ ...table, profileId })) : tables;
}

/**
 * How a tool's calls name things. A payload reader parses the answer, so a
 * cut answer names nothing until its full row is loaded; an input reader
 * needs only to know the call answered without an error.
 */
type Reader = { payload: true; read: (payload: unknown, rawInput: unknown) => Found } | { payload: false; read: (rawInput: unknown) => Found };

/** By the tool's Go name; found again from Claude's wire form or Codex's title (toolResultViews). */
const readers = createToolRegistry<Reader>();

// `{namespace: {schema: {table: [col]}}, columns, indexed_at}`
readers.register("db.schema.get", {
  payload: true,
  read: (payload, rawInput) => {
    const tables: { schema: string; table: string }[] = [];
    const namespace = isRecord(payload) ? payload.namespace : undefined;
    if (isRecord(namespace)) {
      for (const [schema, byTable] of Object.entries(namespace)) {
        if (!schema || !isRecord(byTable)) continue;
        for (const table of Object.keys(byTable)) if (table) tables.push({ schema, table });
      }
    }
    return { tables: onProfile(tables, rawInput), topics: [] };
  },
});

// `{result: [{schema, table, column, referenced_schema, referenced_table, referenced_column}]}`: both ends.
readers.register("db.listForeignKeys", {
  payload: true,
  read: (payload, rawInput) => {
    const tables: { schema: string; table: string }[] = [];
    for (const row of rows(payload)) {
      if (!isRecord(row)) continue;
      if (nonEmpty(row.schema) && nonEmpty(row.table)) tables.push({ schema: row.schema, table: row.table });
      if (nonEmpty(row.referenced_schema) && nonEmpty(row.referenced_table)) {
        tables.push({ schema: row.referenced_schema, table: row.referenced_table });
      }
    }
    return { tables: onProfile(tables, rawInput), topics: [] };
  },
});

// `{result: [{id, name, …}]}`: what the tables' connections are called.
readers.register("db.listProfiles", {
  payload: true,
  read: (payload) => ({
    tables: [],
    topics: [],
    profiles: rows(payload).flatMap((row) => (isRecord(row) && nonEmpty(row.id) && nonEmpty(row.name) ? [{ id: row.id, name: row.name }] : [])),
  }),
});

// `{result: [{name}]}`
readers.register("kafka.listTopics", {
  payload: true,
  read: (payload) => ({
    tables: [],
    topics: rows(payload)
      .map((row) => (isRecord(row) ? row.name : row))
      .filter(nonEmpty),
  }),
});

// A topic the agent peeked, described or published to — and that answered.
for (const tool of ["kafka.peekMessages", "kafka.describeTopic", "kafka.publishMessage"]) {
  readers.register(tool, {
    payload: false,
    read: (rawInput) => {
      const topic = isRecord(rawInput) ? rawInput.topic : undefined;
      return { tables: [], topics: nonEmpty(topic) ? [topic] : [] };
    },
  });
}

const readerOf = (call: ToolCallLike) => readers.lookup(toolNameOf(call));

/** What one call names, and whether its answer was cut so that only its full row can say. */
type Read = { contribution: Contribution | null; cut: boolean };

function contentKey(found: Found): string {
  const tables = found.tables.map((t) => `${t.profileId ?? ""}\u0000${t.schema}\u0000${t.table}`).join("\u0001");
  const profiles = (found.profiles ?? []).map((p) => `${p.id}\u0000${p.name}`).join("\u0001");
  return `${tables}\u0002${found.topics.join("\u0001")}\u0002${profiles}`;
}

function readCall(call: ToolCallLike): Read {
  const reader = readerOf(call);
  if (!reader || call.status?.toLowerCase() !== "completed") return { contribution: null, cut: false };
  const result = toolPayloadOf(call);
  if (result && "error" in result) return { contribution: null, cut: false };
  let found: Found;
  if (reader.payload) {
    // No payload on a completed call: the page read cut it.
    if (!result) return { contribution: null, cut: Boolean(call.truncated) };
    found = reader.read(result.payload, call.rawInput);
  } else {
    found = reader.read(call.rawInput);
  }
  if (found.tables.length === 0 && found.topics.length === 0 && !found.profiles?.length) return { contribution: null, cut: false };
  return { contribution: { ...found, key: contentKey(found) }, cut: false };
}

/**
 * Per call object, read once: a transcript publishes a new items array on
 * every streamed token, and a long session must not re-parse every tool
 * payload each time. A call that changes (an update lands) is a new object.
 * A full row is cached by its own object, which the query keeps.
 */
const reads = new WeakMap<object, Read>();

function readOf(call: ToolCallLike): Read {
  let read = reads.get(call);
  if (!read) {
    read = readCall(call);
    reads.set(call, read);
  }
  return read;
}

const fullReads = new WeakMap<object, Read>();

/** The full row read as the call on screen: its name, title and status, the row's answer. */
function fullReadOf(call: ToolCallLike, full: ToolCallLike): Read {
  let read = fullReads.get(full);
  if (!read) {
    read = readCall({ ...full, name: call.name ?? full.name, title: call.title ?? full.title, status: call.status, truncated: false });
    fullReads.set(full, read);
  }
  return read;
}

/** Full rows loaded for entities, at most, per session: the most recent cut answers. */
export const MAX_FULL_ROWS = 4;

/**
 * The cut answers whose full row would name tables, topics or connections:
 * completed schema reads, foreign-key lists, topic and profile lists the page read cut, most
 * recent first, at most four. A row from a page read names its session; a
 * live one is the transcript's own, `sessionId`.
 */
export function callsToLoad(items: readonly TranscriptItem[], sessionId: string | undefined): { sessionId: string; callId: string }[] {
  const out: { sessionId: string; callId: string }[] = [];
  for (let i = items.length - 1; i >= 0 && out.length < MAX_FULL_ROWS; i--) {
    const item = items[i];
    const owner = item.sessionId ?? sessionId;
    if (!item.toolCall || !owner || !readOf(item.toolCall).cut) continue;
    out.push({ sessionId: owner, callId: item.toolCall.id });
  }
  return out;
}

const sameTable = (a: TableName, b: TableName) => a.schema === b.schema && a.table === b.table && a.profileId === b.profileId;

/** Claims `span` for `entry`; false (and the span is no one's) once another table has it. */
function claim(map: Map<string, TableName | null>, span: string, entry: TableName): boolean {
  if (!map.has(span)) {
    map.set(span, entry);
    return true;
  }
  const seen = map.get(span);
  if (seen && sameTable(seen, entry)) return true;
  map.set(span, null);
  return false;
}

/** The contribution keys a result was built from, to hand the same result back when they have not changed. */
const builtFrom = new WeakMap<SessionEntities, readonly string[]>();

/**
 * The tables and topics this session's DevKit tool calls named: tables from
 * `db.schema.get` and `db.listForeignKeys`, topics from `kafka.listTopics`
 * and the topic of a peek, describe or publish that answered; a table is on
 * the connection its call named, called what `db.listProfiles` said. `loaded` holds
 * full rows by call id for answers the page read cut (callsToLoad).
 * `previous` is the last result: while what the calls name has not changed
 * it comes back as it is, so the prose that reads it through context does not
 * re-render per token.
 */
export function sessionEntities(
  items: readonly TranscriptItem[],
  previous?: SessionEntities,
  loaded?: ReadonlyMap<string, ToolCall>,
): SessionEntities {
  const sources: Contribution[] = [];
  for (const item of items) {
    const call = item.toolCall;
    if (!call) continue;
    let read = readOf(call);
    const full = read.cut ? loaded?.get(call.id) : undefined;
    if (full) read = fullReadOf(call, full);
    if (read.contribution) sources.push(read.contribution);
  }
  if (sources.length === 0) return NO_ENTITIES;
  const keys = sources.map((source) => source.key);
  const before = previous ? builtFrom.get(previous) : undefined;
  if (previous && before && before.length === keys.length && before.every((key, i) => key === keys[i])) {
    return previous;
  }

  // Spelling → the one table it names, or null once two tables spell it the
  // same way: `a` + `b.c` and `a.b` + `c`, or `public.users` on two profiles.
  const qualified = new Map<string, TableName | null>();
  const bare = new Map<string, TableName | null>();
  const topics = new Map<string, EntityHit>();
  const profileNames = new Map<string, string>();
  for (const source of sources) {
    for (const entry of source.tables) {
      const key = `${entry.schema}.${entry.table}`;
      const seen = qualified.get(key);
      if (claim(qualified, key, entry)) {
        claim(bare, entry.table, entry);
      } else {
        if (seen) bare.set(seen.table, null);
        bare.set(entry.table, null);
      }
    }
    for (const name of source.topics) {
      if (!topics.has(name)) topics.set(name, { kind: "topic", id: name, name });
    }
    for (const { id, name } of source.profiles ?? []) profileNames.set(id, name);
  }
  const hitOf = (entry: TableName): EntityHit => {
    const profileName = entry.profileId ? profileNames.get(entry.profileId) : undefined;
    return {
      kind: "table",
      id: tableId(entry.schema, entry.table),
      name: entry.table,
      schema: entry.schema,
      table: entry.table,
      ...(entry.profileId ? { profileId: entry.profileId } : {}),
      ...(profileName ? { profileName } : {}),
    };
  };
  const tables = new Map<string, EntityHit>();
  for (const [key, entry] of qualified) {
    if (entry) tables.set(key, hitOf(entry));
  }
  for (const [name, entry] of bare) {
    // A bare name that is some other table's qualified spelling stays that one.
    const hit = entry ? tables.get(`${entry.schema}.${entry.table}`) : undefined;
    if (hit && !tables.has(name)) tables.set(name, hit);
  }
  const result: SessionEntities = { tables, topics };
  builtFrom.set(result, keys);
  return result;
}
