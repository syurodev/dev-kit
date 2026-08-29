import { describe, expect, it } from "bun:test";
import { escapeCSVCell, serializeCSV, serializeJSON } from "./export";
import type { QueryResult } from "../../bridge/database";

describe("Database Query Result Export", () => {
  it("escapes CSV cells per RFC 4180", () => {
    expect(escapeCSVCell("simple")).toBe("simple");
    expect(escapeCSVCell(null)).toBe("");
    expect(escapeCSVCell(undefined)).toBe("");
    expect(escapeCSVCell("hello, world")).toBe('"hello, world"');
    expect(escapeCSVCell('say "hello"')).toBe('"say ""hello"""');
    expect(escapeCSVCell("multi\nline")).toBe('"multi\nline"');
  });

  it("serializes QueryResult to RFC 4180 CSV", () => {
    const sample: QueryResult = {
      columns: ["id", "name", "notes"],
      rows: [
        ["1", "Alice", "Regular user"],
        ["2", 'Bob "The Builder"', "Needs review, urgent"],
      ],
      rowCount: 2,
      truncated: false,
    };

    const csv = serializeCSV(sample);
    const expected =
      'id,name,notes\r\n1,Alice,Regular user\r\n2,"Bob ""The Builder""","Needs review, urgent"';
    expect(csv).toBe(expected);
  });

  it("serializes QueryResult to all-string JSON preserving string[][] typing", () => {
    const sample: QueryResult = {
      columns: ["id", "count", "is_active"],
      rows: [
        ["42", "100", "true"],
        ["99", "0", "false"],
      ],
      rowCount: 2,
      truncated: false,
    };

    const jsonStr = serializeJSON(sample);
    const parsed = JSON.parse(jsonStr);

    expect(parsed).toEqual([
      { id: "42", count: "100", is_active: "true" },
      { id: "99", count: "0", is_active: "false" },
    ]);
    expect(typeof parsed[0].id).toBe("string");
    expect(typeof parsed[0].count).toBe("string");
    expect(typeof parsed[0].is_active).toBe("string");
  });

  it("handles 200 rows cap export correctly", () => {
    const rows: string[][] = [];
    for (let i = 1; i <= 200; i++) {
      rows.push([String(i), `User ${i}`]);
    }

    const sample: QueryResult = {
      columns: ["id", "username"],
      rows,
      rowCount: 200,
      truncated: true,
    };

    const csv = serializeCSV(sample);
    const lines = csv.split("\r\n");
    expect(lines.length).toBe(201); // 1 header + 200 rows

    const jsonStr = serializeJSON(sample);
    const parsed = JSON.parse(jsonStr);
    expect(parsed.length).toBe(200);
    expect(parsed[199]).toEqual({ id: "200", username: "User 200" });
  });
});
