import { describe, expect, it } from "bun:test";

import { AppError } from "@/bridge/errors";
import type { BudgetPage, Outcome, Review } from "@/bridge/harness";
import { BudgetsController, type BudgetsOps } from "./budgetsController";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}
type Deferred<T> = ReturnType<typeof deferred<T>>;

const page = (workspaceId: string, patch: Partial<BudgetPage> = {}): BudgetPage =>
  ({
    workspaceId, profileRevision: 1, profilePresent: true, defaultBytes: 24576, minBytes: 2048, maxBytes: 65536,
    groups: [{ group: "git", tools: 2, cuttable: 2 }],
    tools: [{ tool: "git.diff", group: "git", definitionBytes: 1, exempt: false }],
    ...patch,
  }) as BudgetPage;
const review = (id: string): Review => ({ id, target: "workspace", workspaceId: "A", tier: "narrowing", summary: id, changes: [], needsConfirmation: true });
const outcome = (patch: Partial<Outcome> = {}): Outcome => ({ status: "applied", commit: "committed", live: false, reloadRequired: true, ...patch }) as Outcome;
const conflict = () => new AppError({ code: "harness.profileConflict", message: "The workspace profile changed. Propose it again.", recoverable: true });
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function rig() {
  const calls: string[] = [];
  const pages: Record<string, Deferred<BudgetPage>[]> = {};
  const prepares: Record<string, Deferred<Review>[]> = {};
  const applies: Record<string, Deferred<Outcome>[]> = {};
  const push = <T>(map: Record<string, Deferred<T>[]>, key: string) => {
    const d = deferred<T>();
    (map[key] ??= []).push(d);
    return d.promise;
  };
  const ops: BudgetsOps = {
    page: (w, g, c) => (calls.push(`page:${w}:${g}:${c}`), push(pages, `${w}:${g}:${c}`)),
    prepare: (w) => (calls.push(`prepare:${w}`), push(prepares, w)),
    apply: (id) => (calls.push(`apply:${id}`), push(applies, id)),
    cancel: async (id) => {
      calls.push(`cancel:${id}`);
    },
  };
  return { ops, calls, pages, prepares, applies };
}

async function loaded(r: ReturnType<typeof rig>, ws = "A", shown = page(ws)) {
  const c = new BudgetsController(r.ops);
  c.setWorkspace(ws);
  r.pages[`${ws}::`][0].resolve(shown);
  await tick();
  return c;
}

describe("a late answer for a workspace the editor left", () => {
  it("never shows A's prepared review for B, and withdraws it", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const preparing = c.prepare();
    expect(c.getState().busy).toBe("prepare");
    c.setWorkspace("B");
    r.pages["B::"][0].resolve(page("B"));
    await tick();
    r.prepares.A[0].resolve(review("proposal-A"));
    await preparing;
    expect(c.getState().review).toBeNull();
    expect(c.getState().busy).toBe("");
    expect(c.getState().workspaceId).toBe("B");
    expect(r.calls).toContain("cancel:proposal-A");
  });

  it("writes no outcome, error or draft from A's Apply into B, and does not cancel it", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const preparing = c.prepare();
    r.prepares.A[0].resolve(review("proposal-A"));
    await preparing;
    const applying = c.apply();
    expect(c.getState().busy).toBe("apply");
    c.setWorkspace("B");
    r.pages["B::"][0].resolve(page("B"));
    await tick();
    expect(r.calls).not.toContain("cancel:proposal-A"); // the open question is not closed from here
    r.applies["proposal-A"][0].resolve(outcome());
    await applying;
    expect(c.getState().outcome).toBeNull();
    expect(c.getState().error).toBe("");
    expect(c.getState().busy).toBe("");
  });

  it("drops a late page for the old workspace or one that names another", async () => {
    const r = rig();
    const c = new BudgetsController(r.ops);
    c.setWorkspace("A");
    c.setWorkspace("B");
    r.pages["A::"][0].resolve(page("A", { profileRevision: 99 }));
    await tick();
    expect(c.getState().page).toBeNull();
    r.pages["B::"][0].resolve(page("A")); // wrong workspace in the answer
    await tick();
    expect(c.getState().page).toBeNull();
  });

  it("does nothing after dispose and withdraws a held review", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const preparing = c.prepare();
    c.dispose();
    r.prepares.A[0].resolve(review("late"));
    await preparing;
    expect(c.getState().review).toBeNull();
    expect(r.calls).toContain("cancel:late");

    const r2 = rig();
    const d = await loaded(r2);
    d.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const p2 = d.prepare();
    r2.prepares.A[0].resolve(review("held"));
    await p2;
    d.dispose();
    expect(r2.calls).toContain("cancel:held");
  });
});

