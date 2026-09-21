import { expect, test } from "bun:test";
import { parseEndpointMatches, parseSymbolMatches } from "./searchResult";

const symbol = { nodeId: "n1", label: "RefundController.create", kind: "method", filePath: "a.java", line: 10 };
const endpoint = { id: "e1", httpMethod: "POST", path: "/refunds", entryNode: "n1" };

test("parses a non-empty symbol match list", () => {
  expect(parseSymbolMatches([symbol])).toEqual([symbol]);
});

test("returns null for an empty symbol match list", () => {
  expect(parseSymbolMatches([])).toBeNull();
});

test("returns null when a symbol match list item is malformed", () => {
  expect(parseSymbolMatches([{ label: "no nodeId" }])).toBeNull();
});

test("parses a non-empty endpoint list", () => {
  expect(parseEndpointMatches([endpoint])).toEqual([endpoint]);
});

test("returns null for an empty endpoint list", () => {
  expect(parseEndpointMatches([])).toBeNull();
});

test("returns null when rawOutput isn't a list at all", () => {
  expect(parseSymbolMatches("nope")).toBeNull();
  expect(parseEndpointMatches({ result: "not an array" })).toBeNull();
});
