import { expect, test } from "bun:test";
import { parseSendResult } from "./sendResult";

const response = { status: 200, statusText: "OK", body: "ignored", truncated: false, bytes: 128, durationMs: 42 };

test("parses a response with requestId from rawInput", () => {
  expect(parseSendResult({ requestId: "r1", environmentId: "e1" }, response)).toEqual({
    response,
    requestId: "r1",
  });
});

test("returns null when rawInput has no requestId", () => {
  expect(parseSendResult({}, response)).toBeNull();
  expect(parseSendResult(null, response)).toBeNull();
});

test("returns null when rawOutput doesn't look like a Response", () => {
  expect(parseSendResult({ requestId: "r1" }, { not: "a response" })).toBeNull();
});
