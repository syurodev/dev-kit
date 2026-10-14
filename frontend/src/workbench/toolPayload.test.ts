import { expect, test } from "bun:test";
import { toolNameOf, toolPayloadOf, toolResultInputOf } from "./toolPayload";
import { registerToolResultView, renderToolResult } from "./toolResultViews";

/** Claude (claude-agent-acp): the result is the content's text, no rawOutput. */
function claudeText(text: string) {
  return [{ type: "content", content: { type: "text", text } }];
}

// --- toolNameOf ---------------------------------------------------------------

test("toolNameOf keeps Claude's wire name: the registry already resolves it", () => {
  expect(toolNameOf({ name: "mcp__devkit__db_query" })).toBe("mcp__devkit__db_query");
});

test("toolNameOf reads a Codex call's tool from its devkit.<tool> title", () => {
  expect(toolNameOf({ title: "devkit.db.query" })).toBe("db.query");
  expect(toolNameOf({ title: "devkit.workspace.memory.get" })).toBe("workspace.memory.get");
});

test("toolNameOf says nothing for a call that is not a DevKit tool", () => {
  expect(toolNameOf({ title: "Read file" })).toBe("");
  expect(toolNameOf({ title: "devkit." })).toBe("");
  expect(toolNameOf({})).toBe("");
});

// --- toolPayloadOf: Claude ------------------------------------------------------

test("Claude: the JSON in the content text is the payload", () => {
  expect(
    toolPayloadOf({
      status: "completed",
      content: claudeText('{"columns":["a"],"rows":[["1"]],"row_count":1}'),
    }),
  ).toEqual({ payload: { columns: ["a"], rows: [["1"]], row_count: 1 } });
});

test("Claude: a fenced AppError on a failed call is the error, fence stripped", () => {
  expect(
    toolPayloadOf({
      status: "failed",
      content: claudeText('```\nquery_failed: relation "x" does not exist\n```'),
    }),
  ).toEqual({ error: 'query_failed: relation "x" does not exist' });
});

test("Claude: a fenced AppError is an error even when the status says nothing", () => {
  expect(toolPayloadOf({ content: claudeText("```\npdf.notFound: No such document\n```") })).toEqual({
    error: "pdf.notFound: No such document",
  });
});

test("Claude: a {result} envelope is unwrapped", () => {
  expect(toolPayloadOf({ status: "completed", content: claudeText('{"result":[{"name":"orders"}]}') })).toEqual({
    payload: [{ name: "orders" }],
  });
});

test("Claude: text that is not JSON is the payload as a string", () => {
  expect(toolPayloadOf({ status: "completed", content: claudeText("Page 1\nHello world") })).toEqual({
    payload: "Page 1\nHello world",
  });
});

test("Claude: a truncated text preview that does not parse is null — load the full row", () => {
  expect(
    toolPayloadOf({ status: "completed", truncated: true, content: claudeText('{"columns":["a"],"rows":[["1"') }),
  ).toBeNull();
  expect(
    toolPayloadOf({
      status: "completed",
      content: [{ type: "content", truncated: true, content: { type: "text", text: '{"columns":' } }],
    }),
  ).toBeNull();
});

// --- toolPayloadOf: Codex -------------------------------------------------------

test("Codex: structuredContent is the payload", () => {
  expect(
    toolPayloadOf({
      title: "devkit.db.query",
      status: "completed",
      rawOutput: {
        content: [{ type: "text", text: '{"columns":["a"],"rows":[]}' }],
        structuredContent: { columns: ["a"], rows: [] },
      },
    }),
  ).toEqual({ payload: { columns: ["a"], rows: [] } });
});

test("Codex: a structuredContent {result} envelope is unwrapped", () => {
  expect(
    toolPayloadOf({
      status: "completed",
      rawOutput: { content: [{ type: "text", text: "x" }], structuredContent: { result: [{ name: "t" }] } },
    }),
  ).toEqual({ payload: [{ name: "t" }] });
});

test("Codex: without structuredContent the text content is read", () => {
  expect(
    toolPayloadOf({ status: "completed", rawOutput: { content: [{ type: "text", text: "plain page text" }] } }),
  ).toEqual({ payload: "plain page text" });
  expect(
    toolPayloadOf({ status: "completed", rawOutput: { content: [{ type: "text", text: '{"a":1}' }] } }),
  ).toEqual({ payload: { a: 1 } });
});

test("Codex: an {error} is the error", () => {
  expect(toolPayloadOf({ status: "failed", rawOutput: { error: "boom" } })).toEqual({ error: "boom" });
});

test("Codex: an isError result is the error, read from its text", () => {
  expect(
    toolPayloadOf({
      status: "completed",
      rawOutput: { content: [{ type: "text", text: "db.notFound: no such profile" }], isError: true },
    }),
  ).toEqual({ error: "db.notFound: no such profile" });
});

