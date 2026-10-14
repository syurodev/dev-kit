import { expect, test } from "bun:test";
import { parseSendResult } from "./sendResult";
import { toolResultInputOf } from "@/workbench/toolPayload";

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

test("parses api.send whatever the vendor sends, through toolPayloadOf", () => {
  // Claude (claude-agent-acp): no rawOutput, the JSON rides in the content text.
  const claude = toolResultInputOf({
    name: "mcp__devkit__api_send",
    status: "completed",
    rawInput: { requestId: "r1" },
    content: [{ type: "content", content: { type: "text", text: JSON.stringify(response) } }],
  });
  expect(claude && parseSendResult(claude.rawInput, claude.rawOutput)).toEqual({ response, requestId: "r1" });

  // Codex: no name, an MCP result in rawOutput.
  const codex = toolResultInputOf({
    title: "devkit.api.send",
    status: "completed",
    rawInput: { requestId: "r1" },
    rawOutput: { content: [{ type: "text", text: JSON.stringify(response) }], structuredContent: response },
  });
  expect(codex?.name).toBe("api.send");
  expect(codex && parseSendResult(codex.rawInput, codex.rawOutput)).toEqual({ response, requestId: "r1" });
});
