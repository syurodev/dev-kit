import { describe, expect, test } from "bun:test";
import { buildUpdate, updateRefusal, buildStagedUpdates } from "./updateCell";

const base = {
  schema: "chiro",
  table: "ba_user",
  engine: "postgres" as string | undefined,
  keyColumns: ["id"],
};

describe("updateRefusal", () => {
  test("a keyed, writable table may be edited", () => {
    expect(updateRefusal({ ...base, readOnly: false })).toBe("");
  });

  // The one that matters: without a key there is no way to name ONE row, so an
  // UPDATE would hit every row that happens to match.
  test("no key is a refusal, not a fallback", () => {
    expect(updateRefusal({ ...base, keyColumns: null, readOnly: false })).toContain(
      "no primary key",
    );
    expect(updateRefusal({ ...base, keyColumns: [], readOnly: false })).toContain("no primary key");
  });

  test("safe mode is a refusal, and says where to turn it off", () => {
    expect(updateRefusal({ ...base, readOnly: true })).toContain("Connection options");
  });

  // A console result can be a join or an expression; there is no single table
  // behind it to write to.
  test("rows from no single table are a refusal", () => {
    expect(updateRefusal({ ...base, schema: "", readOnly: false })).toContain("single table");
  });
});

describe("buildUpdate", () => {
  test("one cell", () => {
    expect(
      buildUpdate({ ...base, rows: [{ values: ["7"] }], columns: ["name"], value: "ada" }),
    ).toBe(`UPDATE "chiro"."ba_user" SET "name" = 'ada' WHERE "id" = '7'`);
  });

  // The bulk case, and the reason it is one statement: a single intention is a
  // single thing to undo, and cannot end up half-applied.
  test("many rows are one statement", () => {
    const sql = buildUpdate({
      ...base,
      rows: [{ values: ["1"] }, { values: ["2"] }, { values: ["3"] }],
      columns: ["status"],
      value: "open",
    });
    expect(sql.match(/UPDATE/g)).toHaveLength(1);
    expect(sql).toBe(
      `UPDATE "chiro"."ba_user" SET "status" = 'open' WHERE "id" = '1' OR "id" = '2' OR "id" = '3'`,
    );
  });

  test("many columns are one SET list", () => {
    expect(
      buildUpdate({ ...base, rows: [{ values: ["1"] }], columns: ["a", "b"], value: "x" }),
    ).toBe(`UPDATE "chiro"."ba_user" SET "a" = 'x', "b" = 'x' WHERE "id" = '1'`);
  });

  // NULL is a value, not the four letters — typing NULL into a cell sets the
  // string, which is what DataGrip does too.
  test("null sets NULL, and a typed \"NULL\" sets the text", () => {
    expect(
      buildUpdate({ ...base, rows: [{ values: ["1"] }], columns: ["note"], value: null }),
    ).toContain(`SET "note" = NULL`);
    expect(
      buildUpdate({ ...base, rows: [{ values: ["1"] }], columns: ["note"], value: "NULL" }),
    ).toContain(`SET "note" = 'NULL'`);
  });

  // = NULL matches nothing, so a row keyed on a NULL would be updated silently
  // not at all. (A key column should never be nullable; a unique index over a
  // nullable column is exactly why pageKeyFor checks.)
  test("a NULL key value compares with IS NULL", () => {
    expect(
      buildUpdate({
        ...base,
        keyColumns: ["tenant", "id"],
        rows: [{ values: [null, "9"] }],
        columns: ["note"],
        value: "x",
      }),
    ).toBe(
      `UPDATE "chiro"."ba_user" SET "note" = 'x' WHERE ("tenant" IS NULL AND "id" = '9')`,
    );
  });

  test("a composite key brackets each row so OR cannot split it", () => {
    const sql = buildUpdate({
      ...base,
      keyColumns: ["tenant", "id"],
      rows: [{ values: ["t1", "1"] }, { values: ["t2", "2"] }],
      columns: ["note"],
      value: "x",
    });
    expect(sql).toContain(`("tenant" = 't1' AND "id" = '1') OR ("tenant" = 't2' AND "id" = '2')`);
  });

  test("MySQL gets backticks and its own escaping", () => {
    expect(
      buildUpdate({
        ...base,
        engine: "mysql",
        rows: [{ values: ["1"] }],
        columns: ["name"],
        value: "O'Brien",
      }),
    ).toBe("UPDATE `chiro`.`ba_user` SET `name` = 'O''Brien' WHERE `id` = '1'");
  });

  test("nothing to change is a programming error, not an empty statement", () => {
    expect(() => buildUpdate({ ...base, rows: [], columns: ["a"], value: "x" })).toThrow();
    expect(() => buildUpdate({ ...base, rows: [{ values: ["1"] }], columns: [], value: "x" })).toThrow();
  });
});

// #52. Staged cells become one statement each, keyed by the row as it loaded,
// and a change to the key itself runs after the row's other changes.
test("staged changes become statements, key changes last in their row", () => {
  const statements = buildStagedUpdates({
    schema: "public",
    table: "users",
    engine: "postgres",
    keyColumns: ["id"],
    columns: ["id", "name", "email"],
    rows: [
      ["1", "ann", null],
      ["2", "bob", "b@x"],
    ],
    staged: { "0:0": "7", "0:1": "anna", "1:2": null },
  });
  expect(statements).toEqual([
    `UPDATE "public"."users" SET "name" = 'anna' WHERE "id" = '1'`,
    `UPDATE "public"."users" SET "id" = '7' WHERE "id" = '1'`,
    `UPDATE "public"."users" SET "email" = NULL WHERE "id" = '2'`,
  ]);
});

