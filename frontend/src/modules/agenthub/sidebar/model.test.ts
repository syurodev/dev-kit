import { describe, expect, it } from "bun:test";
import type { AgentSession } from "@/bridge/agenthub";
import type { SidebarWorkspace } from "./grouping";
import {
  RECENT_LIMIT,
  askBySession,
  attentionBySession,
  attentionKind,
  cardIndicator,
  cardSummary,
  compareRecent,
  groupSessions,
  nextWaiting,
  orderSubagents,
  partitionGroup,
  shortAge,
  subagentsLabel,
  drawnWhenHidingClosed,
  groupCollapsed,
  needsYou,
  plainAsk,
  secondLine,
} from "./model";

const session = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/work/api",
  workspaceId: "",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: "",
  updatedAt: 0,
  ...over,
});

/** n sessions, s0 the newest. */
const sessionsAged = (n: number, over: Partial<AgentSession> = {}) =>
  Array.from({ length: n }, (_, i) => session(`s${i}`, { updatedAt: 10_000 - i * 100, ...over }));

const ids = (list: AgentSession[]) => list.map((s) => s.id);

describe("grouping", () => {
  const platform = "/home/me/g-platform";
  const security: SidebarWorkspace = {
    id: "ws-security",
    name: "fsap-security",
    paths: [`${platform}/kong-plugin`, `${platform}/auth-service`],
  };

  it("files a session by its workspace, then by folder, and scratch last", () => {
    const groups = groupSessions(
      [
        session("a", { projectPath: `${platform}/kong-plugin`, workspaceId: security.id }),
        session("b", { projectPath: `${platform}/auth-service`, workspaceId: security.id }),
        session("c", { projectPath: "/work/zeta" }),
        session("d", { projectPath: "/cfg/scratch/20260101-1-x" }),
      ],
      "/cfg/scratch",
      [security],
    );
    expect(groups.map((g) => [g.kind, g.label, g.sessions.length])).toEqual([
      ["workspace", "fsap-security", 2],
      ["folder", "zeta", 1],
      ["scratch", "No project", 1],
    ]);
  });

  it("keys groups the way the tree did, so a folded group stays folded", () => {
    const [ws, folder, scratch] = groupSessions(
      [
        session("a", { projectPath: "/p/kong", workspaceId: security.id }),
        session("c", { projectPath: "/work/zeta" }),
        session("d", { projectPath: "/cfg/scratch/x" }),
      ],
      "/cfg/scratch",
      [security],
    );
    expect(ws.key).toBe("workspace:ws-security");
    expect(folder.key).toBe("project:/work/zeta");
    expect(scratch.key).toBe("project:/cfg/scratch");
  });

  it("never groups by display name: two folders called api stay two groups", () => {
    const groups = groupSessions([session("a", { projectPath: "/old/api" }), session("b", { projectPath: "/new/api" })]);
    expect(groups).toHaveLength(2);
    expect(groups.map((g) => g.label).sort()).toEqual(["new/api", "old/api"]);
    expect(new Set(groups.map((g) => g.key)).size).toBe(2);
  });

  it("a session belongs only to its chosen workspace, even when its folder is shared", () => {
    const other: SidebarWorkspace = { id: "ws-other", name: "other", paths: [`${platform}/kong-plugin`] };
    const groups = groupSessions(
      [session("k", { projectPath: `${platform}/kong-plugin`, workspaceId: security.id })],
      "",
      [other, security],
    );
    expect(groups.map((g) => g.label)).toEqual(["fsap-security"]);
  });

  it("keeps a child in its parent's group but as a card of its own", () => {
    const groups = groupSessions([
      session("root", { projectPath: "/work/a" }),
      session("kid", { projectPath: "/work/elsewhere", parentSessionId: "root", depth: 1 }),
    ]);
    expect(groups).toHaveLength(1);
    expect(ids(groups[0].sessions)).toEqual(["root", "kid"]);
  });

  it("files a child whose parent is gone as a root of its own project", () => {
    const groups = groupSessions([session("kid", { projectPath: "/work/b", parentSessionId: "deleted", depth: 1 })]);
    expect(groups.map((g) => g.label)).toEqual(["b"]);
  });

  it("survives a parent cycle", () => {
    const groups = groupSessions([
      session("a", { parentSessionId: "b" }),
      session("b", { parentSessionId: "a" }),
    ]);
    expect(groups[0].sessions).toHaveLength(2);
  });

  it("hides closed sessions except the one on screen", () => {
    const list = [session("a", { status: "closed" }), session("b", { status: "closed" }), session("c")];
    const drawn = (hide: boolean, active?: string) =>
      groupSessions(list, "", [], drawnWhenHidingClosed(hide, active)).flatMap((g) => ids(g.sessions));
    expect(drawn(true, "b")).toEqual(["b", "c"]);
    expect(drawn(false)).toEqual(["a", "b", "c"]);
  });

  it("hiding closed sessions never moves a live child to another group", () => {
    // The parent is closed and belongs to workspace A; its child was opened in
    // workspace B. Membership follows the lineage root, so the child is in A —
    // and stays there when the closed parent is left out of what is drawn.
    const a: SidebarWorkspace = { id: "wa", name: "A", paths: ["/a"] };
    const b: SidebarWorkspace = { id: "wb", name: "B", paths: ["/b"] };
    const list = [
      session("parent", { status: "closed", projectPath: "/a", workspaceId: "wa" }),
      session("child", { parentSessionId: "parent", depth: 1, projectPath: "/b", workspaceId: "wb" }),
    ];
    const groupOfChild = (keep?: (s: AgentSession) => boolean) =>
      groupSessions(list, "", [a, b], keep).find((g) => g.sessions.some((s) => s.id === "child"))?.label;
    expect(groupOfChild()).toBe("A");
    expect(groupOfChild(drawnWhenHidingClosed(true))).toBe("A");
  });

  it("omits a group with nothing left to draw", () => {
    const list = [session("a", { status: "closed", projectPath: "/work/x" }), session("b", { projectPath: "/work/y" })];
    expect(groupSessions(list, "", [], drawnWhenHidingClosed(true)).map((g) => g.label)).toEqual(["y"]);
  });
});

