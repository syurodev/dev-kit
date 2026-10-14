import { describe, expect, test } from "bun:test";
import {
  parseForeignKeys,
  parsePlanResult,
  parseQueryResult,
  parseSchemaResult,
  sqlOf,
  toCsv,
} from "./parse";

describe("db.query results", () => {
  test("a read: columns, rows as text or NULL, the cap", () => {
    const parsed = parseQueryResult({
      columns: ["id", "name"],
      rows: [["1", "Ada"], ["2", null]],
      row_count: 2,
      truncated: true,
      rows_affected: -1,
    });
    expect(parsed).toEqual({
      columns: ["id", "name"],
      rows: [["1", "Ada"], ["2", null]],
      rowCount: 2,
      truncated: true,
      rowsAffected: -1,
    });
  });

  test("a write: Go's nil slices arrive as null and mean no columns", () => {
    const parsed = parseQueryResult({ columns: null, rows: null, row_count: 0, truncated: false, rows_affected: 3 });
    expect(parsed).toEqual({ columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: 3 });
  });

  test("JSON text still to parse reads the same", () => {
    const parsed = parseQueryResult(JSON.stringify({ columns: ["a"], rows: [["x"]], row_count: 1, truncated: false, rows_affected: 0 }));
    expect(parsed?.rows).toEqual([["x"]]);
  });

  test("anything else is not a query result", () => {
    expect(parseQueryResult(null)).toBeNull();
    expect(parseQueryResult("not json")).toBeNull();
    expect(parseQueryResult({ columns: "id", rows: [] })).toBeNull();
    expect(parseQueryResult({ columns: ["id"], rows: [[1]] })).toBeNull();
    expect(parseQueryResult({ columns: ["id"], rows: ["1"] })).toBeNull();
    expect(parseQueryResult({ rows: [] })).toBeNull();
  });
});

describe("db.explain results", () => {
  test("snake_case nodes become the module's PlanNode, children included", () => {
    const parsed = parsePlanResult({
      nodes: [
        {
          operation: "Hash Join",
          depth: 0,
          estimated_rows: 10,
          actual_rows: 0,
          self_millis: 0,
          detail: "cond",
          children: [{ operation: "Seq Scan", depth: 1, estimated_rows: 5, actual_rows: 0, self_millis: 0, detail: "" }],
        },
      ],
      raw: "{}",
      analyzed: false,
    });
    expect(parsed).toEqual({
      nodes: [
        {
          operation: "Hash Join",
          depth: 0,
          estimatedRows: 10,
          actualRows: 0,
          selfMillis: 0,
          detail: "cond",
          children: [
            { operation: "Seq Scan", depth: 1, estimatedRows: 5, actualRows: 0, selfMillis: 0, detail: "", children: [] },
          ],
        },
      ],
      raw: "{}",
      analyzed: false,
    });
  });

  test("a plan with nothing to draw, or a node that is not one, falls back", () => {
    expect(parsePlanResult({ nodes: [], raw: "", analyzed: false })).toBeNull();
    expect(parsePlanResult({ nodes: null, raw: "", analyzed: false })).toBeNull();
    expect(parsePlanResult({ nodes: [{ depth: 0 }], raw: "", analyzed: false })).toBeNull();
    expect(parsePlanResult({ nodes: [{ operation: "Scan", estimated_rows: "many" }], analyzed: false })).toBeNull();
    expect(parsePlanResult({ nodes: [{ operation: "Scan", children: [{}] }], analyzed: false })).toBeNull();
    expect(parsePlanResult([])).toBeNull();
  });
});

describe("db.schema.get results", () => {
  const snapshot = {
    namespace: { public: { users: ["id", "email"], orders: ["id"] }, audit: { log: [] } },
    columns: {
      public: {
        users: [
          { name: "email", data_type: "text", nullable: true, position: 2 },
          { name: "id", data_type: "bigint", nullable: false, position: 1 },
        ],
      },
    },
    indexed_at: "2026-10-14T00:00:00Z",
  };

  test("schemas and tables in name order, columns in table order", () => {
    const parsed = parseSchemaResult(snapshot);
    expect(parsed?.schemas.map((s) => s.name)).toEqual(["audit", "public"]);
    const pub = parsed!.schemas[1];
    expect(pub.tables.map((t) => t.name)).toEqual(["orders", "users"]);
    expect(pub.tables[1].columns).toEqual([
      { name: "id", dataType: "bigint", nullable: false },
      { name: "email", dataType: "text", nullable: true },
    ]);
  });

  test("a table with no column detail keeps its names", () => {
    const parsed = parseSchemaResult(snapshot);
    expect(parsed!.schemas[1].tables[0].columns).toEqual([{ name: "id", dataType: "", nullable: null }]);
    expect(parsed!.schemas[0].tables[0].columns).toEqual([]);
  });

  test("anything else is not a schema", () => {
    expect(parseSchemaResult(null)).toBeNull();
    expect(parseSchemaResult({ columns: {} })).toBeNull();
    expect(parseSchemaResult({ namespace: { public: ["users"] } })).toBeNull();
    expect(parseSchemaResult({ namespace: { public: { users: [1] } } })).toBeNull();
    expect(parseSchemaResult({ namespace: { public: { users: ["id"] } }, columns: { public: { users: [{ name: 1 }] } } })).toBeNull();
  });
});

describe("db.listForeignKeys results", () => {
  const fk = {
    schema: "public",
    table: "orders",
    column: "user_id",
    referenced_schema: "public",
    referenced_table: "users",
    referenced_column: "id",
  };

  test("the {result} envelope or a bare array", () => {
    const expected = [
      {
        schema: "public",
        table: "orders",
        column: "user_id",
        referencedSchema: "public",
        referencedTable: "users",
        referencedColumn: "id",
      },
    ];
    expect(parseForeignKeys({ result: [fk] })).toEqual(expected);
    expect(parseForeignKeys([fk])).toEqual(expected);
    expect(parseForeignKeys({ result: null })).toEqual([]);
    expect(parseForeignKeys(null)).toEqual([]);
  });

  test("a row missing a side is not a foreign key", () => {
    expect(parseForeignKeys([{ ...fk, referenced_table: undefined }])).toBeNull();
    expect(parseForeignKeys({ fks: [fk] })).toBeNull();
    expect(parseForeignKeys("nope")).toBeNull();
  });
});

describe("toCsv", () => {
  test("RFC 4180: a header line, CRLF between records, quotes only where needed", () => {
    expect(
      toCsv(
        ["id", "note"],
        [
          ["1", 'say "hi"'],
          ["2", "a,b"],
          ["3", "two\nlines"],
          ["4", null],
        ],
      ),
    ).toBe('id,note\r\n1,"say ""hi"""\r\n2,"a,b"\r\n3,"two\nlines"\r\n4,');
  });

  test("a header that needs quoting is quoted too", () => {
    expect(toCsv(["a,b"], [])).toBe('"a,b"');
  });
});

describe("sqlOf", () => {
  test("the statement the agent sent, or nothing", () => {
    expect(sqlOf({ sql: "select 1" })).toBe("select 1");
    expect(sqlOf({ profileId: "p", sql: "  select 2\n" })).toBe("select 2");
    expect(sqlOf(JSON.stringify({ sql: "select 3" }))).toBe("select 3");
    expect(sqlOf({})).toBe("");
    expect(sqlOf(null)).toBe("");
  });
});
