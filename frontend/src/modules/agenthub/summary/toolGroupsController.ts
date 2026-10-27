import { errorMessage } from "@/bridge/errors";
import type { Outcome, ProfileView, Review } from "@/bridge/harness";
import { needsReconcile } from "@/workbench/workspaceHarnessModel";

/**
 * The session tool-group card's behavior, free of React so it can be driven with
 * deferred promises. One controller serves one card; the card points it at a
 * session with setTarget. Every asynchronous operation captures the epoch it
 * started in, and an answer that arrives after the target changed is never written
 * into the new target's state:
 *
 *  - a prepared proposal that comes back late is withdrawn (it belongs to a target
 *    the person has left and would otherwise sit in the backend, and could be
 *    applied from the wrong card);
 *  - an Apply, Reconcile or Reload that finishes late writes nothing. That is only
 *    about what this card shows: the backend may well have committed it. The
 *    committed or published state is read back from the backend when the target is
 *    shown again, never inferred from a discarded answer;
 *  - a read that comes back late, or names another session, is dropped.
 *
 * The view is only ever what the backend last said. Nothing is optimistic: an
 * outcome is shown as the backend reported it, and Live comes from the next read.
 */

export type Target = { workspaceId: string; sessionId: string };
export type Busy = "" | "prepare" | "apply" | "reconcile" | "reload";

export type ToolGroupsState = {
  target: Target | null;
  view: ProfileView | null;
  review: Review | null;
  outcome: Outcome | null;
  busy: Busy;
  /** The last action's failure. */
  error: string;
  /** The last read's failure. */
  loadError: string;
};

export type ToolGroupsOps = {
  profile(workspaceId: string, sessionId: string): Promise<ProfileView>;
  prepareRevoke(workspaceId: string, sessionId: string, groups: string[]): Promise<Review>;
  apply(id: string): Promise<Outcome>;
  cancel(id: string): Promise<void>;
  reconcile(workspaceId: string, sessionId: string): Promise<Outcome>;
  reload(sessionId: string): Promise<void>;
};

const initial: ToolGroupsState = { target: null, view: null, review: null, outcome: null, busy: "", error: "", loadError: "" };
const quiet = () => {};

export class ToolGroupsController {
  private state: ToolGroupsState = initial;
  private readonly listeners = new Set<() => void>();
  private epoch = 0;
  private running: Promise<void> | null = null;
  private runningEpoch = -1;
  private dirty = false;

  constructor(private readonly ops: ToolGroupsOps) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getState = (): ToolGroupsState => this.state;

  private set(patch: Partial<ToolGroupsState>): void {
    this.state = { ...this.state, ...patch };
    for (const listener of [...this.listeners]) {
      listener();
    }
  }

  /** Points the card at a session (or nowhere). The same target is a no-op. */
  setTarget(target: Target | null): void {
    const current = this.state.target;
    if (current?.workspaceId === target?.workspaceId && current?.sessionId === target?.sessionId) {
      return;
    }
    this.epoch++;
    this.dirty = false;
    // A review that is only being looked at is withdrawn with the target it belongs
    // to. One that is being applied is not: cancelling would close the open question
    // and change what the person is deciding. It resolves on its own, and is read
    // back when the session is shown again.
    const held = this.state.busy === "apply" ? null : this.state.review;
    this.state = { ...initial, target };
    if (held) {
      void this.ops.cancel(held.id).catch(quiet);
    }
    for (const listener of [...this.listeners]) {
      listener();
    }
    if (target) {
      void this.refresh();
    }
  }

  /**
   * Stops the controller: nothing it started may write after this, and a review it
   * held is withdrawn. Listeners belong to whoever subscribed, so a controller that
   * is pointed at a target again (a remounted view) works.
   */
  dispose(): void {
    this.setTarget(null);
    this.epoch++;
  }

  /**
   * Reads the backend's state. At most one read is in flight; a request made while
   * one runs is folded into a single read after it, so a poll, a focus and an
   * action's follow-up never pile up.
   */
  refresh(): Promise<void> {
    const target = this.state.target;
    if (!target) {
      return Promise.resolve();
    }
    if (this.running && this.runningEpoch === this.epoch) {
      this.dirty = true;
      return this.running;
    }
    const epoch = this.epoch;
    const run: Promise<void> = (async () => {
      do {
        this.dirty = false;
        try {
          const view = await this.ops.profile(target.workspaceId, target.sessionId);
          if (epoch !== this.epoch) {
            return;
          }
          if (view.sessionId !== target.sessionId) {
            continue; // an answer about another session is not this card's
          }
          const stale =
            this.state.outcome &&
            (this.state.outcome.status === "paused" || this.state.outcome.status === "unknown") &&
            view.session &&
            !needsReconcile(view.session);
          // A saved-but-paused notice that the backend no longer supports (someone
          // reconciled it elsewhere) is dropped rather than left telling the person
          // to act on something already done.
          this.set({ view, loadError: "", ...(stale ? { outcome: null } : {}) });
        } catch (e) {
          if (epoch !== this.epoch) {
            return;
          }
          this.set({ loadError: errorMessage(e) });
        }
      } while (this.dirty && epoch === this.epoch);
    })().finally(() => {
      if (this.running === run) {
        this.running = null;
      }
    });
    this.running = run;
    this.runningEpoch = epoch;
    return run;
  }

