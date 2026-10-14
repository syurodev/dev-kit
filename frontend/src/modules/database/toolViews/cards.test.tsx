import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryResultCard } from "./QueryResultCard";
import { PlanResultCard } from "./PlanResultCard";
import { ForeignKeysResultCard, SchemaResultCard } from "./SchemaResultCard";
import type { ParsedQueryResult } from "./parse";

const bodyRows = (html: string) => (html.split("<tbody")[1]?.match(/<tr/g) ?? []).length;

function read(n: number, truncated = false): ParsedQueryResult {
  return {
    columns: ["id", "name"],
    rows: Array.from({ length: n }, (_, i) => [String(i), i === 1 ? null : `row ${i}`]),
    rowCount: n,
    truncated,
    rowsAffected: -1,
  };
}

describe("QueryResultCard", () => {
  test("header: Query, how many rows and whether the cap cut them", () => {
    const html = renderToString(<QueryResultCard result={read(3, true)} sql="select * from users" />);
    expect(html).toContain('data-testid="tool-result-card"');
    expect(html).toContain(">Query<");
    expect(html).toContain("3 rows · truncated");
  });

  test("one row is a row", () => {
    expect(renderToString(<QueryResultCard result={read(1)} sql="" />)).toContain("1 row<");
  });

  test("the first line of the SQL, the whole statement in its title", () => {
    const sql = "select id, name\nfrom users\nwhere id > 1";
    const html = renderToString(<QueryResultCard result={read(2)} sql={sql} />);
    expect(html).toContain("text-code-12");
    expect(html).toContain(">select id, name<");
    expect(html).toContain('title="select id, name\nfrom users\nwhere id &gt; 1"');
  });

  test("a table of the first 20 rows, NULL for null, and Show all for the rest", () => {
    const html = renderToString(<QueryResultCard result={read(45)} sql="select 1" />);
    expect(html).toContain('role="separator"');
    expect(bodyRows(html)).toBe(20);
    expect(html).toContain("NULL");
    expect(html).toContain("Show all 45");
  });

  test("twenty rows or fewer need no Show all", () => {
    expect(renderToString(<QueryResultCard result={read(20)} sql="select 1" />)).not.toContain("Show all");
  });

  test("Copy CSV and Open in Database", () => {
    const html = renderToString(<QueryResultCard result={read(2)} sql="select 1" />);
    expect(html).toContain("Copy CSV");
    expect(html).toContain("Open in Database");
  });

  test("a write says how many rows it changed, and has no table", () => {
    const write = { columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: 3 };
    const html = renderToString(<QueryResultCard result={write} sql="update users set x = 1" />);
    expect(html).toContain("3 rows affected");
    expect(html).not.toContain("<table");
    expect(html).not.toContain("Copy CSV");
  });

  test("a write the driver cannot count says Done", () => {
    const write = { columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: -1 };
    expect(renderToString(<QueryResultCard result={write} sql="create table t()" />)).toContain("Done");
  });

  test("the transcript's heading replaces the noun", () => {
    expect(renderToString(<QueryResultCard result={read(1)} sql="" heading="Ran query" />)).toContain(">Ran query<");
  });
});

describe("PlanResultCard", () => {
  const plan = {
    nodes: [
      {
        operation: "Seq Scan",
        depth: 0,
        estimatedRows: 10,
        actualRows: 0,
        selfMillis: 0,
        detail: "on users",
        children: [],
      },
    ],
    raw: "",
    analyzed: false,
  };

  test("a fixed-height diagram and Open in Database", () => {
    const html = renderToString(<PlanResultCard plan={plan} sql="select * from users" />);
    expect(html).toContain('data-testid="tool-result-card"');
    expect(html).toContain(">Plan<");
    expect(html).toContain("1 step");
    expect(html).toContain("h-72");
    expect(html).toContain("Open in Database");
  });

  test("without the statement there is nothing to open", () => {
    expect(renderToString(<PlanResultCard plan={plan} sql="" />)).not.toContain("Open in Database");
  });
});

describe("SchemaResultCard", () => {
  const schema = {
    schemas: [
      {
        name: "public",
        tables: [
          { name: "orders", columns: [{ name: "id", dataType: "bigint", nullable: false }] },
          {
            name: "users",
            columns: [
              { name: "id", dataType: "bigint", nullable: false },
              { name: "email", dataType: "text", nullable: true },
            ],
          },
        ],
      },
    ],
  };

  test("schemas, then their tables with a column count", () => {
    const html = renderToString(<SchemaResultCard schema={schema} />);
    expect(html).toContain(">Schema<");
    expect(html).toContain("2 tables");
    expect(html).toContain("public");
    expect(html).toContain("orders");
    expect(html).toContain("1 column");
    expect(html).toContain("2 columns");
  });

  test("tables start folded, as buttons that say so", () => {
    const html = renderToString(<SchemaResultCard schema={schema} />);
    expect(html.match(/aria-expanded="false"/g)?.length).toBe(2);
    expect(html).not.toContain("bigint");
  });

  test("an expanded table lists name, type and nullability", () => {
    const html = renderToString(<SchemaResultCard schema={schema} initiallyExpanded={["public.users"]} />);
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("email");
    expect(html).toContain("text");
    expect(html).toContain("nullable");
    expect(html).toContain("not null");
  });

  test("nothing indexed says so", () => {
    expect(renderToString(<SchemaResultCard schema={{ schemas: [] }} />)).toContain("No tables");
  });
});

describe("ForeignKeysResultCard", () => {
  test("one row per key, table.column → referenced_table.referenced_column", () => {
    const html = renderToString(
      <ForeignKeysResultCard
        keys={[
          {
            schema: "public",
            table: "orders",
            column: "user_id",
            referencedSchema: "public",
            referencedTable: "users",
            referencedColumn: "id",
          },
        ]}
      />,
    );
    expect(html).toContain(">Foreign keys<");
    expect(html).toContain("orders.user_id → users.id");
  });

  test("none declared says so", () => {
    expect(renderToString(<ForeignKeysResultCard keys={[]} />)).toContain("No foreign keys");
  });
});
