import { describe, expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { AppError } from "@/bridge/errors";
import type { Outcome, Review, StaleDiagnosis } from "@/bridge/harness";
import { StaleRepairView, describeStaleEntry, repairing } from "./StaleRepairPanel";
import { StaleRepairController, canRepair, type StaleOps } from "./staleRepairController";

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

const diag = (workspaceId: string, patch: Partial<StaleDiagnosis> = {}): StaleDiagnosis =>
  ({
    workspaceId, profileRevision: 3, profilePresent: true, state: "stale", total: 1,
    entries: [{ kind: "group", name: "pdf", on: false }], ...patch,
  }) as StaleDiagnosis;
const review = (id: string, workspaceId = "A"): Review =>
  ({ id, target: "repair", workspaceId, tier: "widening", summary: `summary ${id}`, changes: [], needsConfirmation: true }) as Review;
const outcome = (patch: Partial<Outcome> = {}): Outcome =>
  ({ status: "applied", target: "repair", commit: "committed", live: false, reloadRequired: true, recovery: "The stale settings were removed.", ...patch }) as Outcome;
const refused = (code: string) => new AppError({ code, message: `${code} refusal`, recoverable: true });
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function rig() {
  const calls: string[] = [];
  const diagnoses: Record<string, Deferred<StaleDiagnosis>[]> = {};
  const prepares: Record<string, Deferred<Review>[]> = {};
  const applies: Record<string, Deferred<Outcome>[]> = {};
  const push = <T>(map: Record<string, Deferred<T>[]>, key: string) => {
    const d = deferred<T>();
    (map[key] ??= []).push(d);
    return d.promise;
  };
  const ops: StaleOps = {
    diagnose: (w) => (calls.push(`diagnose:${w}`), push(diagnoses, w)),
    prepare: (w) => (calls.push(`prepare:${w}`), push(prepares, w)),
    apply: (id) => (calls.push(`apply:${id}`), push(applies, id)),
    cancel: async (id) => {
      calls.push(`cancel:${id}`);
    },
  };
  return { ops, calls, diagnoses, prepares, applies };
}

async function shown(r: ReturnType<typeof rig>, ws = "A", d = diag(ws)) {
  const c = new StaleRepairController(r.ops);
  c.setWorkspace(ws);
  r.diagnoses[ws][0].resolve(d);
  await tick();
  return c;
}

describe("a stale profile has a reachable repair even though no session or normal read is involved", () => {
  it("offers the repair from the diagnosis alone", async () => {
    const r = rig();
    const c = await shown(r);
    expect(canRepair(c.getState())).toBe(true);
    expect(r.calls).toEqual(["diagnose:A"]);
  });

  it("offers nothing for a healthy profile, an unrepairable one or a list too long to review", async () => {
    for (const d of [diag("A", { state: "none", total: 0, entries: [] }), diag("A", { state: "unrepairable", message: "invalid", entries: [], total: 0 }), diag("A", { truncated: true, total: 90 })]) {
      const r = rig();
      const c = await shown(r, "A", d);
      expect(canRepair(c.getState())).toBe(false);
      await c.prepare();
      expect(r.calls).toEqual(["diagnose:A"]);
    }
  });

  it("does not offer a repair when the diagnosis could not be read (a generic failure is not an opening)", async () => {
    const r = rig();
    const c = new StaleRepairController(r.ops);
    c.setWorkspace("A");
    r.diagnoses.A[0].reject(refused("harness.snapshotUnavailable"));
    await tick();
    expect(c.getState().readError).toContain("snapshotUnavailable");
    expect(canRepair(c.getState())).toBe(false);
    await c.prepare();
    expect(r.calls).toEqual(["diagnose:A"]);
  });

  it("keeps the last good diagnosis, but withdraws the offer, when a later read fails", async () => {
    const r = rig();
    const c = await shown(r);
    void c.refresh();
    r.diagnoses.A[1].reject(refused("x"));
    await tick();
    expect(c.getState().diagnosis?.state).toBe("stale");
    expect(canRepair(c.getState())).toBe(false);
  });
});

describe("late answers after the workspace changed", () => {
  it("never shows A's diagnosis for B", async () => {
    const r = rig();
    const c = new StaleRepairController(r.ops);
    c.setWorkspace("A");
    c.setWorkspace("B");
    r.diagnoses.A[0].resolve(diag("A"));
    await tick();
    expect(c.getState().diagnosis).toBeNull();
    r.diagnoses.B[0].resolve(diag("B", { state: "none", total: 0, entries: [] }));
    await tick();
    expect(c.getState().diagnosis?.workspaceId).toBe("B");
  });

  it("withdraws a review prepared for a workspace the panel left", async () => {
    const r = rig();
    const c = await shown(r);
    const preparing = c.prepare();
    c.setWorkspace("B");
    r.diagnoses.B[0].resolve(diag("B"));
    await tick();
    r.prepares.A[0].resolve(review("proposal-A"));
    await preparing;
    expect(c.getState().review).toBeNull();
    expect(c.getState().workspaceId).toBe("B");
    expect(r.calls).toContain("cancel:proposal-A");
  });

  it("writes nothing from A's Apply into B, and does not cancel the open question", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("proposal-A"));
    await p;
    const applying = c.apply();
    c.setWorkspace("B");
    r.diagnoses.B[0].resolve(diag("B", { state: "none", total: 0, entries: [] }));
    await tick();
    r.applies["proposal-A"][0].resolve(outcome());
    await applying;
    expect(c.getState().outcome).toBeNull();
    expect(c.getState().review).toBeNull();
    expect(r.calls).not.toContain("cancel:proposal-A");
  });

  it("closing the panel stops every pending write", async () => {
    const r = rig();
    const c = new StaleRepairController(r.ops);
    c.setWorkspace("A");
    c.dispose();
    r.diagnoses.A[0].resolve(diag("A"));
    await tick();
    expect(c.getState().diagnosis).toBeNull();
  });
});