test("Codex: the size stub is null — there is nothing to show", () => {
  expect(toolPayloadOf({ status: "completed", rawOutput: { truncated: true, bytes: 70000 } })).toBeNull();
});

test("Codex: rawOutput carried as a JSON string still reads as the MCP result", () => {
  expect(
    toolPayloadOf({
      status: "completed",
      rawOutput: JSON.stringify({ content: [{ type: "text", text: "x" }], structuredContent: { ok: true } }),
    }),
  ).toEqual({ payload: { ok: true } });
});

// --- toolPayloadOf: plain rawOutput ---------------------------------------------

test("plain rawOutput: a JSON string is parsed and a {result} envelope unwrapped", () => {
  expect(toolPayloadOf({ status: "completed", rawOutput: '{"a":1}' })).toEqual({ payload: { a: 1 } });
  expect(toolPayloadOf({ status: "completed", rawOutput: '{"result":[1,2]}' })).toEqual({ payload: [1, 2] });
  expect(toolPayloadOf({ status: "completed", rawOutput: { status: 200, statusText: "OK" } })).toEqual({
    payload: { status: 200, statusText: "OK" },
  });
});

test("plain rawOutput: text that is not JSON is the payload as a string", () => {
  expect(toolPayloadOf({ status: "completed", rawOutput: "not json at all" })).toEqual({
    payload: "not json at all",
  });
});

test("plain rawOutput: a truncated preview that does not parse is null", () => {
  expect(toolPayloadOf({ status: "completed", truncated: true, rawOutput: '{"columns":["a"],"ro' })).toBeNull();
});

test("a payload whose own fields are named truncated/bytes is not the Codex stub", () => {
  const response = { status: 200, statusText: "OK", truncated: true, bytes: 70000, durationMs: 4 };
  expect(toolPayloadOf({ status: "completed", rawOutput: response })).toEqual({ payload: response });
});

test("a failed call with no output still reports an error", () => {
  expect(toolPayloadOf({ status: "failed" })).toEqual({ error: "failed" });
  expect(toolPayloadOf({ status: "failed", content: claudeText("permission denied") })).toEqual({
    error: "permission denied",
  });
});

test("a call with no output at all is null", () => {
  expect(toolPayloadOf({ status: "in_progress" })).toBeNull();
  expect(toolPayloadOf({ status: "completed", content: [] })).toBeNull();
});

// --- toolResultInputOf + renderToolResult ---------------------------------------

test("toolResultInputOf hands views the payload, the content blocks and the status", () => {
  const image = { type: "image", mimeType: "image/png", data: "AAAA" };
  const input = toolResultInputOf({
    name: "mcp__devkit__pdf_viewPages",
    status: "completed",
    rawInput: { documentId: "doc1" },
    content: [...claudeText("Pages 1-2"), { type: "content", content: image }],
  });
  expect(input).toEqual({
    name: "mcp__devkit__pdf_viewPages",
    rawInput: { documentId: "doc1" },
    rawOutput: "Pages 1-2",
    content: [{ type: "text", text: "Pages 1-2" }, image],
    status: "completed",
  });
});

test("toolResultInputOf is null for an error: views see only successful payloads", () => {
  expect(toolResultInputOf({ name: "x", status: "failed", rawOutput: { error: "boom" } })).toBeNull();
  expect(toolResultInputOf({ name: "x", status: "completed", rawOutput: { truncated: true, bytes: 9 } })).toBeNull();
});

test("a Codex call reaches the view registered under its Go name", () => {
  const unregister = registerToolResultView("payloadtest.codex", ({ rawOutput, status }) =>
    `${status}:${JSON.stringify(rawOutput)}`,
  );
  try {
    const input = toolResultInputOf({
      title: "devkit.payloadtest.codex",
      status: "completed",
      rawOutput: { content: [{ type: "text", text: "{}" }], structuredContent: { n: 1 } },
    });
    expect(input && renderToolResult(input)).toBe('completed:{"n":1}');
  } finally {
    unregister();
  }
});

test("a page image dropped from the stored row does not make the caption a preview", () => {
  // internal/agenthub: transcript_slim.go empties a stored page image and marks
  // its entry truncated; the page read then marks the call. The caption itself
  // is whole, so it is still the payload — what pdf.viewPages' view reads.
  const call = {
    name: "mcp__devkit__pdf_viewPages",
    status: "completed",
    truncated: true,
    content: [
      ...claudeText("Document doc1\n[]"),
      { type: "content", truncated: true, content: { type: "image", mimeType: "image/png", data: "" } },
    ],
  };
  expect(toolPayloadOf(call)).toEqual({ payload: "Document doc1\n[]" });
});
