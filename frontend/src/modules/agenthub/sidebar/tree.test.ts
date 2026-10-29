import { describe, expect, it } from "bun:test";
import type { AgentSession } from "@/bridge/agenthub";
import {
  ancestorsOf,
  branchHint,
  branchHintLabel,
  branchOpen,
  branchSignals,
  buildForest,
  flattenNode,
  heldBranches,
  indented,
  MIN_VISIBLE_CHILDREN,
  partitionForest,
  type SessionNode,
  visibleChildren,
} from "./tree";

const session = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/work/api",
  workspaceId: "",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: `Title ${id}`,
  acpSessionId: "",
  updatedAt: 0,
  ...over,
});
const kid = (id: string, parent: string, over: Partial<AgentSession> = {}) =>
  session(id, { parentSessionId: parent, depth: 1, ...over });

const shape = (nodes: readonly SessionNode[]): unknown[] =>
  nodes.map((n) => (n.children.length ? [n.session.id, shape(n.children)] : n.session.id));

describe("buildForest", () => {
  it("nests a child under its parent, newest child first, at the right level", () => {
    const forest = buildForest([
      session("root", { updatedAt: 5 }),
      kid("old", "root", { updatedAt: 1 }),
      kid("new", "root", { updatedAt: 3 }),
      kid("grand", "new", { updatedAt: 2, depth: 2 }),
    ]);
    expect(shape(forest)).toEqual([["root", [["new", ["grand"]], "old"]]]);
    const root = forest[0];
    expect(root.level).toBe(0);
    expect(root.children[0].level).toBe(1);
    expect(root.children[0].children[0].level).toBe(2);
    expect(root.descendants).toBe(3);
    expect(root.children[0].descendants).toBe(1);
  });

  it("does not depend on the order the list arrived in", () => {
    const list = [kid("b", "a", { updatedAt: 2 }), session("a", { updatedAt: 9 }), kid("c", "a", { updatedAt: 3 })];
    expect(shape(buildForest(list))).toEqual(shape(buildForest([...list].reverse())));
  });

  it("a child whose parent is not in the list is a root, not dropped", () => {
    const forest = buildForest([kid("orphan", "gone", { updatedAt: 1 }), session("other", { updatedAt: 2 })]);
    expect(forest.map((n) => n.session.id)).toEqual(["other", "orphan"]);
    expect(forest.every((n) => n.level === 0)).toBe(true);
  });

  it("a cycle loses no session and does not loop", () => {
    const forest = buildForest([
      session("a", { parentSessionId: "b", updatedAt: 1 }),
      session("b", { parentSessionId: "a", updatedAt: 2 }),
      session("c", { parentSessionId: "c", updatedAt: 3 }),
    ]);
    const ids = forest.flatMap(flattenNode).map((n) => n.session.id);
    expect(ids.sort()).toEqual(["a", "b", "c"]);
    // The newest member of the unreachable cycle is the root; the other hangs under it.
    expect(shape(forest)).toContainEqual(["b", ["a"]]);
    expect(forest.find((n) => n.session.id === "c")?.children).toEqual([]);
  });

  it("a root is as recent as the newest session in its tree", () => {
    const forest = buildForest([session("p", { updatedAt: 1 }), kid("k", "p", { updatedAt: 50 })]);
    expect(forest[0].recent).toBe(50);
    expect(forest[0].session.updatedAt).toBe(1);
  });

  it("empty list, empty forest", () => {
    expect(buildForest([])).toEqual([]);
  });
});

