import { describe, expect, it } from "bun:test";
import {
  escapeCSVCell,
  serializeCSV,
  serializeJSON,
  tableExportFilename,
  exportFilenameError,
  exportQueryResult,
  serializeExport,
} from "./export";
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

  describe("A28: the filename a table tab suggests", () => {
    it("is the table's own name", () => {
      expect(tableExportFilename("users")).toBe("users");
      expect(tableExportFilename("order_items")).toBe("order_items");
      expect(tableExportFilename("Sales-2026")).toBe("Sales-2026");
    });

    it("trims the identifier before judging it", () => {
      expect(tableExportFilename("  users  ")).toBe("users");
    });

    it("replaces what a filesystem would refuse", () => {
      // Quoted identifiers really can contain these; a save dialog handed one
      // either fails or writes somewhere nobody asked for.
      expect(tableExportFilename("public/users")).toBe("public-users");
      expect(tableExportFilename("a:b*c?d")).toBe("a-b-c-d");
      expect(tableExportFilename("my table")).toBe("my-table");
      // ...but a name that is merely not ASCII is left alone.
      expect(tableExportFilename("caf\u00e9")).toBe("caf\u00e9");
    });

    it("never proposes a hidden file or a bare extension", () => {
      expect(tableExportFilename(".hidden")).toBe("hidden");
      expect(tableExportFilename("..")).toBe("table-rows");
      expect(tableExportFilename("")).toBe("table-rows");
      expect(tableExportFilename("   ")).toBe("table-rows");
      expect(tableExportFilename("///")).toBe("table-rows");
    });

    it("leaves no trailing dot for the extension to follow", () => {
      expect(tableExportFilename("users.")).toBe("users");
    });
  });
});

it("keeps reserved object keys as exported data", () => {
  const result = {
    columns: ["__proto__", "constructor"],
    rows: [["data", "value"]],
    rowCount: 1,
    truncated: false,
  };
  expect(JSON.parse(serializeJSON(result))[0]["__proto__"]).toBe("data");
  expect(JSON.parse(serializeJSON(result))[0].constructor).toBe("value");
});

it("refuses ambiguous JSON names before opening a file picker", async () => {
  const result = {
    columns: ["x", "x"],
    rows: [["a", "b"]],
    rowCount: 1,
    truncated: false,
  };
  await expect(exportQueryResult(result, "json")).rejects.toThrow(
    "unique column names",
  );
});
it("checks suggested basenames without erasing Unicode", () => {
  expect(exportFilenameError("../bad")).not.toBe("");
  expect(exportFilenameError(".json")).not.toBe("");
  expect(exportFilenameError("dữ liệu.json")).toBe("");
});

it("worker serialization preserves large result precision and NULL", async () => {
  const rows = Array.from({ length: 1000 }, () => [
    "9007199254740993",
    null,
    "dữ liệu",
  ]);
  const content = await serializeExport(
    {
      columns: ["id", "missing", "text"],
      rows,
      rowCount: 1000,
      truncated: false,
    },
    "json",
  );
  const parsed = JSON.parse(content);
  expect(parsed.length).toBe(1000);
  expect(parsed[999]).toEqual({
    id: "9007199254740993",
    missing: null,
    text: "dữ liệu",
  });
});
it("one large cell also serializes with exact CSV escaping", async () => {
  const text = '"' + "x".repeat(150000) + "\n";
  const result = {
    columns: ["value"],
    rows: [[text]],
    rowCount: 1,
    truncated: false,
  };
  expect(await serializeExport(result, "csv")).toBe(serializeCSV(result));
});
