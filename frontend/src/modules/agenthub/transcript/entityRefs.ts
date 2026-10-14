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
    }
  | { kind: "topic"; id: string; name: string };

export type SessionEntities = {
  /** `schema.table` always; the bare `table` too when no other schema has one by that name. */
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
type Contribution = { tables: readonly { schema: string; table: string }[]; topics: readonly string[]; key: string };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const nonEmpty = (value: unknown): value is string => typeof value === "string" && value !== "";

/** A `{result: [...]}` payload, whether or not the payload layer already unwrapped it. */
function rows(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  return isRecord(payload) && Array.isArray(payload.result) ? payload.result : [];
}

type Found = { tables: { schema: string; table: string }[]; topics: string[] };

/**
 * How a tool's calls name things. A payload reader parses the answer, so a
 * cut answer names nothing until its full row is loaded; an input reader
 * needs only to know the call answered without an error.
 */
type Reader = { payload: true; read: (payload: unknown) => Found } | { payload: false; read: (rawInput: unknown) => Found };

/** By the tool's Go name; found again from Claude's wire form or Codex's title (toolResultViews). */
const readers = createToolRegistry<Reader>();

// `{namespace: {schema: {table: [col]}}, columns, indexed_at}`
readers.register("db.schema.get", {
  payload: true,
  read: (payload) => {
    const tables: { schema: string; table: string }[] = [];
    const namespace = isRecord(payload) ? payload.namespace : undefined;
    if (isRecord(namespace)) {
      for (const [schema, byTable] of Object.entries(namespace)) {
        if (!schema || !isRecord(byTable)) continue;
        for (const table of Object.keys(byTable)) if (table) tables.push({ schema, table });
      }
    }
    return { tables, topics: [] };
  },
});

// `{result: [{schema, table, column, referenced_schema, referenced_table, referenced_column}]}`: both ends.
readers.register("db.listForeignKeys", {
  payload: true,
  read: (payload) => {
    const tables: { schema: string; table: string }[] = [];
    for (const row of rows(payload)) {
      if (!isRecord(row)) continue;
      if (nonEmpty(row.schema) && nonEmpty(row.table)) tables.push({ schema: row.schema, table: row.table });
      if (nonEmpty(row.referenced_schema) && nonEmpty(row.referenced_table)) {
        tables.push({ schema: row.referenced_schema, table: row.referenced_table });
      }
    }
    return { tables, topics: [] };
  },
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
  return `${found.tables.map((t) => `${t.schema}\u0000${t.table}`).join("\u0001")}\u0002${found.topics.join("\u0001")}`;
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
    found = reader.read(result.payload);
  } else {
    found = reader.read(call.rawInput);
  }
  if (found.tables.length === 0 && found.topics.length === 0) return { contribution: null, cut: false };
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
 * The cut answers whose full row would name tables or topics: completed
 * schema reads, foreign-key lists and topic lists the page read cut, most
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

/** The contribution keys a result was built from, to hand the same result back when they have not changed. */
const builtFrom = new WeakMap<SessionEntities, readonly string[]>();

/**
 * The tables and topics this session's DevKit tool calls named: tables from
 * `db.schema.get` and `db.listForeignKeys`, topics from `kafka.listTopics`
 * and the topic of a peek, describe or publish that answered. `loaded` holds
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

  // Qualified spelling → the one table it names, or null once two tables
  // spell it the same way (`a` + `b.c` and `a.b` + `c`).
  const qualified = new Map<string, { schema: string; table: string } | null>();
  // Bare name → the qualified key, or null once a second table has it too.
  const bare = new Map<string, string | null>();
  const topics = new Map<string, EntityHit>();
  for (const source of sources) {
    for (const { schema, table } of source.tables) {
      const key = `${schema}.${table}`;
      if (!qualified.has(key)) {
        qualified.set(key, { schema, table });
        bare.set(table, bare.has(table) ? null : key);
        continue;
      }
      const seen = qualified.get(key);
      if (seen && (seen.schema !== schema || seen.table !== table)) {
        qualified.set(key, null);
        bare.set(seen.table, null);
        bare.set(table, null);
      } else if (seen === null) {
        bare.set(table, null);
      }
    }
    for (const name of source.topics) {
      if (!topics.has(name)) topics.set(name, { kind: "topic", id: name, name });
    }
  }
  const tables = new Map<string, EntityHit>();
  for (const [key, entry] of qualified) {
    if (entry) tables.set(key, { kind: "table", id: tableId(entry.schema, entry.table), name: entry.table, ...entry });
  }
  for (const [name, key] of bare) {
    // A bare name that is some other table's qualified spelling stays that one.
    const hit = key ? tables.get(key) : undefined;
    if (hit && !tables.has(name)) tables.set(name, hit);
  }
  const result: SessionEntities = { tables, topics };
  builtFrom.set(result, keys);
  return result;
}