describe("ancestors, held branches and indentation", () => {
  const forest = buildForest([
    session("r"),
    kid("a", "r"),
    kid("b", "a", { depth: 2 }),
    kid("c", "b", { depth: 3 }),
    session("other"),
  ]);

  it("the path above a session, nearest last, excluding itself", () => {
    expect(ancestorsOf(forest, "c")).toEqual(["r", "a", "b"]);
    expect(ancestorsOf(forest, "r")).toEqual([]);
    expect(ancestorsOf(forest, "nope")).toEqual([]);
  });

  it("holds every branch above the session on screen and above a pinned one", () => {
    expect([...heldBranches(forest, "b", [])].sort()).toEqual(["a", "r"]);
    expect([...heldBranches(forest, undefined, ["c"])].sort()).toEqual(["a", "b", "r"]);
    expect(heldBranches(forest, "other", []).size).toBe(0);
  });

  it("a held branch stays open whatever was folded; otherwise the fold decides", () => {
    const folded = new Set(["r"]);
    expect(branchOpen("r", folded, true)).toBe(true);
    expect(branchOpen("r", folded, false)).toBe(false);
    expect(branchOpen("a", folded, false)).toBe(true);
  });

  it("indents two levels of children and no more", () => {
    expect([0, 1, 2, 3, 4].map(indented)).toEqual([true, true, false, false, false]);
  });
});

describe("partitionForest: a tree is one entry of its group", () => {
  /** n roots, r0 the newest. */
  const roots = (n: number, over: (i: number) => Partial<AgentSession> = () => ({})) =>
    Array.from({ length: n }, (_, i) => session(`r${i}`, { updatedAt: 1_000_000 - i * 1000, ...over(i) }));

  it("five trees are five entries however many sessions they hold", () => {
    const list = [
      ...roots(7),
      kid("k1", "r0", { updatedAt: 999_000 }),
      kid("k2", "r0", { updatedAt: 998_000 }),
      kid("k3", "r1", { updatedAt: 997_000 }),
    ];
    const { shown, stacked } = partitionForest(buildForest(list));
    expect(shown.map((n) => n.session.id)).toEqual(["r0", "r1", "r2", "r3", "r4"]);
    expect(stacked.map((n) => n.session.id)).toEqual(["r5", "r6"]);
    expect(shown.flatMap(flattenNode)).toHaveLength(8);
  });

  it("a tree is as recent as its newest child, so a running child keeps its parent drawn", () => {
    const list = [...roots(7), kid("busy", "r6", { updatedAt: 5_000_000 })];
    const { shown, stacked } = partitionForest(buildForest(list));
    expect(shown[0].session.id).toBe("r6");
    expect(stacked.map((n) => n.session.id)).toEqual(["r4", "r5"]);
  });

  it("the session on screen keeps its tree drawn, though old and deep", () => {
    const list = [...roots(7), kid("deep", "r6", { updatedAt: 1 })];
    const forest = buildForest(list);
    const view = partitionForest(forest, { activeId: "deep" });
    expect(view.shown.map((n) => n.session.id)).toContain("r6");
    expect(view.stacked.map((n) => n.session.id)).toEqual(["r5"]);
  });

  it("a pinned child keeps its tree drawn but does not move it to the top", () => {
    const list = [...roots(7), kid("pin", "r6", { updatedAt: 1 })];
    const { shown } = partitionForest(buildForest(list), { pinnedIds: ["pin"] });
    const ids = shown.map((n) => n.session.id);
    expect(ids).toContain("r6");
    expect(ids[0]).toBe("r0");
    expect(ids[ids.length - 1]).toBe("r6");
  });

  it("a pinned root goes first", () => {
    const { shown } = partitionForest(buildForest(roots(7)), { pinnedIds: ["r6"] });
    expect(shown[0].session.id).toBe("r6");
  });

  it("every tree is in exactly one of the two lists", () => {
    const list = [...roots(9), kid("k", "r8", { updatedAt: 3 })];
    const { shown, stacked } = partitionForest(buildForest(list), { activeId: "k" });
    const ids = [...shown, ...stacked].map((n) => n.session.id).sort();
    expect(ids).toEqual(Array.from({ length: 9 }, (_, i) => `r${i}`).sort());
  });
});

