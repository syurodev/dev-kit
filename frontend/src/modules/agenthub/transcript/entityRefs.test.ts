import { describe, expect, test } from "bun:test";
import type { ToolCall, TranscriptItem } from "../types";
import { entityOf, NO_ENTITIES, sessionEntities, tableId } from "./entityRefs";

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

describe("tableId", () => {
  test("schema.table, the schema quoted when it holds a dot or starts with a quote", () => {
    expect(tableId("public", "orders")).toBe("public.orders");
    expect(tableId("sales.eu", "orders")).toBe('"sales.eu".orders');
    expect(tableId('a"b.c', "t")).toBe('"a""b.c".t');
    expect(tableId('"x', "t")).toBe('"""x".t');
  });
});
