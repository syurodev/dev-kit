import { expect, test } from "bun:test";
import { parseEntryPointMatches, parseSymbolMatches } from "./searchResult";

const symbol = { nodeId: "n1", label: "RefundController.create", kind: "method", filePath: "a.java", line: 10 };
const entry = { id: "e1", verb: "POST", target: "/refunds", entryNode: "n1", kind: "http" };

test("parses a non-empty symbol match list", () => {
  expect(parseSymbolMatches([symbol])).toEqual([symbol]);
});

test("returns null for an empty symbol match list", () => {
  expect(parseSymbolMatches([])).toBeNull();
});

test("returns null when a symbol match list item is malformed", () => {
  expect(parseSymbolMatches([{ label: "no nodeId" }])).toBeNull();
});

test("parses a non-empty entry point list", () => {
  expect(parseEntryPointMatches([entry])).toEqual([{ id: "e1", verb: "POST", target: "/refunds" }]);
});

test("reads a transcript written before entry points had kinds", () => {
  const legacy = { id: "e1", httpMethod: "POST", path: "/refunds", entryNode: "n1" };
  expect(parseEntryPointMatches([legacy])).toEqual([{ id: "e1", verb: "POST", target: "/refunds" }]);
});

test("returns null when an entry point list item is malformed", () => {
  expect(parseEntryPointMatches([{ id: "e1" }])).toBeNull();
});

test("returns null for an empty entry point list", () => {
  expect(parseEntryPointMatches([])).toBeNull();
});

test("returns null when rawOutput isn't a list at all", () => {
  expect(parseSymbolMatches("nope")).toBeNull();
  expect(parseEntryPointMatches({ result: "not an array" })).toBeNull();
});

test("reads one page of results, which carries its total and cursor beside the rows", () => {
  const page = { result: [symbol], total: 120, nextCursor: "abc" };
  expect(parseSymbolMatches(page)).toEqual([symbol]);
  expect(parseSymbolMatches(JSON.stringify(page))).toEqual([symbol]);
  expect(parseEntryPointMatches({ result: [entry], total: 300, nextCursor: "abc" })).toEqual([
    { id: "e1", verb: "POST", target: "/refunds" },
  ]);
});