describe("branchSignals and branchHint", () => {
  const list = [
    session("p"),
    kid("run", "p", { status: "running" }),
    kid("wait", "p", { status: "awaiting_permission" }),
    kid("err", "p", { status: "error" }),
    kid("closed", "p", { status: "closed" }),
    kid("grand", "run", { depth: 2, status: "running" }),
    session("loner"),
  ];

  it("counts what is going on below a session, grandchildren included, closed ones for nothing", () => {
    const signals = branchSignals(list, {}, { grand: 2 });
    expect(signals.p).toEqual({ running: 2, waiting: 1, failed: 1, unread: 1 });
    expect(signals.run).toEqual({ running: 1, waiting: 0, failed: 0, unread: 1 });
    expect(signals.loner).toBeUndefined();
  });

  it("a child in a turn of its own making counts as running; background tasks alone do not", () => {
    const signals = branchSignals(
      [
        session("p"),
        kid("agent", "p", { activity: { agentTurn: true, tasks: 0 } }),
        kid("tasks", "p", { activity: { agentTurn: false, tasks: 4 } }),
      ],
      {},
      {},
    );
    expect(signals.p.running).toBe(1);
  });

  it("a dismissed failure no longer marks the parent; a new one does", () => {
    const failed = (updatedAt: number) => [session("p"), kid("err", "p", { status: "error", updatedAt })];
    expect(branchSignals(failed(10), {}, {}).p.failed).toBe(1);
    expect(branchSignals(failed(10), {}, {}, { err: 10 }).p.failed).toBe(0);
    expect(branchHint(branchSignals(failed(10), {}, {}, { err: 10 }).p, true)).toBeNull();
    expect(branchSignals(failed(11), {}, {}, { err: 10 }).p.failed).toBe(1);
  });

  it("a question counts as waiting though the turn is still running, and is not also running", () => {
    const signals = branchSignals([session("p"), kid("q", "p", { status: "running" })], { q: { permissions: 0, questions: 1 } }, {});
    expect(signals.p).toEqual({ running: 0, waiting: 1, failed: 0, unread: 0 });
  });

  it("survives a cycle", () => {
    const signals = branchSignals(
      [session("a", { parentSessionId: "b", status: "running" }), session("b", { parentSessionId: "a", status: "running" })],
      {},
      {},
    );
    expect(signals.a.running).toBeLessThanOrEqual(2);
    expect(signals.b.running).toBeLessThanOrEqual(2);
  });

  it("waiting and failed children are marked whether the branch is open or not; running and unread only when folded", () => {
    expect(branchHint({ running: 2, waiting: 1, failed: 1, unread: 1 }, true)).toEqual({ kind: "waiting", count: 1 });
    expect(branchHint({ running: 2, waiting: 0, failed: 1, unread: 1 }, true)).toEqual({ kind: "failed", count: 1 });
    expect(branchHint({ running: 2, waiting: 0, failed: 0, unread: 1 }, true)).toBeNull();
    expect(branchHint({ running: 2, waiting: 0, failed: 0, unread: 1 }, false)).toEqual({ kind: "running", count: 2 });
    expect(branchHint({ running: 0, waiting: 0, failed: 0, unread: 3 }, false)).toEqual({ kind: "unread", count: 3 });
    expect(branchHint({ running: 0, waiting: 0, failed: 0, unread: 0 }, false)).toBeNull();
    expect(branchHint(undefined, false)).toBeNull();
  });

  it("says the hint in words", () => {
    expect(branchHintLabel(null)).toBe("");
    expect(branchHintLabel({ kind: "waiting", count: 1 })).toBe("A child session is waiting for you");
    expect(branchHintLabel({ kind: "waiting", count: 3 })).toBe("3 child sessions are waiting for you");
    expect(branchHintLabel({ kind: "unread", count: 2 })).toBe("2 child sessions have new messages");
  });
});