describe("recency order", () => {
  it("is newest first, whatever order they arrive in", () => {
    const list = [session("old", { updatedAt: 1 }), session("new", { updatedAt: 3 }), session("mid", { updatedAt: 2 })];
    expect(ids([...list].sort(compareRecent))).toEqual(["new", "mid", "old"]);
    expect(ids([...list].reverse().sort(compareRecent))).toEqual(["new", "mid", "old"]);
  });

  it("breaks ties by id, so equal times never swap places between renders", () => {
    const list = [session("b", { updatedAt: 5 }), session("c", { updatedAt: 5 }), session("a", { updatedAt: 5 })];
    expect(ids([...list].sort(compareRecent))).toEqual(["a", "b", "c"]);
    expect(ids([...list].reverse().sort(compareRecent))).toEqual(["a", "b", "c"]);
  });

  it("treats an unknown time as oldest", () => {
    const list = [session("x"), session("y", { updatedAt: 9 })];
    expect(ids([...list].sort(compareRecent))).toEqual(["y", "x"]);
  });

  it("does not move when only the status text changes: order reads updatedAt, which chunks do not touch", () => {
    const before = sessionsAged(3);
    const streaming = before.map((s) => (s.id === "s2" ? { ...s, status: "running" as const } : s));
    expect(ids(partitionGroup(streaming).shown)).toEqual(ids(partitionGroup(before).shown));
  });
});