describe("drafts survive a conflict and a failed read", () => {
  it("keeps the draft and rereads after a refusal before the write", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    c.edit("group", "git", "cut", { op: "set", value: "headTail" });
    const preparing = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await preparing;
    const applying = c.apply();
    r.applies.p1[0].reject(conflict());
    await tick();
    r.pages["A::"][1].resolve(page("A", { profileRevision: 2 }));
    await applying;
    const s = c.getState();
    expect(s.error).toContain("changed");
    expect(Object.keys(s.draft).sort()).toEqual(["group:git", "tool:git.diff"]);
    expect(s.review).toBeNull(); // spent: a fresh Prepare is required
    expect(s.page?.profileRevision).toBe(2);
    expect(s.busy).toBe("");
  });

  it("keeps the draft and the last good page when a read fails", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "cut", { op: "set", value: "headTail" });
    const reading = c.refresh();
    r.pages["A::"][1].reject(new AppError({ code: "internal_error", message: "boom", recoverable: true }));
    await reading;
    expect(c.getState().loadError).toBe("boom");
    expect(c.getState().page).not.toBeNull();
    expect(Object.keys(c.getState().draft)).toEqual(["tool:git.diff"]);
  });

  it("a successful apply clears the draft and shows the outcome as the backend reported it", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const preparing = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await preparing;
    const applying = c.apply();
    r.applies.p1[0].resolve(outcome({ live: false }));
    await tick();
    expect(c.getState().outcome?.live).toBe(false);
    expect(c.getState().draft).toEqual({});
    r.pages["A::"][1].resolve(page("A", { profileRevision: 2 }));
    await applying;
  });

  it("cannot edit while an apply is waiting", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    const preparing = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await preparing;
    void c.apply();
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 8192 });
    c.discard();
    expect(c.getState().draft["tool:git.diff"].bytes).toEqual({ op: "set", value: 4096 });
  });
});

describe("the editor follows the backend", () => {
  it("rebases onto an outside change: what became true is dropped, the rest stays", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    c.edit("group", "git", "cut", { op: "set", value: "headTail" });
    const reading = c.refresh();
    // An agent's request saved the same 4096 for git.diff meanwhile.
    r.pages["A::"][1].resolve(page("A", {
      profileRevision: 2,
      tools: [{ tool: "git.diff", group: "git", definitionBytes: 1, exempt: false, toolBytes: 4096 }],
    } as Partial<BudgetPage>));
    await reading;
    expect(Object.keys(c.getState().draft)).toEqual(["group:git"]);
    expect(c.getState().page?.profileRevision).toBe(2);
  });

  it("folds overlapping reads into one in flight and one trailing read", async () => {
    const r = rig();
    const c = await loaded(r);
    const before = r.calls.filter((x) => x.startsWith("page")).length;
    const reads = [c.refresh(), c.refresh(), c.refresh()];
    expect(r.calls.filter((x) => x.startsWith("page")).length).toBe(before + 1);
    r.pages["A::"][1].resolve(page("A"));
    await tick();
    expect(r.calls.filter((x) => x.startsWith("page")).length).toBe(before + 2);
    r.pages["A::"][2].resolve(page("A"));
    await Promise.all(reads);
    expect(r.calls.filter((x) => x.startsWith("page")).length).toBe(before + 2);
  });

  it("loads the next page on request, bound to the filter, and drops a late one after the filter changes", async () => {
    const r = rig();
    const c = await loaded(r, "A", page("A", { nextCursor: "c1" } as Partial<BudgetPage>));
    const more = c.loadMore();
    expect(r.calls).toContain("page:A::c1");
    r.pages["A::c1"][0].resolve(page("A", { tools: [{ tool: "git.log", group: "git", definitionBytes: 1, exempt: false }], nextCursor: "" } as Partial<BudgetPage>));
    await more;
    expect(c.getState().page?.tools?.map((t) => t.tool)).toEqual(["git.diff", "git.log"]);
    expect(c.getState().nextCursor).toBe("");

    const r2 = rig();
    const d = await loaded(r2, "A", page("A", { nextCursor: "c1" } as Partial<BudgetPage>));
    const stale = d.loadMore();
    d.setGroup("db"); // the filter the cursor belongs to is gone
    r2.pages["A::c1"][0].resolve(page("A", { tools: [{ tool: "git.log", group: "git", definitionBytes: 1, exempt: false }] } as Partial<BudgetPage>));
    await stale;
    expect(d.getState().page).toBeNull();
    expect(r2.calls).toContain("page:A:db:");
  });

  it("changing the filter keeps the draft", async () => {
    const r = rig();
    const c = await loaded(r);
    c.edit("tool", "git.diff", "bytes", { op: "set", value: 4096 });
    c.setGroup("db");
    expect(Object.keys(c.getState().draft)).toEqual(["tool:git.diff"]);
  });
});
