import { describe, expect, test } from "bun:test";
import { latestPlan, sessionChanges, toolSources, linkHosts } from "./summaryModel";

const item = (x: object) => x as never;

describe("latestPlan", () => {
  test("the last plan item in the session wins", () => {
    const items = [
      item({ kind: "plan", plan: [{ content: "a", status: "completed" }] }),
      item({ kind: "agent_message_chunk", text: "x" }),
      item({ kind: "plan", plan: [{ content: "b", status: "in_progress" }] }),
    ];
    expect(latestPlan(items)).toEqual([{ content: "b", status: "in_progress" }]);
  });
  test("none", () => {
    expect(latestPlan([])).toEqual([]);
  });
});

describe("sessionChanges", () => {
  test("sums per path across checkpoints and keeps the newest checkpoint for the diff", () => {
    const index = new Map([
      ["p1", { id: "c1", createdAt: "2026-10-13T10:00:00Z", status: "done", changed: [{ path: "a.go", added: 2, removed: 1, binary: false }] }],
      [
        "p2",
        {
          id: "c2",
          createdAt: "2026-10-13T10:05:00Z",
          status: "done",
          changed: [
            { path: "a.go", added: 3, removed: 0, binary: false },
            { path: "b.go", added: 1, removed: 0, binary: false },
          ],
        },
      ],
    ]) as never;
    expect(sessionChanges(index)).toEqual([
      { path: "a.go", added: 5, removed: 1, binary: false, checkpointId: "c2" },
      { path: "b.go", added: 1, removed: 0, binary: false, checkpointId: "c2" },
    ]);
  });
});

describe("toolSources", () => {
  test("counts DevKit data tools by module, either name form", () => {
    const items = [
      item({ kind: "tool_call", toolCall: { name: "mcp__devkit__db_query" } }),
      item({ kind: "tool_call", toolCall: { name: "db.explain" } }),
      item({ kind: "tool_call", toolCall: { name: "kafka.peekMessages" } }),
      item({ kind: "tool_call", toolCall: { name: "Read" } }),
    ];
    expect(toolSources(items)).toEqual([
      { module: "database", label: "Database", count: 2 },
      { module: "kafka", label: "Kafka", count: 1 },
    ]);
  });
});

describe("linkHosts", () => {
  test("hosts from user messages and fetch tool inputs, deduplicated, first seen first", () => {
    const items = [
      item({ kind: "user_message_chunk", text: "see https://grafana.local/d/auth and https://grafana.local/x" }),
      item({ kind: "tool_call", toolCall: { kind: "fetch", rawInput: { url: "https://docs.example.com/a" } } }),
    ];
    expect(linkHosts(items)).toEqual([
      { host: "grafana.local", url: "https://grafana.local/d/auth" },
      { host: "docs.example.com", url: "https://docs.example.com/a" },
    ]);
  });
});
