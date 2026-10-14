import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryResultCard } from "./QueryResultCard";
import { PlanResultCard } from "./PlanResultCard";
import { ForeignKeysResultCard, SchemaResultCard } from "./SchemaResultCard";
import type { ParsedQueryResult } from "./parse";
import { tableKey } from "./lists";

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

  test("columns are measured over every returned row, so Show all does not move them", () => {
    const wide = read(30);
    wide.rows[25] = ["25", "a value far wider than any of the first twenty rows hold"];
    const folded = renderToString(<QueryResultCard result={wide} sql="select 1" />);
    const widths = (html: string) => html.match(/<col style="width:[0-9.]+px"/g);
    const narrow = renderToString(<QueryResultCard result={read(30)} sql="select 1" />);
    expect(widths(folded)).not.toEqual(widths(narrow));
  });

  test("a write says how many rows it changed, has no table, and nothing to open", () => {
    const write = { columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: 3 };
    const html = renderToString(<QueryResultCard result={write} sql="update users set x = 1" />);
    expect(html).toContain("3 rows affected");
    expect(html).not.toContain("<table");
    expect(html).not.toContain("Copy CSV");
    expect(html).not.toContain("Open in Database");
  });

  test("one row affected is one row", () => {
    const write = { columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: 1 };
    expect(renderToString(<QueryResultCard result={write} sql="delete from t where id = 1" />)).toContain(
      "1 row affected",
    );
  });

  test("DDL's 0 is Done, not a failure", () => {
    const write = { columns: [], rows: [], rowCount: 0, truncated: false, rowsAffected: 0 };
    const html = renderToString(<QueryResultCard result={write} sql="create table t (id int)" />);
    expect(html).toContain("Done");
    expect(html).not.toContain("0 rows affected");
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
    const html = renderToString(<PlanResultCard plan={plan} sql="select * from users" profileId="p1" />);
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
    const html = renderToString(<SchemaResultCard schema={schema} initiallyExpanded={[tableKey("public", "users")]} />);
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("email");
    expect(html).toContain("text");
    expect(html).toContain("nullable");
    expect(html).toContain("not null");
  });

  const many = (n: number) => ({
    schemas: [
      {
        name: "public",
        tables: Array.from({ length: n }, (_, i) => ({ name: `t${String(i).padStart(3, "0")}`, columns: [] })),
      },
    ],
  });

  test("a long schema lists the first 40 tables and offers the rest", () => {
    const html = renderToString(<SchemaResultCard schema={many(45)} />);
    expect(html.match(/aria-expanded=/g)?.length).toBe(40);
    expect(html).toContain("Show all 45");
  });

  test("opened, it lists them all and offers the way back", () => {
    const html = renderToString(<SchemaResultCard schema={many(45)} initiallyAll />);
    expect(html.match(/aria-expanded=/g)?.length).toBe(45);
    expect(html).toContain("Show fewer");
  });

  test("past 500 it says how many it left out", () => {
    const html = renderToString(<SchemaResultCard schema={many(520)} initiallyAll />);
    expect(html.match(/aria-expanded=/g)?.length).toBe(500);
    expect(html).toContain("+20 more not shown");
  });

  test("its toggles draw the ring focus style", () => {
    const html = renderToString(<SchemaResultCard schema={many(45)} />);
    expect(html).toContain("focus-visible:ring-1");
    expect(html).not.toContain("focus-visible:bg-(--state-soft) focus-visible:outline-none\"");
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

  const key = (i: number, referencedSchema = "public") => ({
    schema: "public",
    table: `t${i}`,
    column: "user_id",
    referencedSchema,
    referencedTable: "users",
    referencedColumn: "id",
  });

  test("a key into another schema names both schemas", () => {
    const html = renderToString(<ForeignKeysResultCard keys={[key(1, "auth")]} />);
    expect(html).toContain("public → auth");
  });

  test("a key inside one schema has no second line", () => {
    expect(renderToString(<ForeignKeysResultCard keys={[key(1)]} />)).not.toContain("public → public");
  });

  test("a long list folds to 40, opens to all, and folds back", () => {
    const keys = Array.from({ length: 45 }, (_, i) => key(i));
    const folded = renderToString(<ForeignKeysResultCard keys={keys} />);
    expect(folded.match(/data-slot="framed-card-row"/g)?.length).toBe(40);
    expect(folded).toContain("Show all 45");
    const open = renderToString(<ForeignKeysResultCard keys={keys} initiallyAll />);
    expect(open.match(/data-slot="framed-card-row"/g)?.length).toBe(45);
    expect(open).toContain("Show fewer");
  });

  test("the same key twice is two rows", () => {
    const html = renderToString(<ForeignKeysResultCard keys={[key(1), key(1)]} />);
    expect(html.match(/data-slot="framed-card-row"/g)?.length).toBe(2);
  });

  test("none declared says so", () => {
    expect(renderToString(<ForeignKeysResultCard keys={[]} />)).toContain("No foreign keys");
  });
});
