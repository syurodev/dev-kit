import { describe, expect, test } from "bun:test";
import {
  backgroundTasks,
  connectorRows,
  environmentRows,
  splitFileName,
  latestPlan,
  limitRows,
  liveLimits,
  linkHosts,
  resetsIn,
  sessionChanges,
  toolSources,
  tokenRows,
} from "./summaryModel";

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

describe("environmentRows", () => {
  const options = [
    { id: "model", currentValue: "gpt-6", options: [{ value: "gpt-6", name: "GPT-6" }] },
    { id: "reasoning_effort", currentValue: "medium", options: [{ value: "medium", name: "Medium" }] },
  ];
  test("names what the session has and leaves out what it does not", () => {
    const rows = environmentRows({
      vendorName: "Codex",
      folderPath: "/work/api/",
      workspaceName: null,
      repos: [],
      configOptions: options,
      mode: "auto",
      availableModes: [{ id: "auto", name: "Auto" }],
    });
    expect(rows.map((r) => [r.key, r.value])).toEqual([
      ["agent", "Codex"],
      ["model", "GPT-6"],
      ["effort", "Medium"],
      ["mode", "Auto"],
      ["folder", "api"],
    ]);
    expect(rows.find((r) => r.key === "folder")?.title).toBe("/work/api/");
  });
  test("names the account only once the session is known to run as one, right after the agent", () => {
    const base = { vendorName: "Claude", folderPath: "", configOptions: options, mode: null, availableModes: [], repos: [] };
    expect(environmentRows({ ...base, accountLabel: "Work" }).map((r) => [r.key, r.value])).toEqual([
      ["agent", "Claude"],
      ["account", "Work"],
      ["model", "GPT-6"],
      ["effort", "Medium"],
    ]);
    expect(environmentRows(base).find((r) => r.key === "account")).toBeUndefined();
    expect(environmentRows({ ...base, accountLabel: null }).find((r) => r.key === "account")).toBeUndefined();
    expect(environmentRows({ ...base, accountLabel: "" }).find((r) => r.key === "account")).toBeUndefined();
  });
  test("one repository is its branch, several are counted", () => {
    const base = { vendorName: "Claude", folderPath: "", configOptions: [], mode: null, availableModes: [] };
    const one = environmentRows({ ...base, repos: [{ name: "api", branch: "main", own: true }] });
    expect(one.find((r) => r.key === "branch")?.value).toBe("main");
    const many = environmentRows({
      ...base,
      workspaceName: "Clinic",
      repos: [
        { name: "api", branch: "main", own: true },
        { name: "web", branch: "feat/x", own: false },
      ],
    });
    const branches = many.find((r) => r.key === "branch");
    expect(branches?.value).toBe("2 branches · 2 repos");
    expect(branches?.title).toBe("feat/x (1): web\nmain (1): api");
    expect(many.find((r) => r.key === "workspace")?.value).toBe("Clinic");
  });
});

describe("environmentRows with many repositories", () => {
  const base = { vendorName: "opencode", folderPath: "", configOptions: [], mode: null, availableModes: [] };
  const repos = (branches: string[]) => branches.map((branch, i) => ({ name: `svc-${i}`, branch, own: i === 0 }));

  test("fifteen repositories on one branch say the branch and the count", () => {
    const row = environmentRows({ ...base, repos: repos(Array(15).fill("main")) }).find((r) => r.key === "branch");
    expect(row?.label).toBe("Branches");
    expect(row?.value).toBe("main · 15 repos");
  });

  test("on several branches it counts the branches, and the tooltip groups the repositories, biggest group first", () => {
    const row = environmentRows({ ...base, repos: repos(["main", "main", "feat/x", "main"]) }).find((r) => r.key === "branch");
    expect(row?.value).toBe("2 branches · 4 repos");
    expect(row?.title).toBe("main (3): svc-0, svc-1, svc-3\nfeat/x (1): svc-2");
  });
});

describe("splitFileName", () => {
  test("keeps the extension, and a short infix with it, apart from the stem", () => {
    expect(splitFileName("booking-medical-examination-note.spec.ts")).toEqual({ stem: "booking-medical-examination-note", tail: ".spec.ts" });
    expect(splitFileName("customer.entity.ts")).toEqual({ stem: "customer", tail: ".entity.ts" });
    expect(splitFileName("service.go")).toEqual({ stem: "service", tail: ".go" });
    expect(splitFileName("archive.tar.gz")).toEqual({ stem: "archive", tail: ".tar.gz" });
  });
  test("a long infix is a name, not an extension", () => {
    expect(splitFileName("some.very-long-segment.ts")).toEqual({ stem: "some.very-long-segment", tail: ".ts" });
  });
  test("a dotfile, a bare name and a trailing dot have no tail", () => {
    expect(splitFileName(".gitignore")).toEqual({ stem: ".gitignore", tail: "" });
    expect(splitFileName("Makefile")).toEqual({ stem: "Makefile", tail: "" });
    expect(splitFileName("odd.")).toEqual({ stem: "odd.", tail: "" });
  });
});