describe("cancel, refusal and the outcome", () => {
  it("Cancel withdraws the review and changes nothing", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await p;
    await c.back();
    expect(c.getState().review).toBeNull();
    expect(r.calls).toContain("cancel:p1");
    expect(canRepair(c.getState())).toBe(true);
  });

  it("a conflict at Apply shows the refusal, no success, reads again and offers a fresh preparation", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await p;
    const applying = c.apply();
    r.applies.p1[0].reject(refused("harness.profileConflict"));
    await tick();
    r.diagnoses.A[1].resolve(diag("A", { profileRevision: 4, entries: [{ kind: "group", name: "pdf", on: false }, { kind: "group", name: "x", on: true }], total: 2 }));
    await applying;
    const s = c.getState();
    expect(s.error).toContain("profileConflict");
    expect(s.outcome).toBeNull();
    expect(s.review).toBeNull();
    expect(s.diagnosis?.total).toBe(2);
    expect(canRepair(s)).toBe(true);
  });

  it("a saved repair reads the backend again and shows what is now true, not a claim", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await p;
    const applying = c.apply();
    r.applies.p1[0].resolve(outcome());
    await tick();
    expect(c.getState().outcome?.status).toBe("applied");
    r.diagnoses.A[1].resolve(diag("A", { state: "none", total: 0, entries: [], profileRevision: 4 }));
    await applying;
    expect(canRepair(c.getState())).toBe(false);
    expect(c.getState().diagnosis?.profileRevision).toBe(4);
    expect(r.calls.filter((x) => x.startsWith("diagnose:")).length).toBe(2);
  });

  it("an uncertain outcome is kept, never turned into success, and never retried by itself", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await p;
    const applying = c.apply();
    r.applies.p1[0].resolve(outcome({ status: "unknown", commit: "unknown", recovery: "Check the workspace's saved settings again." }));
    await tick();
    r.diagnoses.A[1].resolve(diag("A"));
    await applying;
    expect(c.getState().outcome?.status).toBe("unknown");
    expect(r.calls.filter((x) => x.startsWith("apply:")).length).toBe(1);
    expect(r.calls.filter((x) => x.startsWith("prepare:")).length).toBe(1);
  });

  it("Apply cannot be submitted twice while it waits", async () => {
    const r = rig();
    const c = await shown(r);
    const p = c.prepare();
    r.prepares.A[0].resolve(review("p1"));
    await p;
    const first = c.apply();
    await c.apply();
    expect(r.calls.filter((x) => x.startsWith("apply:")).length).toBe(1);
    r.applies.p1[0].resolve(outcome());
    await tick();
    r.diagnoses.A[1].resolve(diag("A", { state: "none", total: 0, entries: [] }));
    await first;
  });
});

describe("what the person sees", () => {
  const state = (d: StaleDiagnosis | null, patch = {}) => ({ workspaceId: "A", diagnosis: d, review: null, outcome: null, busy: "" as const, error: "", readError: "", ...patch });
  const render = (s: ReturnType<typeof state>) =>
    renderToStaticMarkup(StaleRepairView({ state: s, onPrepare() {}, onBack() {}, onApply() {}, onReload() {}, onClose() {} }));

  it("lists every stale setting with its saved value and states the consequences", () => {
    const d = diag("A", {
      total: 3,
      entries: [
        { kind: "group", name: "pdf", on: false },
        { kind: "toolBudget", name: "gone.tool", bytes: 4096, cut: "headTail" },
        { kind: "groupBudget", name: "ghost", bytes: 3000 },
      ],
    });
    const html = render(state(d));
    for (const want of ["Tool group “pdf”: saved off", "Tool “gone.tool”: result limit saved (size 4096 bytes, cut headTail)", "Tool group “ghost”: result limit saved (size 3000 bytes)",
      "not remembered", "available", "Sessions already running keep", "restore the missing module or build", "Review repair", "Cancel"]) {
      expect(html).toContain(want);
    }
    expect(repairing(state(d))).toBe(true);
  });

  it("shows the review with Cancel and Repair, and no repair offer behind it", () => {
    const html = render(state(diag("A"), { review: review("p1") }));
    expect(html).toContain("summary p1");
    expect(html).toContain("Repair");
    expect(html).not.toContain("Review repair");
  });

  it("shows nothing for a healthy profile and a plain error with Reload for a failed read", () => {
    expect(render(state(diag("A", { state: "none", total: 0, entries: [] })))).not.toContain("Review repair");
    const html = render(state(null, { readError: "boom" }));
    expect(html).toContain("Could not check");
    expect(html).toContain("Reload");
    expect(html).not.toContain("Review repair");
  });

  it("says a profile it cannot repair is not repairable here and offers nothing", () => {
    const html = render(state(diag("A", { state: "unrepairable", message: "The stored profile cannot be read, so it cannot be repaired here.", entries: [], total: 0 })));
    expect(html).toContain("cannot repair");
    expect(html).not.toContain("Review repair");
  });

  it("describes a name from storage as text", () => {
    expect(describeStaleEntry({ kind: "group", name: "<b>x</b>", on: true })).toBe("Tool group “<b>x</b>”: saved on");
    expect(render(state(diag("A", { entries: [{ kind: "group", name: "<img src=x onerror=1>", on: true }] })))).not.toContain("<img");
  });
});
