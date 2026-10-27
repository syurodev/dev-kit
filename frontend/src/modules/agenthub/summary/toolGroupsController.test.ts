import { describe, expect, it } from "bun:test";

import { AppError } from "@/bridge/errors";
import type { Outcome, ProfileView, Review, SessionState } from "@/bridge/harness";
import { keepFresh, ToolGroupsController, type FreshnessEnv, type ToolGroupsOps } from "./toolGroupsController";

// Behavior with deferred promises: the test decides when each backend answer
// arrives, so a slow answer for a session the card has left is real, not simulated
// by markup.

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

const session = (patch: Partial<SessionState> = {}): SessionState =>
  ({ state: "current", live: true, active: true, paused: false, pending: false, reloadRequired: false, newSessionRequired: false, overrides: [], ...patch }) as SessionState;
const view = (sessionId: string, patch: Partial<SessionState> = {}, groups = []): ProfileView =>
  ({ workspaceId: "w", sessionId, profileRevision: 1, profilePresent: true, groups, session: session(patch) }) as ProfileView;
const review = (id: string): Review => ({ id, target: "session", workspaceId: "w", tier: "narrowing", summary: id, changes: [], needsConfirmation: true });
const outcome = (patch: Partial<Outcome> = {}): Outcome => ({ status: "applied", commit: "committed", live: true, ...patch }) as Outcome;

const A = { workspaceId: "w", sessionId: "A" };
const B = { workspaceId: "w", sessionId: "B" };

/** Ops whose every call waits for the test; calls are recorded in order. */
function rig() {
  const calls: string[] = [];
  const profiles: Record<string, Deferred<ProfileView>[]> = {};
  const prepares: Record<string, Deferred<Review>[]> = {};
  const applies: Record<string, Deferred<Outcome>[]> = {};
  const reconciles: Record<string, Deferred<Outcome>[]> = {};
  const reloads: Record<string, Deferred<void>[]> = {};
  const push = <T>(map: Record<string, Deferred<T>[]>, key: string) => {
    const d = deferred<T>();
    (map[key] ??= []).push(d);
    return d.promise;
  };
  const ops: ToolGroupsOps = {
    profile: (_w, s) => (calls.push(`profile:${s}`), push(profiles, s)),
    prepareRevoke: (_w, s) => (calls.push(`prepare:${s}`), push(prepares, s)),
    apply: (id) => (calls.push(`apply:${id}`), push(applies, id)),
    cancel: async (id) => {
      calls.push(`cancel:${id}`);
    },
    reconcile: (_w, s) => (calls.push(`reconcile:${s}`), push(reconciles, s)),
    reload: (s) => (calls.push(`reload:${s}`), push(reloads, s)),
  };
  return { ops, calls, profiles, prepares, applies, reconciles, reloads };
}
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

async function pointed(r: ReturnType<typeof rig>, target = A, shown = view(target.sessionId)) {
  const c = new ToolGroupsController(r.ops);
  c.setTarget(target);
  r.profiles[target.sessionId][0].resolve(shown);
  await tick();
  return c;
}

