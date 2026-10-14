import { toolPayloadOf, type ToolCallLike } from "@/workbench/toolPayload";
import { createToolRegistry } from "@/workbench/toolResultViews";
import type { TranscriptItem } from "../types";

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

/** What one call contributes. */
type Contribution = { tables: readonly { schema: string; table: string }[]; topics: readonly string[] };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const nonEmpty = (value: unknown): value is string => typeof value === "string" && value !== "";

/** A `{result: [...]}` payload, whether or not the payload layer already unwrapped it. */
function rows(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  return isRecord(payload) && Array.isArray(payload.result) ? payload.result : [];
}

function topicInput(rawInput: unknown): Contribution {
  const topic = isRecord(rawInput) ? rawInput.topic : undefined;
  return { tables: [], topics: nonEmpty(topic) ? [topic] : [] };
}

type Reader = (payload: unknown, rawInput: unknown) => Contribution;

/** By the tool's Go name; found again from Claude's wire form or Codex's title (toolResultViews). */
const readers = createToolRegistry<Reader>();

// `{namespace: {schema: {table: [col]}}, columns, indexed_at}`
readers.register("db.schema.get", (payload) => {
  const tables: { schema: string; table: string }[] = [];
  const namespace = isRecord(payload) ? payload.namespace : undefined;
  if (isRecord(namespace)) {
    for (const [schema, byTable] of Object.entries(namespace)) {
      if (!schema || !isRecord(byTable)) continue;
      for (const table of Object.keys(byTable)) if (table) tables.push({ schema, table });
    }
  }
  return { tables, topics: [] };
});

// `{result: [{schema, table, column, referenced_schema, referenced_table, referenced_column}]}`: both ends.
readers.register("db.listForeignKeys", (payload) => {
  const tables: { schema: string; table: string }[] = [];
  for (const row of rows(payload)) {
    if (!isRecord(row)) continue;
    if (nonEmpty(row.schema) && nonEmpty(row.table)) tables.push({ schema: row.schema, table: row.table });
    if (nonEmpty(row.referenced_schema) && nonEmpty(row.referenced_table)) {
      tables.push({ schema: row.referenced_schema, table: row.referenced_table });
    }
  }
  return { tables, topics: [] };
});

// `{result: [{name}]}`
readers.register("kafka.listTopics", (payload) => ({
  tables: [],
  topics: rows(payload)
    .map((row) => (isRecord(row) ? row.name : row))
    .filter(nonEmpty),
}));

// A topic the agent peeked, described or published to — and that answered.
for (const tool of ["kafka.peekMessages", "kafka.describeTopic", "kafka.publishMessage"]) {
  readers.register(tool, (_payload, rawInput) => topicInput(rawInput));
}

/** The tool's name as the registry takes it: Claude's wire form, a Go name, or Codex's `devkit.<tool>` title. */
function lookupName(call: ToolCallLike): string | undefined {
  if (call.name) return call.name;
  return call.title?.startsWith("devkit.") ? call.title.slice("devkit.".length) : undefined;
}

/**
 * Per call object, read once: a transcript publishes a new items array on
 * every streamed token, and a long session must not re-parse every tool
 * payload each time. A call that changes (an update lands) is a new object.
 */
const contributions = new WeakMap<object, Contribution | null>();

function contributionOf(call: ToolCallLike): Contribution | null {
  if (contributions.has(call)) return contributions.get(call) ?? null;
  let found: Contribution | null = null;
  const reader = readers.lookup(lookupName(call));
  if (reader) {
    // Only an answer counts: a topic a failed peek named may not exist.
    const result = toolPayloadOf(call);
    if (result && "payload" in result) {
      const read = reader(result.payload, call.rawInput);
      if (read.tables.length > 0 || read.topics.length > 0) found = read;
    }
  }
  contributions.set(call, found);
  return found;
}

/** The contributions a result was built from, to hand the same result back when they have not changed. */
const builtFrom = new WeakMap<SessionEntities, readonly Contribution[]>();

/**
 * The tables and topics this session's DevKit tool calls named: tables from
 * `db.schema.get` and `db.listForeignKeys`, topics from `kafka.listTopics`
 * and the topic of a peek, describe or publish that answered. `previous` is
 * the last result: while no call has changed it comes back as it is, so the
 * prose that reads it through context does not re-render per token.
 *
 * A call the page read cut to a preview names nothing (toolPayloadOf has no
 * payload for it); its full row is not fetched for this.
 */
export function sessionEntities(items: readonly TranscriptItem[], previous?: SessionEntities): SessionEntities {
  const sources: Contribution[] = [];
  for (const item of items) {
    const contribution = item.toolCall ? contributionOf(item.toolCall) : null;
    if (contribution) sources.push(contribution);
  }
  if (sources.length === 0) return NO_ENTITIES;
  const before = previous ? builtFrom.get(previous) : undefined;
  if (previous && before && before.length === sources.length && before.every((source, i) => source === sources[i])) {
    return previous;
  }

  const qualified = new Map<string, EntityHit>();
  // Bare name → the qualified key, or null once a second schema has it too.
  const bare = new Map<string, string | null>();
  const topics = new Map<string, EntityHit>();
  for (const source of sources) {
    for (const { schema, table } of source.tables) {
      const key = `${schema}.${table}`;
      if (qualified.has(key)) continue;
      qualified.set(key, { kind: "table", id: tableId(schema, table), name: table, schema, table });
      bare.set(table, bare.has(table) ? null : key);
    }
    for (const name of source.topics) {
      if (!topics.has(name)) topics.set(name, { kind: "topic", id: name, name });
    }
  }
  const tables = new Map(qualified);
  for (const [name, key] of bare) {
    // A bare name that is some other table's qualified spelling stays that one.
    if (key && !tables.has(name)) tables.set(name, qualified.get(key)!);
  }
  const result: SessionEntities = { tables, topics };
  builtFrom.set(result, sources);
  return result;
}
