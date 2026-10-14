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
  const call = (name: string) => item({ kind: "tool_call", toolCall: { name } });
  const titled = (title: string) => item({ kind: "tool_call", toolCall: { id: "x", kind: "other", title } });

  test("listing saved profiles is not using the module", () => {
    expect(toolSources([call("mcp__devkit__ssh_listProfiles")])).toEqual([]);
    expect(
      toolSources([
        call("ssh.listProfiles"),
        call("ssh.activeProfile"),
        call("ssh.testConnection"),
        call("db.listProfiles"),
        call("db.history.list"),
        call("mcp__devkit__db_savedQuery_list"),
        call("kafka.connectionStatus"),
        call("kafka.listProfiles"),
      ]),
    ).toEqual([]);
  });

  test("a tool of another's that looks like ours once underscores turn to dots is not ours", () => {
    // A vendor's own `db_query` is not DevKit's db.query: names are matched
    // forward, from the Go names, never by guessing dots back into one.
    expect(toolSources([call("db_query"), call("ssh_runCommand"), call("kafka_listTopics")])).toEqual([]);
  });

  test("ssh.runCommand says how many commands ran", () => {
    expect(toolSources([call("ssh.runCommand"), call("ssh.listProfiles"), call("ssh.runCommand")])).toEqual([
      { module: "ssh", label: "SSH", count: 2, detail: "ran 2 commands" },
    ]);
    expect(toolSources([call("ssh.runCommand")])).toEqual([{ module: "ssh", label: "SSH", count: 1, detail: "ran 1 command" }]);
  });

  test("wire names and Codex titles are read in full, not by prefix", () => {
    expect(toolSources([call("mcp__devkit__ssh_runCommand"), titled("devkit.ssh.runCommand"), titled("devkit.ssh_runCommand")])).toEqual([
      { module: "ssh", label: "SSH", count: 3, detail: "ran 3 commands" },
    ]);
    expect(toolSources([titled("devkit.db.query"), titled("devkit.db_schema_get")])).toEqual([
      { module: "database", label: "Database", count: 2, detail: "1 query" },
    ]);
    expect(toolSources([call("mcp__devkit__db_schema_get")])).toEqual([
      { module: "database", label: "Database", count: 1, detail: "read the schema" },
    ]);
  });

  test("another server's tool with the same name, or a shell script, is not a source", () => {
    expect(
      toolSources([
        titled("github.db.query"),
        item({ kind: "tool_call", toolCall: { id: "4", kind: "execute", title: "db.sh" } }),
        call("Bash"),
        call("Read"),
      ]),
    ).toEqual([]);
  });

  test("database counts queries; schema reads ride along without being named", () => {
    expect(toolSources([call("mcp__devkit__db_query"), call("db.query"), call("db.schema.get")])).toEqual([
      { module: "database", label: "Database", count: 3, detail: "2 queries" },
    ]);
    expect(toolSources([call("db.explain"), call("db.listForeignKeys")])).toEqual([
      { module: "database", label: "Database", count: 2, detail: "read the schema" },
    ]);
  });

  test("kafka names the most significant thing done", () => {
    expect(toolSources([call("kafka.peekMessages"), call("kafka.peekMessages"), call("kafka.listTopics")])).toEqual([
      { module: "kafka", label: "Kafka", count: 3, detail: "peeked 2 topics" },
    ]);
    expect(toolSources([call("kafka.peekMessages"), call("kafka.publishMessage")])).toEqual([
      { module: "kafka", label: "Kafka", count: 2, detail: "published 1" },
    ]);
    expect(toolSources([call("kafka.describeTopic"), call("kafka.listConsumerGroups")])).toEqual([
      { module: "kafka", label: "Kafka", count: 2, detail: "read topics" },
    ]);
  });

  test("modules keep the order they were first used in", () => {
    expect(toolSources([call("kafka.listTopics"), call("db.query")]).map((s) => s.module)).toEqual(["kafka", "database"]);
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
