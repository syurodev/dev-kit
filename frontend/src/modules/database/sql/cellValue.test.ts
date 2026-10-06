import { expect, test } from "bun:test";
import {
  cellKind,
  validateCellValue,
  booleanValue,
  splitOffset,
  sameCellValue,
} from "./cellValue";
import { bulkEditRefusal } from "../grid/bulkEditLimits";

test.each([
  ["boolean", "boolean"],
  ["tinyint(1)", "boolean"],
  ["tinyint(1) unsigned", "boolean"],
  ["bit(1)", "text"],
  ["int8", "integer"],
  ["numeric(30,10)", "number"],
  ["timestamp(6) with time zone", "datetime-zone"],
  ["timestamp without time zone", "datetime"],
  ["datetime(6)", "datetime"],
  ["time with time zone", "time-zone"],
  ["date", "date"],
  ["jsonb", "json"],
  ["uuid", "uuid"],
  ["boolean[]", "text"],
  ["custom_domain", "text"],
])("%s uses %s controls", (type, kind) =>
  expect(cellKind(type)).toBe(kind as never),
);
test.each(["true", "t", "1", "false", "f", "0"])(
  "boolean %s normalises without a numeric/string SQL cast",
  (v) => expect(["true", "false"]).toContain(booleanValue(v)),
);
test.each([
  ["date", "2024-02-29", true],
  ["date", "2023-02-29", false],
  ["date", "2026-04-31", false],
  ["time", "12:34:56.123456", true],
  ["time", "12:60:00", false],
  ["time", "24:00:00", true],
  ["time", "24:00:01", false],
  ["timestamp", "2026-10-05T12:30:01.123456", true],
  ["timestamp", "2026-10-05 12:30:01", true],
  ["timestamptz", "2026-10-05T12:30:01.123456+07:00", true],
  ["timestamptz", "2026-10-05T12:30:00Z", true],
  ["timestamptz", "2026-10-05T12:30:00", false],
  ["timetz", "12:30:01+0730", true],
  ["numeric", "9007199254740993.123456789", true],
  ["bigint", "9007199254740993", true],
  ["bigint", "1.5", false],
  ["numeric", "1e-30", true],
  ["numeric", "1; DROP TABLE users", false],
  ["jsonb", '{"id":9007199254740993}', true],
  ["jsonb", "{broken}", false],
  ["uuid", "550e8400-e29b-41d4-a716-446655440000", true],
])("validates %s %s", (type, value, valid) =>
  expect(validateCellValue(value as string, type as string) === "").toBe(
    valid as boolean,
  ),
);
test("NULL differs from empty text and is blocked by NOT NULL", () => {
  expect(validateCellValue(null, "boolean", true)).toBe("");
  expect(validateCellValue(null, "text", false)).toContain("NULL");
  expect(validateCellValue("", "text", false)).toBe("");
  expect(validateCellValue("", "integer")).not.toBe("");
});
test("MySQL TIME durations retain sign and more than 24 hours", () => {
  expect(validateCellValue("-120:45:59.123456", "time", true, "mysql")).toBe(
    "",
  );
  expect(validateCellValue("839:00:00", "time", true, "mysql")).not.toBe("");
  expect(validateCellValue("-120:00:00", "time", true, "postgres")).not.toBe(
    "",
  );
});
test("offset splitting preserves all fractional digits without Date conversion", () =>
  expect(splitOffset("2026-10-05 14:30:01.123456+07:00")).toEqual({
    local: "2026-10-05T14:30:01.123456",
    offset: "+07:00",
  }));
test("no-op checks keep NULL/empty distinct and avoid boolean/temporal spelling writes", () => {
  expect(sameCellValue("t", "true", "boolean")).toBe(true);
  expect(sameCellValue("1", "true", "tinyint(1)")).toBe(true);
  expect(
    sameCellValue(
      "2026-10-05 12:30:00.000000",
      "2026-10-05T12:30",
      "timestamp",
    ),
  ).toBe(true);
  expect(sameCellValue("12:30:01.123456", "12:30:01.123457", "time")).toBe(
    false,
  );
  expect(sameCellValue(null, "", "text")).toBe(false);
});
test("bulk draft expansion is bounded before creating positional edits", () => {
  expect(bulkEditRefusal(1000, Array(10).fill("x"))).toBe("");
  expect(bulkEditRefusal(1000, Array(11).fill("x"))).toContain("10,000");
  expect(bulkEditRefusal(1000, ["é".repeat(600)])).toContain("1 MiB");
});