describe("a late answer for the session the card left", () => {
  it("never puts A's prepared review on B, and withdraws it", async () => {
    const r = rig();
    const c = await pointed(r);
    const revoking = c.revoke("db");
    expect(c.getState().busy).toBe("prepare");
    c.setTarget(B); // the person switches while prepareRevoke(A) is pending
    expect(c.getState().busy).toBe("");
    r.profiles.B[0].resolve(view("B"));
    await tick();
    r.prepares.A[0].resolve(review("proposal-A"));
    await revoking;

    expect(c.getState().review).toBeNull();
    expect(c.getState().busy).toBe("");
    expect(c.getState().error).toBe("");
    expect(r.calls).toContain("cancel:proposal-A"); // not left waiting in the backend
    // B applies nothing it was not shown: with no review of its own, Apply is a no-op.
    await c.apply();
    expect(r.calls.some((x) => x === "apply:proposal-A")).toBe(false);
  });

  it("keeps B's own review and busy state when A's prepare fails late", async () => {
    const r = rig();
    const c = await pointed(r);
    const revokingA = c.revoke("db");
    c.setTarget(B);
    r.profiles.B[0].resolve(view("B"));
    await tick();
    const revokingB = c.revoke("git");
    expect(c.getState().busy).toBe("prepare");
    r.prepares.A[0].reject(new AppError({ code: "validation_failed", message: "A failed", recoverable: true }));
    await revokingA;
    expect(c.getState().busy).toBe("prepare"); // still B's operation
    expect(c.getState().error).toBe("");
    r.prepares.B[0].resolve(review("proposal-B"));
    await revokingB;
    expect(c.getState().review?.id).toBe("proposal-B");
    expect(c.getState().busy).toBe("");
  });

  it("writes no outcome, error or busy from A's Apply into B", async () => {
    const r = rig();
    const c = await pointed(r);
    const prepared = c.revoke("db");
    r.prepares.A[0].resolve(review("proposal-A"));
    await prepared;
    const applying = c.apply();
    expect(c.getState().busy).toBe("apply");
    c.setTarget(B); // the confirmation is still open when the person moves on
    r.profiles.B[0].resolve(view("B"));
    await tick();
    expect(c.getState().busy).toBe("");
    r.applies["proposal-A"][0].resolve(outcome({ status: "paused" as Outcome["status"], live: false }));
    await applying;

    expect(c.getState().outcome).toBeNull();
    expect(c.getState().error).toBe("");
    expect(c.getState().review).toBeNull();
    // The card claims nothing about A: what A's change did is read back when A is shown again.
    expect(r.calls).not.toContain("cancel:proposal-A"); // a spent proposal is not cancelled
  });

  it("drops a failed Apply of the old session too", async () => {
    const r = rig();
    const c = await pointed(r);
    const prepared = c.revoke("db");
    r.prepares.A[0].resolve(review("proposal-A"));
    await prepared;
    const applying = c.apply();
    c.setTarget(B);
    r.profiles.B[0].resolve(view("B"));
    await tick();
    r.applies["proposal-A"][0].reject(new AppError({ code: "validation_failed", message: "denied", recoverable: true }));
    await applying;
    expect(c.getState().error).toBe("");
  });

  it("drops a late Reconcile and a late Reload", async () => {
    for (const kind of ["reconcile", "reload"] as const) {
      const r = rig();
      const c = await pointed(r, A, view("A", { paused: true, reloadRequired: true }));
      const acting = kind === "reconcile" ? c.reconcile() : c.reload();
      expect(c.getState().busy).toBe(kind);
      c.setTarget(B);
      r.profiles.B[0].resolve(view("B"));
      await tick();
      if (kind === "reconcile") {
        r.reconciles.A[0].resolve(outcome());
      } else {
        r.reloads.A[0].resolve();
      }
      await acting;
      expect(c.getState().outcome).toBeNull();
      expect(c.getState().busy).toBe("");
      // And B is free to act on its own.
      const second = kind === "reconcile" ? c.reconcile() : c.reload();
      expect(c.getState().busy).toBe(kind);
      void second;
    }
  });

  it("drops a read that arrives for the old session or names another one", async () => {
    const r = rig();
    const c = new ToolGroupsController(r.ops);
    c.setTarget(A);
    c.setTarget(B);
    r.profiles.A[0].resolve(view("A", { overrides: ["db"] }));
    await tick();
    expect(c.getState().view).toBeNull();
    r.profiles.B[0].resolve(view("A")); // an answer about the wrong session
    await tick();
    expect(c.getState().view).toBeNull();
    r.profiles.B[1]?.resolve(view("B"));
  });

  it("withdraws a held review when the card leaves its session, and not on the same target", async () => {
    const r = rig();
    const c = await pointed(r);
    const prepared = c.revoke("db");
    r.prepares.A[0].resolve(review("proposal-A"));
    await prepared;
    c.setTarget(A); // same target: nothing changes
    expect(c.getState().review?.id).toBe("proposal-A");
    c.setTarget(B);
    expect(c.getState().review).toBeNull();
    expect(r.calls).toContain("cancel:proposal-A");
  });

  it("does nothing after dispose", async () => {
    const r = rig();
    const c = await pointed(r);
    const revoking = c.revoke("db");
    c.dispose();
    r.prepares.A[0].resolve(review("proposal-A"));
    await revoking;
    expect(c.getState().review).toBeNull();
    expect(r.calls).toContain("cancel:proposal-A");
  });
});

