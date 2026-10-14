import { describe, expect, test } from "bun:test";
import { renderToolResult } from "@/workbench/toolResultViews";
import { hasIntent } from "@/workbench/intents";
import { toolResultInputOf } from "@/workbench/toolPayload";
import "../manifest";

const view = (name: string, rawOutput: unknown, rawInput: unknown = {}) =>
  renderToolResult({ name, rawInput, rawOutput });

describe("the database module's tool result views", () => {
  test("each of its tools has a view, by Go name and by Claude's wire name", () => {
    const query = { columns: ["a"], rows: [["1"]], row_count: 1, truncated: false, rows_affected: -1 };
    expect(view("db.query", query, { sql: "select 1" })).not.toBeNull();
    expect(view("mcp__devkit__db_query", query)).not.toBeNull();
    expect(
      view("db.explain", { nodes: [{ operation: "Seq Scan", depth: 0, detail: "" }], raw: "", analyzed: false }),
    ).not.toBeNull();
    expect(view("db.schema.get", { namespace: { public: { users: ["id"] } }, columns: {} })).not.toBeNull();
    expect(view("mcp__devkit__db_schema_get", { namespace: {} })).not.toBeNull();
    expect(view("db.listForeignKeys", [])).not.toBeNull();
  });

  test("a shape that is not theirs falls back to the generic block", () => {
    expect(view("db.query", { hello: "world" })).toBeNull();
    expect(view("db.explain", { nodes: [] })).toBeNull();
    expect(view("db.schema.get", "text")).toBeNull();
    expect(view("db.listForeignKeys", { nope: true })).toBeNull();
  });

  test("no foreign keys, as Go sends them — a nil slice in the envelope — is still the card", () => {
    const claude = toolResultInputOf({
      name: "mcp__devkit__db_listForeignKeys",
      status: "completed",
      content: [{ type: "content", content: { type: "text", text: JSON.stringify({ result: null }) } }],
    });
    expect(claude).not.toBeNull();
    expect(renderToolResult(claude!)).not.toBeNull();
    const codex = toolResultInputOf({
      title: "devkit.db.listForeignKeys",
      status: "completed",
      rawOutput: { content: [{ type: "text", text: "{}" }], structuredContent: { result: null } },
    });
    expect(codex).not.toBeNull();
    expect(renderToolResult(codex!)).not.toBeNull();
  });

  test("database.reveal is registered", () => {
    expect(hasIntent("database.reveal")).toBe(true);
  });
});