describe("visibleChildren", () => {
  /** The children of one root, newest first (buildForest orders them). */
  const kids = (n: number, over: (i: number) => Partial<AgentSession> = () => ({})) =>
    buildForest([
      session("root", { updatedAt: 1000 }),
      ...Array.from({ length: n }, (_, i) => kid(`k${i}`, "root", { updatedAt: 100 - i, ...over(i) })),
    ])[0].children;
  const ids = (nodes: readonly SessionNode[]) => nodes.map((n) => n.session.id);

  it("keeps the three newest of eleven idle children and hides the rest", () => {
    const { shown, hidden } = visibleChildren(kids(11));
    expect(MIN_VISIBLE_CHILDREN).toBe(3);
    expect(ids(shown)).toEqual(["k0", "k1", "k2"]);
    expect(ids(hidden)).toEqual(["k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10"]);
  });

  it("counts a child in an agent-started turn as running: it stays drawn", () => {
    const { hidden } = visibleChildren(kids(11, (i) => (i >= 5 && i < 10 ? { activity: { agentTurn: true, tasks: 0 } } : {})));
    expect(hidden.length).toBe(11 - 5);
    expect(hidden.map((n) => n.session.id)).not.toContain("k5");
  });

  it("shows every running child when more than three run", () => {
    const { shown, hidden } = visibleChildren(kids(11, (i) => ({ status: i >= 5 && i < 10 ? "running" : "idle" })));
    expect(ids(shown)).toEqual(["k5", "k6", "k7", "k8", "k9"]);
    expect(hidden).toHaveLength(6);
  });

  it("fills up to three with the newest idle ones when fewer than three run", () => {
    const one = visibleChildren(kids(11, (i) => ({ status: i === 7 ? "running" : "idle" })));
    expect(ids(one.shown)).toEqual(["k0", "k1", "k7"]);
    const two = visibleChildren(kids(5, (i) => ({ status: i === 3 || i === 4 ? "running" : "idle" })));
    expect(ids(two.shown)).toEqual(["k0", "k3", "k4"]);
  });

  it("draws the session on screen or a pinned one though it is old, going past the limit", () => {
    const active = visibleChildren(kids(11), { activeId: "k9" });
    expect(ids(active.shown)).toEqual(["k0", "k1", "k2", "k9"]);
    const pinned = visibleChildren(kids(11), { pinned: new Set(["k8", "k10"]) });
    expect(ids(pinned.shown)).toEqual(["k0", "k1", "k2", "k8", "k10"]);
  });

  it("draws the child above a grandchild that is on screen", () => {
    const children = buildForest([
      session("root", { updatedAt: 1000 }),
      ...Array.from({ length: 6 }, (_, i) => kid(`k${i}`, "root", { updatedAt: 100 - i })),
      kid("deep", "k5", { updatedAt: 1, depth: 2 }),
    ])[0].children;
    expect(ids(visibleChildren(children, { activeId: "deep" }).shown)).toEqual(["k0", "k1", "k2", "k5"]);
  });

  it("hides nothing when there are no more children than the limit", () => {
    for (const n of [0, 1, 3]) {
      const { shown, hidden } = visibleChildren(kids(n));
      expect(shown).toHaveLength(n);
      expect(hidden).toHaveLength(0);
    }
    // Four children, one running: limit is still three, so one is hidden.
    expect(visibleChildren(kids(4, (i) => ({ status: i === 0 ? "running" : "idle" }))).hidden).toHaveLength(1);
    // Four running: limit is four, nothing hidden.
    expect(visibleChildren(kids(4, () => ({ status: "running" }))).hidden).toHaveLength(0);
  });

  it("keeps the input order in both lists", () => {
    const children = kids(8, (i) => ({ status: i === 6 ? "running" : "idle" }));
    const { shown, hidden } = visibleChildren(children);
    expect(ids(shown)).toEqual(["k0", "k1", "k6"]);
    expect(ids(hidden)).toEqual(["k2", "k3", "k4", "k5", "k7"]);
  });
});