  private begin(busy: Busy): { target: Target; epoch: number } | null {
    const { target } = this.state;
    if (!target || this.state.busy !== "") {
      return null;
    }
    this.set({ busy, error: "" });
    return { target, epoch: this.epoch };
  }

  /** Prepares removing a group from the session's override. Writes nothing. */
  async revoke(group: string): Promise<void> {
    const started = this.begin("prepare");
    if (!started) {
      return;
    }
    const { target, epoch } = started;
    this.set({ outcome: null });
    try {
      const review = await this.ops.prepareRevoke(target.workspaceId, target.sessionId, [group]);
      if (epoch !== this.epoch) {
        // The card moved on while this was being prepared: it is not shown, and it
        // is not left waiting in the backend for the wrong card to apply.
        void this.ops.cancel(review.id).catch(quiet);
        return;
      }
      this.set({ review });
    } catch (e) {
      if (epoch === this.epoch) {
        this.set({ error: errorMessage(e) });
      }
    } finally {
      if (epoch === this.epoch) {
        this.set({ busy: "" });
      }
    }
  }

  /** Drops the prepared review (Back). */
  async back(): Promise<void> {
    const held = this.state.review;
    if (!held || this.state.busy === "apply") {
      return;
    }
    this.set({ review: null });
    await this.ops.cancel(held.id).catch(quiet);
  }

  /**
   * Asks through the shared confirmation flow and commits. The review stays up
   * while the person is asked, and is spent whatever the answer.
   */
  async apply(): Promise<void> {
    const review = this.state.review;
    if (!review) {
      return;
    }
    const started = this.begin("apply");
    if (!started) {
      return;
    }
    const { epoch } = started;
    try {
      const outcome = await this.ops.apply(review.id);
      if (epoch !== this.epoch) {
        return; // see the class comment: what happened is read back, not inferred
      }
      this.set({ outcome });
    } catch (e) {
      if (epoch === this.epoch) {
        this.set({ error: errorMessage(e) });
      }
    } finally {
      if (epoch === this.epoch) {
        this.set({ review: null, busy: "" });
        await this.refresh();
      }
    }
  }

  /** Brings the session's endpoint to its latest saved override. */
  async reconcile(): Promise<void> {
    await this.act("reconcile", async (target) => {
      const outcome = await this.ops.reconcile(target.workspaceId, target.sessionId);
      return { outcome };
    });
  }

  /** The existing explicit runtime reload. */
  async reload(): Promise<void> {
    await this.act("reload", async (target) => {
      await this.ops.reload(target.sessionId);
      return {};
    });
  }

  private async act(busy: Busy, run: (target: Target) => Promise<Partial<ToolGroupsState>>): Promise<void> {
    const started = this.begin(busy);
    if (!started) {
      return;
    }
    const { target, epoch } = started;
    try {
      const patch = await run(target);
      if (epoch !== this.epoch) {
        return;
      }
      this.set(patch);
    } catch (e) {
      if (epoch === this.epoch) {
        this.set({ error: errorMessage(e) });
      }
    } finally {
      if (epoch === this.epoch) {
        this.set({ busy: "" });
        await this.refresh();
      }
    }
  }
}

/** What keepFresh needs from its environment; the card passes the browser's. */
export type FreshnessEnv = {
  intervalMs: number;
  setInterval(handler: () => void, ms: number): unknown;
  clearInterval(handle: unknown): void;
  visible(): boolean;
  /** Registers a callback for the window regaining focus or becoming visible. */
  onWake(callback: () => void): () => void;
};

/**
 * Keeps a controller's view in step with changes made outside the card (an agent's
 * request, the workspace editor, a publication failure, a reconcile elsewhere): it
 * reads again on an interval while the window is visible and when it wakes. Reads
 * are single-flight through the controller. Returns the function that stops it.
 */
export function keepFresh(controller: ToolGroupsController, env: FreshnessEnv): () => void {
  const read = () => {
    if (env.visible()) {
      void controller.refresh();
    }
  };
  const handle = env.setInterval(read, env.intervalMs);
  const unwake = env.onWake(read);
  return () => {
    env.clearInterval(handle);
    unwake();
  };
}