describe("five recent and the stack", () => {
  it("shows five and folds the rest, counting exactly what is hidden", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(8));
    expect(RECENT_LIMIT).toBe(5);
    expect(ids(shown)).toEqual(["s0", "s1", "s2", "s3", "s4"]);
    expect(ids(stacked)).toEqual(["s5", "s6", "s7"]);
  });

  it("has no stack at five or fewer", () => {
    expect(partitionGroup(sessionsAged(5)).stacked).toEqual([]);
    expect(partitionGroup(sessionsAged(1)).shown).toHaveLength(1);
    expect(partitionGroup([]).shown).toEqual([]);
  });

  it("every session is drawn exactly once or stacked exactly once", () => {
    const list = sessionsAged(23);
    const { shown, stacked } = partitionGroup(list, {
      activeId: "s20",
      pinnedIds: ["s15", "s2"],
      waiting: (s) => s.id === "s18",
    });
    const all = [...ids(shown), ...ids(stacked)];
    expect(all.sort()).toEqual(ids(list).sort());
    expect(new Set(all).size).toBe(list.length);
  });

  it("the session on screen is drawn even when it is old, and counts as an extra", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(8), { activeId: "s7" });
    expect(ids(shown)).toEqual(["s0", "s1", "s2", "s3", "s4", "s7"]);
    expect(ids(stacked)).toEqual(["s5", "s6"]);
  });

  it("a session waiting on the person is drawn even when it is old", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(8), { waiting: (s) => s.id === "s6" });
    expect(ids(shown)).toContain("s6");
    expect(ids(stacked)).toEqual(["s5", "s7"]);
  });

  it("a pinned session goes first, in pin order, and never hides", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(8), { pinnedIds: ["s7", "s3"] });
    expect(ids(shown)).toEqual(["s7", "s3", "s0", "s1", "s2", "s4"]);
    expect(ids(stacked)).toEqual(["s5", "s6"]);
  });

  it("one session in two exception sets, or an exception inside the five, is not duplicated", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(8), {
      activeId: "s6",
      pinnedIds: ["s6", "s1"],
      waiting: (s) => s.id === "s6" || s.id === "s2",
    });
    expect(ids(shown).filter((id) => id === "s6")).toHaveLength(1);
    expect(ids(shown).filter((id) => id === "s1")).toHaveLength(1);
    // The five recent are still drawn, plus the one old exception: six cards.
    expect(shown).toHaveLength(6);
    expect(ids(stacked)).toEqual(["s5", "s7"]);
  });

  it("running is not an exception, or a group of running agents would never fold", () => {
    const { shown, stacked } = partitionGroup(sessionsAged(12, { status: "running" }));
    expect(shown).toHaveLength(5);
    expect(stacked).toHaveLength(7);
  });

  it("an old session that starts waiting appears without pushing another into the stack", () => {
    const list = sessionsAged(9);
    const before = partitionGroup(list);
    const after = partitionGroup(list, { waiting: (s) => s.id === "s8" });
    // Everything drawn before is still drawn, in the same relative order.
    expect(ids(after.shown).filter((id) => ids(before.shown).includes(id))).toEqual(ids(before.shown));
    expect(ids(after.shown)).toEqual([...ids(before.shown), "s8"]);
    expect(after.stacked).toHaveLength(before.stacked.length - 1);
  });

  it("an exception sits where its age puts it, so its position is the same each render", () => {
    const options = { waiting: (s: AgentSession) => s.id === "s7" };
    const a = partitionGroup(sessionsAged(9), options);
    const b = partitionGroup([...sessionsAged(9)].reverse(), options);
    expect(ids(a.shown)).toEqual(ids(b.shown));
    expect(ids(a.shown)[ids(a.shown).length - 1]).toBe("s7");
  });

  it("honours a different limit", () => {
    expect(partitionGroup(sessionsAged(6), { limit: 2 }).stacked).toHaveLength(4);
  });
});

describe("what needs the person", () => {
  const permission = { sessionId: "p" };
  const question = { sessionId: "q" };

  it("counts permissions and questions apart, per session", () => {
    expect(attentionBySession([permission, permission], [question, { sessionId: "p" }])).toEqual({
      p: { permissions: 2, questions: 1 },
      q: { permissions: 0, questions: 1 },
    });
  });

  it("reads the queue, not only session.status", () => {
    const running = session("r", { status: "running" });
    expect(attentionKind(running)).toBeNull();
    // A queued permission while status still says running.
    expect(attentionKind(running, { permissions: 1, questions: 0 })).toBe("permission");
    // A question never changes status: the turn is still running.
    expect(attentionKind(running, { permissions: 0, questions: 1 })).toBe("question");
    expect(attentionKind(session("w", { status: "awaiting_permission" }))).toBe("permission");
  });

  it("a permission outranks a question", () => {
    expect(attentionKind(session("x"), { permissions: 1, questions: 2 })).toBe("permission");
    expect(cardIndicator(session("x"), { permissions: 1, questions: 2 })).toBe("permission");
  });

  it("shows one glyph for running, waiting or failed, and none for idle or closed", () => {
    expect(cardIndicator(session("a", { status: "running" }))).toBe("running");
    expect(cardIndicator(session("a", { status: "error" }))).toBe("error");
    expect(cardIndicator(session("a", { status: "idle" }))).toBeNull();
    expect(cardIndicator(session("a", { status: "closed" }))).toBeNull();
    expect(cardIndicator(session("a", { status: "" }))).toBeNull();
    // Waiting beats running and error: it is the one with a person behind it.
    expect(cardIndicator(session("a", { status: "error" }), { permissions: 0, questions: 1 })).toBe("question");
  });

  it("walks on to the next waiting session, wrapping", () => {
    expect(nextWaiting(["a", "b", "c"], "b")).toBe("c");
    expect(nextWaiting(["a", "b", "c"], "c")).toBe("a");
    expect(nextWaiting(["a", "b"], "elsewhere")).toBe("a");
    expect(nextWaiting([], "a")).toBeUndefined();
  });
});

