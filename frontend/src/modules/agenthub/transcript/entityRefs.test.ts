import { describe, expect, test } from "bun:test";
import type { ToolCall, TranscriptItem } from "../types";
import { callsToLoad, entityOf, MAX_FULL_ROWS, NO_ENTITIES, sessionEntities, tableId } from "./entityRefs";

const claudeText = (text: string) => [{ type: "content", content: { type: "text", text } }];

const item = (call: ToolCall): TranscriptItem => ({ id: call.id, kind: "tool_call", toolCall: call });

/** Claude: the wire name, the result as JSON text in content. */
const claude = (id: string, tool: string, output: unknown, rawInput?: unknown, status = "completed"): TranscriptItem =>
  item({
    id,
    name: "mcp__devkit__" + tool.split(".").join("_"),
    status,
    rawInput,
    content: claudeText(JSON.stringify(output)),
  });

/** Codex: no name, the title `devkit.<tool>`, the MCP result in rawOutput. */
const codex = (id: string, tool: string, output: unknown, rawInput?: unknown): TranscriptItem =>
  item({
    id,
    title: `devkit.${tool}`,
    status: "completed",
    rawInput,
    rawOutput: { content: [{ type: "text", text: JSON.stringify(output) }], structuredContent: output },
  });

const schema = {
  namespace: { public: { orders: ["id"], users: ["id"] }, archive: { orders: ["id"], invoices: ["id"] } },
  columns: {},
  indexed_at: "2026-10-14T00:00:00Z",
};

describe("sessionEntities", () => {
  test("tables from db.schema.get, qualified, and bare when one schema has the name", () => {
    const entities = sessionEntities([claude("s", "db.schema.get", schema)]);
    expect(entityOf(entities, "public.orders")).toMatchObject({ kind: "table", schema: "public", table: "orders", id: "public.orders" });
    expect(entityOf(entities, "archive.orders")).toMatchObject({ kind: "table", id: "archive.orders" });
    expect(entityOf(entities, "users")).toMatchObject({ kind: "table", schema: "public", table: "users", id: "public.users" });
    expect(entityOf(entities, "invoices")).toMatchObject({ id: "archive.invoices" });
    // In two schemas: which one is meant is a guess.
    expect(entityOf(entities, "orders")).toBeNull();
    expect(entityOf(entities, "payments")).toBeNull();
  });

  test("tables from db.listForeignKeys, both ends, wrapped in {result} or not", () => {
    const fk = { schema: "sales", table: "lines", column: "order_id", referenced_schema: "sales", referenced_table: "orders", referenced_column: "id" };
    for (const output of [{ result: [fk] }, [fk]]) {
      const entities = sessionEntities([claude("fk", "db.listForeignKeys", output)]);
      expect(entityOf(entities, "sales.lines")).toMatchObject({ kind: "table" });
      expect(entityOf(entities, "orders")).toMatchObject({ id: "sales.orders" });
    }
  });

  test("topics from kafka.listTopics and the topic of peek, describe and publish", () => {
    const entities = sessionEntities([
      codex("t", "kafka.listTopics", { result: [{ name: "orders.created" }] }),
      claude("p", "kafka.peekMessages", { result: [] }, { topic: "payments", partition: 0 }),
      codex("d", "kafka.describeTopic", { name: "audit", replication_factor: 1, partitions: [{ id: 0 }] }, { topic: "audit" }),
      claude("w", "kafka.publishMessage", { partition: 0, offset: 4 }, { topic: "emails", value: "x" }),
    ]);
    for (const name of ["orders.created", "payments", "audit", "emails"]) {
      expect(entityOf(entities, name)).toEqual({ kind: "topic", id: name, name });
    }
  });

  test("a call that failed or has no answer yet names nothing", () => {
    const entities = sessionEntities([
      claude("p", "kafka.peekMessages", "```\nkafka.unknownTopic: no such topic\n```", { topic: "ghost", partition: 0 }, "failed"),
      item({ id: "live", name: "mcp__devkit__kafka_describeTopic", status: "in_progress", rawInput: { topic: "pending" } }),
    ]);
    expect(entityOf(entities, "ghost")).toBeNull();
    expect(entityOf(entities, "pending")).toBeNull();
  });

  test("other tools and other shapes add nothing", () => {
    const entities = sessionEntities([
      claude("q", "db.query", { columns: ["name"], rows: [["orders"]] }, { sql: "select * from orders" }),
      item({ id: "bash", name: "Bash", status: "completed", rawInput: { topic: "nope" }, rawOutput: "ok" }),
      claude("bad", "db.schema.get", { namespace: "nope" }),
    ]);
    expect(entities).toBe(NO_ENTITIES);
  });

  test("a name that is both a table and a topic is neither", () => {
    const entities = sessionEntities([
      claude("s", "db.schema.get", schema),
      claude("t", "kafka.listTopics", { result: [{ name: "users" }] }),
    ]);
    expect(entityOf(entities, "users")).toBeNull();
    expect(entityOf(entities, "public.users")).toMatchObject({ kind: "table" });
  });

  test("the same calls give back the same answer, so prose does not re-render", () => {
    const items = [claude("s", "db.schema.get", schema), claude("t", "kafka.listTopics", { result: [{ name: "a" }] })];
    const first = sessionEntities(items);
    const streamed: TranscriptItem[] = [...items, { id: "m", kind: "agent_message_chunk", text: "hi" }];
    expect(sessionEntities(streamed, first)).toBe(first);
    const more = [...streamed, claude("t2", "kafka.listTopics", { result: [{ name: "b" }] })];
    const next = sessionEntities(more, first);
    expect(next).not.toBe(first);
    expect(entityOf(next, "b")).toMatchObject({ kind: "topic" });
  });

  test("each call's payload is read once, not on every publish", () => {
    let reads = 0;
    const call: ToolCall = { id: "t", name: "mcp__devkit__kafka_listTopics", status: "completed", content: [] };
    Object.defineProperty(call, "content", {
      get() {
        reads += 1;
        return claudeText(JSON.stringify({ result: [{ name: "a" }] }));
      },
    });
    const first = sessionEntities([item(call)]);
    const afterFirst = reads;
    sessionEntities([item(call), { id: "m", kind: "agent_message_chunk", text: "x" }], first);
    expect(reads).toBe(afterFirst);
  });
});

