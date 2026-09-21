import { expect, test } from "bun:test";
import {
  hasToolResultView,
  parseToolOutput,
  registerToolResultView,
  renderToolResult,
} from "./toolResultViews";

test("hasToolResultView is false when nobody registered the name", () => {
  expect(hasToolResultView("codegraph.nobody-registered-this")).toBe(false);
  expect(hasToolResultView("mcp__devkit__codegraph_nobody-registered-this")).toBe(false);
  expect(hasToolResultView(undefined)).toBe(false);
});

test("registerToolResultView matches the wire-mangled name Claude actually reports", () => {
  const unregister = registerToolResultView("codegraph.getApiFlow", ({ rawOutput }) => `flow:${rawOutput}`);
  try {
    expect(hasToolResultView("mcp__devkit__codegraph_getApiFlow")).toBe(true);
    // The exact Go name also matches, in case a future vendor reports it unmangled.
    expect(hasToolResultView("codegraph.getApiFlow")).toBe(true);
    expect(
      renderToolResult({ name: "mcp__devkit__codegraph_getApiFlow", rawInput: null, rawOutput: "ok" }),
    ).toBe("flow:ok");
  } finally {
    unregister();
  }
  expect(hasToolResultView("mcp__devkit__codegraph_getApiFlow")).toBe(false);
});

test("a name with underscores in its own segment still round-trips", () => {
  // notes.set_organization: the transform must not confuse this tool's own
  // "_" with the "." it also replaces.
  const unregister = registerToolResultView("notes.set_organization", () => "org");
  try {
    expect(hasToolResultView("mcp__devkit__notes_set_organization")).toBe(true);
  } finally {
    unregister();
  }
});

test("a name that nests two dots still round-trips", () => {
  const unregister = registerToolResultView("tasks.board.list", () => "board");
  try {
    expect(hasToolResultView("mcp__devkit__tasks_board_list")).toBe(true);
  } finally {
    unregister();
  }
});

test("registering the same name twice replaces the renderer", () => {
  const calls: string[] = [];
  registerToolResultView("tasks.create", () => {
    calls.push("first");
    return null;
  });
  const unregisterSecond = registerToolResultView("tasks.create", () => {
    calls.push("second");
    return null;
  });
  renderToolResult({ name: "mcp__devkit__tasks_create", rawInput: null, rawOutput: null });
  expect(calls).toEqual(["second"]);
  unregisterSecond();
});

test("renderToolResult falls back to null when the renderer declines", () => {
  const unregister = registerToolResultView("tasks.get", () => null);
  try {
    expect(
      renderToolResult({ name: "mcp__devkit__tasks_get", rawInput: null, rawOutput: null }),
    ).toBeNull();
  } finally {
    unregister();
  }
});

test("parseToolOutput passes an already-parsed object through untouched", () => {
  expect(parseToolOutput({ endpointId: "e1", steps: [] })).toEqual({ endpointId: "e1", steps: [] });
});

test("parseToolOutput parses a JSON string (a built-in vendor tool, or a re-quoted preview)", () => {
  expect(parseToolOutput('{"a":1}')).toEqual({ a: 1 });
});

test("parseToolOutput returns null for text that isn't JSON at all", () => {
  expect(parseToolOutput("not json")).toBeNull();
});

test("parseToolOutput returns null for a truncated document cut mid-structure", () => {
  expect(parseToolOutput('{"endpointId":"e1","steps":[{"la')).toBeNull();
});

test("parseToolOutput unwraps structured()'s single-key {result: …} envelope", () => {
  expect(parseToolOutput({ result: ["a", "b"] })).toEqual(["a", "b"]);
  expect(parseToolOutput({ result: "file contents" })).toBe("file contents");
});

test("parseToolOutput leaves an object alone when result is one of several keys", () => {
  expect(parseToolOutput({ result: "ok", other: 1 })).toEqual({ result: "ok", other: 1 });
});

test("parseToolOutput leaves an array alone even though it has one element", () => {
  expect(parseToolOutput(["only"])).toEqual(["only"]);
});