describe("short ages", () => {
  const now = 1_000_000_000_000;
  const ago = (ms: number) => shortAge(now - ms, now);
  it("is the shortest honest unit", () => {
    expect(ago(5_000)).toBe("now");
    expect(ago(10 * 60_000)).toBe("10m");
    expect(ago(2 * 3_600_000)).toBe("2h");
    expect(ago(3 * 86_400_000)).toBe("3d");
    expect(ago(14 * 86_400_000)).toBe("2w");
    expect(ago(90 * 86_400_000)).toBe("3mo");
    expect(ago(800 * 86_400_000)).toBe("2y");
  });
  it("says nothing when the time is unknown, and never goes negative", () => {
    expect(shortAge(undefined, now)).toBe("");
    expect(shortAge(0, now)).toBe("");
    expect(shortAge(now + 60_000, now)).toBe("now");
  });
});

describe("subagents", () => {
  const agents = [
    { id: "a", name: "Review whitelist", state: "completed" },
    { id: "b", name: "Java fixes" },
    { id: "c", name: "Flaky test", state: "failed" },
    { id: "d", name: "Docs" },
  ];

  it("puts running first, then failed, then done, each in the order it came", () => {
    expect(orderSubagents(agents).map((a) => a.id)).toEqual(["b", "d", "c", "a"]);
  });

  it("names every subagent and its state, not only the ones that fit", () => {
    expect(subagentsLabel(agents)).toBe(
      "4 subagents: Java fixes, running; Docs, running; Flaky test, failed; Review whitelist, done",
    );
    expect(subagentsLabel([{ id: "z1234567", name: "" }])).toBe("1 subagent: Subagent z12345, running");
    expect(subagentsLabel([])).toBe("");
  });
});

describe("the card's sentence", () => {
  it("says in words what the icons say", () => {
    const parts = cardSummary({
      vendorName: "Claude",
      session: session("a", { status: "awaiting_permission" }),
      attention: { permissions: 2, questions: 1 },
      subagents: [{ id: "x", name: "Docs" }],
      pinned: true,
      age: "10m",
      parentTitle: "Planner",
    });
    expect(parts).toEqual([
      "Claude",
      "Waiting for permission (2 requests)",
      "Has a question for you",
      "1 subagent: Docs, running",
      "Child of Planner",
      "Pinned",
      "Active 10m ago",
    ]);
  });

  it("is quiet for an idle session with nothing attached", () => {
    expect(
      cardSummary({ vendorName: "Codex", session: session("a"), subagents: [], pinned: false, age: "" }),
    ).toEqual(["Codex"]);
    expect(
      cardSummary({ vendorName: "Codex", session: session("a", { status: "closed" }), subagents: [], pinned: false, age: "now" }),
    ).toEqual(["Codex", "Closed", "Active just now"]);
  });
});

describe("needsYou", () => {
  it("waiting or failed sessions, newest first", () => {
    const list = [
      session("a", { status: "idle", updatedAt: 1 }),
      session("b", { status: "error", updatedAt: 3 }),
      session("c", { status: "running", updatedAt: 2 }),
      session("d", { status: "idle", updatedAt: 4 }),
    ];
    const attention = { c: { permissions: 1, questions: 0 } };
    expect(needsYou(list, attention).map((x) => x.id)).toEqual(["b", "c"]);
  });

  it("closed sessions never need you", () => {
    expect(needsYou([session("a", { status: "closed" })], { a: { permissions: 1, questions: 0 } })).toEqual([]);
  });
});