/** A call the page read cut: its raw output is the first bytes of its JSON. */
const cut = (id: string, tool: string, rawInput?: unknown, sessionId?: string): TranscriptItem => ({
  id,
  kind: "tool_call",
  sessionId,
  toolCall: {
    id,
    name: "mcp__devkit__" + tool.split(".").join("_"),
    status: "completed",
    truncated: true,
    rawInput,
    rawOutput: '{"namespace":{"public":{"ord',
  },
});

describe("what a call answered", () => {
  test("a cut peek still names its topic: the topic is in the input, not the answer", () => {
    const entities = sessionEntities([cut("p", "kafka.peekMessages", { topic: "payments", partition: 0 })]);
    expect(entityOf(entities, "payments")).toMatchObject({ kind: "topic" });
  });

  test("a completed call that answered with an error names nothing", () => {
    // Claude fences a DevKit AppError in content, the call itself marked completed.
    const entities = sessionEntities([
      item({
        id: "p",
        name: "mcp__devkit__kafka_describeTopic",
        status: "completed",
        rawInput: { topic: "ghost" },
        content: claudeText("```\nkafka.unknownTopic: no such topic\n```"),
      }),
    ]);
    expect(entityOf(entities, "ghost")).toBeNull();
  });

  test("a call replaced by its completed self goes from nothing to its topic", () => {
    const running: ToolCall = { id: "d", name: "mcp__devkit__kafka_describeTopic", status: "in_progress", rawInput: { topic: "audit" } };
    const first = sessionEntities([item(running)]);
    expect(entityOf(first, "audit")).toBeNull();
    const done = sessionEntities([item({ ...running, status: "completed", content: claudeText('{"name":"audit"}') })], first);
    expect(entityOf(done, "audit")).toMatchObject({ kind: "topic" });
  });

  test("an update that names nothing new keeps the same answer", () => {
    const call = claude("t", "kafka.listTopics", { result: [{ name: "a" }] });
    const first = sessionEntities([call]);
    const again = claude("t", "kafka.listTopics", { result: [{ name: "a" }] });
    expect(again.toolCall).not.toBe(call.toolCall);
    expect(sessionEntities([again], first)).toBe(first);
  });
});

describe("full rows behind cut answers", () => {
  test("a cut schema read names nothing until its full row is handed in", () => {
    const items = [cut("s", "db.schema.get", undefined, "s1")];
    expect(sessionEntities(items)).toBe(NO_ENTITIES);
    const full: ToolCall = { id: "s", status: "completed", content: claudeText(JSON.stringify(schema)) };
    const loaded = new Map([["s", full]]);
    const entities = sessionEntities(items, undefined, loaded);
    expect(entityOf(entities, "public.users")).toMatchObject({ kind: "table" });
    // A new map holding the same rows, on a later publish: the same answer.
    expect(sessionEntities([...items, { id: "m", kind: "agent_message_chunk", text: "x" }], entities, new Map(loaded))).toBe(entities);
  });

  test("callsToLoad: only cut answers of tools that read them, the four most recent", () => {
    const items: TranscriptItem[] = [
      ...Array.from({ length: 6 }, (_, i) => cut(`s${i}`, i % 2 ? "db.listForeignKeys" : "db.schema.get", undefined, "s1")),
      cut("lt", "kafka.listTopics"),
      // The topic is in the input: nothing to load.
      cut("p", "kafka.peekMessages", { topic: "x" }, "s1"),
      // Whole: nothing to load.
      claude("whole", "db.schema.get", schema),
      { ...cut("f", "db.schema.get", undefined, "s1"), toolCall: { ...cut("f", "db.schema.get").toolCall!, status: "failed" } },
      { ...cut("q", "db.query", undefined, "s1") },
    ];
    expect(MAX_FULL_ROWS).toBe(4);
    expect(callsToLoad(items, "fallback")).toEqual([
      { sessionId: "fallback", callId: "lt" },
      { sessionId: "s1", callId: "s5" },
      { sessionId: "s1", callId: "s4" },
      { sessionId: "s1", callId: "s3" },
    ]);
    // With no session to ask, nothing.
    expect(callsToLoad([cut("lt", "kafka.listTopics")], undefined)).toEqual([]);
  });
});