describe("limitRows", () => {
  const now = Date.UTC(2026, 9, 16, 12, 0, 0);
  test("the vendor's own window labels, with when each resets", () => {
    const rows = limitRows(
      [
        { label: "5-hour", usedPercent: 42, resetsAt: now / 1000 + 3 * 3600, status: "ok" },
        { label: "Weekly", usedPercent: 91.4, resetsAt: now / 1000 + 20 * 60, status: "warning" },
        { label: "Spark · Plan", usedPercent: 100, status: "reached" },
      ],
      now,
    );
    expect(rows.map((r) => [r.label, r.value, r.tone])).toEqual([
      ["5-hour", "42% · resets in 3h", undefined],
      ["Weekly", "91% · resets in 20m", "warning"],
      ["Spark · Plan", "100%", "danger"],
    ]);
  });
  test("a window whose reset has passed is not a row: its usage started over", () => {
    const rows = limitRows(
      [
        { label: "5-hour", usedPercent: 99, resetsAt: now / 1000 - 1 },
        { label: "Weekly", usedPercent: 10, resetsAt: now / 1000 + 600 },
        { label: "Spark", usedPercent: 30 },
      ],
      now,
    );
    expect(rows.map((r) => r.label)).toEqual(["Weekly", "Spark"]);
    // The same windows a minute later: the next one has gone too.
    expect(limitRows([{ label: "Weekly", usedPercent: 10, resetsAt: now / 1000 + 600 }], now + 601_000)).toEqual([]);
    expect(liveLimits([{ label: "Weekly", usedPercent: 10, resetsAt: now / 1000 + 600 }], now + 600_000)).toEqual([]);
  });
  test("none when the vendor reports none", () => {
    expect(limitRows(undefined)).toEqual([]);
    expect(limitRows([])).toEqual([]);
  });
  test("resetsIn counts hours and days once minutes stop meaning anything", () => {
    const at = (minutes: number) => now / 1000 + minutes * 60;
    expect(resetsIn(at(5), now)).toBe("resets in 5m");
    expect(resetsIn(at(89), now)).toBe("resets in 89m");
    expect(resetsIn(at(300), now)).toBe("resets in 5h");
    expect(resetsIn(at(60 * 72), now)).toBe("resets in 3d");
    expect(resetsIn(at(-5), now)).toBe("resets in 0m");
  });
});

describe("tokenRows", () => {
  const counts = (t: number, i: number, c: number, o: number, r: number) => ({ total: t, input: i, cached: c, output: o, reasoning: r });
  test("input with its cache share, output with its reasoning, the last request", () => {
    const rows = tokenRows({ session: counts(1_000_000, 980_000, 490_000, 20_000, 5_000), lastRequest: counts(30_000, 29_000, 0, 1_000, 0) });
    expect(rows.map((r) => [r.label, r.value])).toEqual([
      ["Input", "980k · 50% cached"],
      ["Output", "20k · 5k reasoning"],
      ["Last request", "30k"],
    ]);
  });
  test("nothing for a vendor that gives no split, or an empty one", () => {
    expect(tokenRows(null)).toEqual([]);
    expect(tokenRows(undefined)).toEqual([]);
    expect(tokenRows({ session: counts(0, 0, 0, 0, 0), lastRequest: counts(0, 0, 0, 0, 0) })).toEqual([]);
  });
});

describe("connectorRows", () => {
  test("state as a word, a failure's text on hover, a sign-in said as such", () => {
    const rows = connectorRows([
      { name: "devkit", state: "ready" },
      { name: "github", state: "failed", error: "401", needsLogin: true },
      { name: "db", state: "failed", error: "refused" },
      { name: "slow", state: "starting" },
    ]);
    expect(rows.map((r) => [r.label, r.value, r.tone, r.title])).toEqual([
      ["devkit", "Ready", undefined, undefined],
      ["github", "Sign-in needed", "warning", "401"],
      ["db", "Failed", "danger", "refused"],
      ["slow", "Starting", undefined, undefined],
    ]);
    expect(connectorRows(undefined)).toEqual([]);
  });
});

describe("backgroundTasks", () => {
  test("one per id, running first, a subagent's own listed: the process belongs to the session", () => {
    const items = [
      item({ asyncTask: { id: "a", name: "Tests", state: "completed" } }),
      item({ asyncTask: { id: "b", name: "Dev server", state: "running" } }),
      item({ asyncTask: { id: "a", name: "Tests", state: "failed" } }),
      item({ origin: "sub1", asyncTask: { id: "c", name: "Theirs", state: "running" } }),
      item({ kind: "agent_message_chunk" }),
    ];
    expect(backgroundTasks(items).map((t) => [t.id, t.state])).toEqual([
      ["b", "running"],
      ["c", "running"],
      ["a", "failed"],
    ]);
  });
});