describe("secondLine", () => {
  it("a pending ask wins and is a warning", () => {
    expect(secondLine({ session: session("a", { status: "running", preview: "old" }), ask: "Allow migrate up?" })).toEqual({
      text: "Allow migrate up?",
      tone: "warning",
    });
  });

  it("an error shows the preview as danger", () => {
    expect(secondLine({ session: session("a", { status: "error", preview: "Usage limit." }), ask: null })).toEqual({
      text: "Usage limit.",
      tone: "danger",
    });
  });

  it("otherwise the preview, plain", () => {
    expect(secondLine({ session: session("a", { preview: "Done." }), ask: null })).toEqual({
      text: "Done.",
      tone: "plain",
    });
  });

  it("nothing to say", () => {
    expect(secondLine({ session: session("a"), ask: null })).toEqual({ text: "", tone: "plain" });
  });
});

describe("groupCollapsed", () => {
  it("the current workspace is open unless collapsed", () => {
    expect(groupCollapsed("workspace:1", "1", new Set(), new Set())).toBe(false);
    expect(groupCollapsed("workspace:1", "1", new Set(["workspace:1"]), new Set())).toBe(true);
  });

  it("other groups are collapsed unless expanded", () => {
    expect(groupCollapsed("workspace:2", "1", new Set(), new Set())).toBe(true);
    expect(groupCollapsed("workspace:2", "1", new Set(), new Set(["workspace:2"]))).toBe(false);
    expect(groupCollapsed("project:/x", "1", new Set(), new Set())).toBe(true);
  });

  it("with no current workspace every group follows the collapsed list", () => {
    expect(groupCollapsed("workspace:2", null, new Set(), new Set())).toBe(false);
  });
});

describe("askBySession", () => {
  it("a permission's title wins over a question's message", () => {
    expect(askBySession([{ sessionId: "a", title: "Allow?" }], [{ sessionId: "a", message: "Which?" }])).toEqual({ a: "Allow?" });
  });

  it("a question alone gives its message", () => {
    expect(askBySession([], [{ sessionId: "a", message: "Which?" }])).toEqual({ a: "Which?" });
  });

  it("the first permission of a session is the one said", () => {
    expect(
      askBySession(
        [
          { sessionId: "a", title: "First?" },
          { sessionId: "a", title: "Second?" },
        ],
        [],
      ),
    ).toEqual({ a: "First?" });
  });

  it("the first question of a session is the one said", () => {
    expect(
      askBySession([], [
        { sessionId: "a", message: "First?" },
        { sessionId: "a", message: "Second?" },
      ]),
    ).toEqual({ a: "First?" });
  });
});

describe("plainAsk", () => {
  it("strips code ticks, bold and underscores emphasis, and folds whitespace", () => {
    expect(plainAsk("Run `rm -rf build`?")).toBe("Run rm -rf build?");
    expect(plainAsk("Allow **write** to  __a.ts__\n  now?")).toBe("Allow write to a.ts now?");
    expect(plainAsk("  plain  ")).toBe("plain");
  });

  it("leaves emphasis markers inside paths, globs and identifiers alone", () => {
    expect(plainAsk("Edit src/__init__.py")).toBe("Edit src/__init__.py");
    expect(plainAsk("rm -rf **/node_modules")).toBe("rm -rf **/node_modules");
    expect(plainAsk("src/**/*.ts")).toBe("src/**/*.ts");
    expect(plainAsk("**Allow** this?")).toBe("Allow this?");
    expect(plainAsk("Run __this__, now")).toBe("Run this, now");
  });

  it("is applied where asks are built", () => {
    expect(askBySession([{ sessionId: "a", title: "Run `rm -rf build`?" }], [])).toEqual({ a: "Run rm -rf build?" });
    expect(askBySession([], [{ sessionId: "a", message: "Use **npm** or `bun`?" }])).toEqual({ a: "Use npm or bun?" });
  });
});