describe("the card follows the backend, not itself", () => {
  it("reads outside changes: a publication failure, a new override, a reconcile elsewhere", async () => {
    const r = rig();
    const c = await pointed(r, A, view("A")); // live, no overrides: no Revoke, no Reconcile
    expect(c.getState().view?.session?.live).toBe(true);

    // An agent's request added an override and the publication failed.
    const read = c.refresh();
    r.profiles.A[1].resolve(view("A", { live: false, paused: true, overrides: ["db"] }));
    await read;
    expect(c.getState().view?.session).toMatchObject({ live: false, paused: true, overrides: ["db"] });

    // Someone reconciled it elsewhere.
    const again = c.refresh();
    r.profiles.A[2].resolve(view("A", { live: true, paused: false, overrides: ["db"] }));
    await again;
    expect(c.getState().view?.session).toMatchObject({ live: true, paused: false });
  });

  it("never invents Live: an outcome does not change the view until the backend says so", async () => {
    const r = rig();
    const c = await pointed(r, A, view("A", { live: false, paused: true }));
    const acting = c.reconcile();
    r.reconciles.A[0].resolve(outcome({ live: true }));
    await tick();
    expect(c.getState().outcome?.live).toBe(true);
    expect(c.getState().view?.session?.live).toBe(false); // until the read below answers
    r.profiles.A[1].resolve(view("A", { live: false, paused: true }));
    await acting;
    expect(c.getState().view?.session?.live).toBe(false);
  });

  it("drops a paused notice once the backend no longer shows a pause", async () => {
    const r = rig();
    const c = await pointed(r, A, view("A", { live: false, paused: true }));
    const acting = c.reconcile();
    r.reconciles.A[0].resolve(outcome({ status: "paused" as Outcome["status"], live: false }));
    await tick();
    expect(c.getState().outcome?.status).toBe("paused" as Outcome["status"]);
    r.profiles.A[1].resolve(view("A", { live: true, paused: false }));
    await acting;
    expect(c.getState().outcome).toBeNull();
  });

  it("folds overlapping reads into one in flight and one trailing read", async () => {
    const r = rig();
    const c = await pointed(r);
    const before = r.calls.filter((x) => x.startsWith("profile")).length;
    const reads = [c.refresh(), c.refresh(), c.refresh(), c.refresh()];
    expect(r.calls.filter((x) => x.startsWith("profile")).length).toBe(before + 1);
    r.profiles.A[1].resolve(view("A", { overrides: ["db"] }));
    await tick();
    expect(r.calls.filter((x) => x.startsWith("profile")).length).toBe(before + 2); // exactly one trailing read
    r.profiles.A[2].resolve(view("A", { overrides: ["db", "git"] }));
    await Promise.all(reads);
    expect(r.calls.filter((x) => x.startsWith("profile")).length).toBe(before + 2);
    expect(c.getState().view?.session?.overrides).toEqual(["db", "git"]);
  });

  it("shows a failed read as such and recovers on the next one", async () => {
    const r = rig();
    const c = await pointed(r);
    const failing = c.refresh();
    r.profiles.A[1].reject(new AppError({ code: "internal_error", message: "boom", recoverable: true }));
    await failing;
    expect(c.getState().loadError).toBe("boom");
    expect(c.getState().view).not.toBeNull(); // the last good view stays
    const ok = c.refresh();
    r.profiles.A[2].resolve(view("A"));
    await ok;
    expect(c.getState().loadError).toBe("");
  });
});

describe("keepFresh", () => {
  function env(visible = true) {
    let tickFn: (() => void) | null = null;
    let wake: (() => void) | null = null;
    const e = {
      intervalMs: 4000,
      visible: () => visible,
      setInterval: (fn: () => void) => ((tickFn = fn), 1),
      clearInterval: () => {
        tickFn = null;
      },
      onWake: (cb: () => void) => {
        wake = cb;
        return () => {
          wake = null;
        };
      },
    } satisfies FreshnessEnv;
    return { e, tick: () => tickFn?.(), wake: () => wake?.(), active: () => tickFn !== null || wake !== null, hide: () => (visible = false) };
  }

  it("reads on the interval and on wake, only while visible, and stops cleanly", async () => {
    const r = rig();
    const c = await pointed(r);
    const f = env();
    const stop = keepFresh(c, f.e);
    const count = () => r.calls.filter((x) => x.startsWith("profile")).length;
    const base = count();
    f.tick();
    expect(count()).toBe(base + 1);
    r.profiles.A[1].resolve(view("A"));
    await tick();
    f.wake();
    expect(count()).toBe(base + 2);
    r.profiles.A[2].resolve(view("A"));
    await tick();

    f.hide();
    f.tick();
    expect(count()).toBe(base + 2); // hidden: no request

    stop();
    expect(f.active()).toBe(false);
  });

  it("does not pile requests up while one is slow", async () => {
    const r = rig();
    const c = await pointed(r);
    const f = env();
    keepFresh(c, f.e);
    const count = () => r.calls.filter((x) => x.startsWith("profile")).length;
    const base = count();
    for (let i = 0; i < 10; i++) {
      f.tick();
    }
    expect(count()).toBe(base + 1);
  });

  it("a poll that answers after the card moved on changes nothing", async () => {
    const r = rig();
    const c = await pointed(r);
    const f = env();
    keepFresh(c, f.e);
    f.tick();
    c.setTarget(B);
    r.profiles.A[1].resolve(view("A", { paused: true, overrides: ["db"] }));
    await tick();
    expect(c.getState().view).toBeNull();
    r.profiles.B[0].resolve(view("B"));
    await tick();
    expect(c.getState().view?.sessionId).toBe("B");
    expect(c.getState().view?.session?.overrides).toEqual([]);
  });
});
