import { expect, test } from "bun:test";
import { parseDiffResult } from "./diffResult";

const change = { path: "src/a.go", kind: "modified" as const, added: 3, removed: 1 };

test("parses a change list with its rev from rawInput", () => {
  expect(parseDiffResult({ rev: "abc123" }, [change])).toEqual({ changes: [change], rev: "abc123" });
});

test("an empty rev means the work tree, same as the tool call", () => {
  expect(parseDiffResult({ rev: "" }, [change])).toEqual({ changes: [change], rev: "" });
  expect(parseDiffResult({}, [change])).toEqual({ changes: [change], rev: "" });
});

test("returns null for an empty change list — nothing to show", () => {
  expect(parseDiffResult({ rev: "abc123" }, [])).toBeNull();
});

test("returns null when rawOutput isn't a change list", () => {
  expect(parseDiffResult({ rev: "abc123" }, { not: "a list" })).toBeNull();
  expect(parseDiffResult({ rev: "abc123" }, null)).toBeNull();
});

test("unwraps structured()'s {result: […]} envelope", () => {
  expect(parseDiffResult({ rev: "abc123" }, { result: [change] })).toEqual({ changes: [change], rev: "abc123" });
});