describe("names that collide", () => {
  test("two tables spelled the same qualified way are neither, nor are their bare names", () => {
    const entities = sessionEntities([
      claude("s", "db.schema.get", { namespace: { a: { "b.c": ["id"] }, "a.b": { c: ["id"] }, x: { y: ["id"] } } }),
    ]);
    expect(entityOf(entities, "a.b.c")).toBeNull();
    expect(entityOf(entities, "b.c")).toBeNull();
    expect(entityOf(entities, "c")).toBeNull();
    expect(entityOf(entities, "y")).toMatchObject({ id: "x.y" });
  });
});

describe("tables belong to the connection that read them", () => {
  const staging = { profileId: "p-staging" };
  const prod = { profileId: "p-prod" };
  const fk = (schema: string, table: string) => ({
    result: [{ schema, table, column: "x", referenced_schema: schema, referenced_table: table, referenced_column: "id" }],
  });

  test("a table carries the profile its call named, from either vendor", () => {
    for (const make of [claude, codex]) {
      const entities = sessionEntities([make("s", "db.schema.get", schema, staging)]);
      expect(entityOf(entities, "public.users")).toMatchObject({ kind: "table", id: "public.users", profileId: "p-staging" });
      expect(entityOf(entities, "users")).toMatchObject({ profileId: "p-staging" });
    }
    const keys = sessionEntities([claude("fk", "db.listForeignKeys", fk("sales", "lines"), prod)]);
    expect(entityOf(keys, "sales.lines")).toMatchObject({ profileId: "p-prod" });
  });

  test("the same table under two profiles is neither, qualified or bare", () => {
    const entities = sessionEntities([
      claude("a", "db.schema.get", { namespace: { public: { users: ["id"], audit: ["id"] } } }, staging),
      codex("b", "db.listForeignKeys", fk("public", "users"), prod),
    ]);
    expect(entityOf(entities, "public.users")).toBeNull();
    expect(entityOf(entities, "users")).toBeNull();
    // Only one profile has it: it opens there.
    expect(entityOf(entities, "public.audit")).toMatchObject({ profileId: "p-staging" });
    expect(entityOf(entities, "audit")).toMatchObject({ profileId: "p-staging" });
  });

  test("the same table read twice on one profile is still that table", () => {
    const entities = sessionEntities([
      claude("a", "db.schema.get", { namespace: { public: { users: ["id"] } } }, staging),
      claude("b", "db.listForeignKeys", fk("public", "users"), staging),
    ]);
    expect(entityOf(entities, "public.users")).toMatchObject({ profileId: "p-staging" });
    expect(entityOf(entities, "users")).toMatchObject({ profileId: "p-staging" });
  });

  test("a profile's name comes from the session's own db.listProfiles answer", () => {
    const entities = sessionEntities([
      codex("l", "db.listProfiles", { result: [{ id: "p-staging", name: "Staging" }, { id: "p-prod", name: "Prod" }] }),
      claude("s", "db.schema.get", { namespace: { public: { users: ["id"] } } }, staging),
    ]);
    expect(entityOf(entities, "users")).toMatchObject({ profileId: "p-staging", profileName: "Staging" });
    // Without a list, no name: nothing is fetched to find one.
    const unnamed = sessionEntities([claude("s", "db.schema.get", { namespace: { public: { users: ["id"] } } }, staging)]);
    expect(entityOf(unnamed, "users")).not.toHaveProperty("profileName");
  });

  test("the same names on another profile are a new answer", () => {
    const first = sessionEntities([claude("s", "db.schema.get", schema, staging)]);
    const moved = sessionEntities([claude("s", "db.schema.get", schema, prod)], first);
    expect(moved).not.toBe(first);
    expect(entityOf(moved, "public.users")).toMatchObject({ profileId: "p-prod" });
  });
});

describe("tableId", () => {
  test("schema.table, the schema quoted when it holds a dot or starts with a quote", () => {
    expect(tableId("public", "orders")).toBe("public.orders");
    expect(tableId("sales.eu", "orders")).toBe('"sales.eu".orders');
    expect(tableId('a"b.c', "t")).toBe('"a""b.c".t');
    expect(tableId('"x', "t")).toBe('"""x".t');
  });
});
