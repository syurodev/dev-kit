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
  test("a subagent's plan is not the session's", () => {
    const items = [
      item({ kind: "plan", plan: [{ content: "mine", status: "pending" }] }),
      item({ kind: "plan", origin: "sub1", plan: [{ content: "theirs", status: "pending" }] }),
    ];
    expect(latestPlan(items)).toEqual([{ content: "mine", status: "pending" }]);
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

describe("sessionChanges order and status", () => {
  const cp = (id: string, createdAt: string, paths: string[], status = "done") => ({
    id,
    createdAt,
    status,
    changed: paths.map((path) => ({ path, added: 1, removed: 0, binary: false })),
  });
  test("newest touched first, a turn's own order kept inside it", () => {
    const index = new Map([
      ["p1", cp("c1", "2026-10-13T10:00:00Z", ["a.go", "b.go", "c.go"])],
      ["p2", cp("c2", "2026-10-13T10:05:00Z", ["d.go", "a.go"])],
    ]) as never;
    expect(sessionChanges(index).map((c) => c.path)).toEqual(["d.go", "a.go", "b.go", "c.go"]);
  });
  test("a checkpoint that is not done yet counts for nothing", () => {
    const index = new Map([
      ["p1", cp("c1", "2026-10-13T10:00:00Z", ["a.go"])],
      ["p2", cp("c2", "2026-10-13T10:05:00Z", ["a.go", "b.go"], "pending")],
      ["p3", cp("c3", "2026-10-13T10:06:00Z", ["c.go"], "failed")],
    ]) as never;
    expect(sessionChanges(index)).toEqual([{ path: "a.go", added: 1, removed: 0, binary: false, checkpointId: "c1" }]);
  });
});

describe("toolSources", () => {
  test("Codex's MCP calls carry no name; the title is server.tool", () => {
    const items = [
      item({ kind: "tool_call", toolCall: { id: "1", kind: "other", title: "devkit.db.query" } }),
      item({ kind: "tool_call", toolCall: { id: "2", kind: "other", title: "devkit.ssh_runCommand" } }),
      item({ kind: "tool_call", toolCall: { id: "3", kind: "other", title: "github.db.query" } }),
      item({ kind: "tool_call", toolCall: { id: "4", kind: "execute", title: "db.sh" } }),
    ];
    expect(toolSources(items)).toEqual([
      { module: "database", label: "Database", count: 1 },
      { module: "ssh", label: "SSH", count: 1 },
    ]);
  });

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
  test("trailing punctuation is not part of the link; a subagent's message is not the person's", () => {
    const items = [
      item({ kind: "user_message_chunk", text: "read https://a.example.com/x/y. Then https://b.example.com!?" }),
      item({ kind: "user_message_chunk", origin: "sub1", text: "https://sub.example.com" }),
    ];
    expect(linkHosts(items)).toEqual([
      { host: "a.example.com", url: "https://a.example.com/x/y" },
      { host: "b.example.com", url: "https://b.example.com" },
    ]);
  });
  test("only http(s) with a host", () => {
    const items = [item({ kind: "tool_call", toolCall: { kind: "fetch", rawInput: { url: "file:///etc/passwd" } } })];
    expect(linkHosts(items)).toEqual([]);
  });
});
