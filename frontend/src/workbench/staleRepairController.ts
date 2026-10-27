import { errorMessage } from "@/bridge/errors";
import type { Outcome, Review, StaleDiagnosis } from "@/bridge/harness";

/**
 * The repair of a stale stored profile (settings for groups or tools this build does
 * not have), free of React so it can be driven with deferred promises. It reads the
 * diagnosis, prepares the review Go derives from the stored profile, and applies it
 * through the shared confirmation. Every asynchronous operation captures the epoch it
 * started in; an answer that arrives after the workspace changed or the panel was
 * closed is never shown, and a review prepared too late is withdrawn. An Apply that is
 * waiting for the person is not cancelled by leaving (that would close the open
 * question): what it did is read back, never inferred. Nothing here shows success
 * before the backend says so.
 */

export type StaleOps = {
  diagnose(workspaceId: string): Promise<StaleDiagnosis>;
  prepare(workspaceId: string): Promise<Review>;
  apply(id: string): Promise<Outcome>;
  cancel(id: string): Promise<void>;
};

export type StaleState = {
  workspaceId: string;
  /** The last successful read. A failed read keeps the previous one and sets readError. */
  diagnosis: StaleDiagnosis | null;
  review: Review | null;
  outcome: Outcome | null;
  busy: "" | "prepare" | "apply";
  /** A refusal of prepare or apply (conflict, stale, denied). */
  error: string;
  /** The diagnosis could not be read. No repair is offered on a read failure. */
  readError: string;
};

const initial = (workspaceId = ""): StaleState => ({
  workspaceId, diagnosis: null, review: null, outcome: null, busy: "", error: "", readError: "",
});
const quiet = () => {};

/** A repair is offered only for a repairable diagnosis that can be shown in full. */
export function canRepair(state: StaleState): boolean {
  const d = state.diagnosis;
  return !!d && d.state === "stale" && !d.truncated && state.readError === "";
}

export class StaleRepairController {
  private state: StaleState = initial();
  private readonly listeners = new Set<() => void>();
  private epoch = 0;
  private running: Promise<void> | null = null;
  private runningEpoch = -1;
  private dirty = false;

  constructor(private readonly ops: StaleOps) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
  getState = (): StaleState => this.state;

  private set(patch: Partial<StaleState>): void {
    this.state = { ...this.state, ...patch };
    for (const listener of [...this.listeners]) {
      listener();
    }
  }

  /** Points the panel at a workspace. The same workspace is a no-op. */
  setWorkspace(workspaceId: string): void {
    if (workspaceId === this.state.workspaceId) {
      return;
    }
    this.epoch++;
    this.dirty = false;
    // A review only being looked at goes with its workspace; one being applied stays.
    const held = this.state.busy === "apply" ? null : this.state.review;
    this.state = initial(workspaceId);
    if (held) {
      void this.ops.cancel(held.id).catch(quiet);
    }
    for (const listener of [...this.listeners]) {
      listener();
    }
    if (workspaceId) {
      void this.refresh();
    }
  }

  /** Stops the panel: nothing it started may write after this. */
  dispose(): void {
    this.setWorkspace("");
    this.epoch++;
  }

  /**
   * Reads the diagnosis (single-flight: a request made while one runs is folded into
   * one more read after it). A failed read keeps the last good diagnosis, records the
   * failure and withdraws the offer.
   */
  refresh(): Promise<void> {
    const { workspaceId } = this.state;
    if (!workspaceId) {
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
          const diagnosis = await this.ops.diagnose(workspaceId);
          if (epoch !== this.epoch) {
            return;
          }
          if (diagnosis.workspaceId !== workspaceId) {
            continue; // an answer about another workspace is not this panel's
          }
          this.set({ diagnosis, readError: "" });
        } catch (e) {
          if (epoch !== this.epoch) {
            return;
          }
          this.set({ readError: errorMessage(e) });
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

  /** Prepares the repair. Go derives the removals; this names only the workspace. Writes nothing. */
  async prepare(): Promise<void> {
    const { workspaceId, busy } = this.state;
    if (!workspaceId || busy !== "" || !canRepair(this.state)) {
      return;
    }
    const epoch = this.epoch;
    this.set({ busy: "prepare", error: "", outcome: null });
    try {
      const review = await this.ops.prepare(workspaceId);
      if (epoch !== this.epoch) {
        void this.ops.cancel(review.id).catch(quiet); // not left waiting for the wrong panel
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
        // A refusal usually means the profile moved: read it again.
        if (this.state.error) {
          await this.refresh();
        }
      }
    }
  }

  /** Back from the review (Cancel). Withdraws it; nothing was saved. */
  async back(): Promise<void> {
    const held = this.state.review;
    if (!held || this.state.busy === "apply") {
      return;
    }
    this.set({ review: null });
    await this.ops.cancel(held.id).catch(quiet);
  }

  /**
   * Asks through the shared confirmation flow and commits. The review is spent
   * whatever the answer. The panel then reads the backend again; a saved, refused or
   * uncertain result is shown as such and never as a success the backend did not report.
   */
  async apply(): Promise<void> {
    const review = this.state.review;
    if (!review || this.state.busy !== "") {
      return;
    }
    const epoch = this.epoch;
    this.set({ busy: "apply", error: "" });
    try {
      const outcome = await this.ops.apply(review.id);
      if (epoch !== this.epoch) {
        return; // read back when the workspace is shown again
      }
      this.set({ outcome });
    } catch (e) {
      // Refused before any write (denied, conflict, stale): the profile is unchanged.
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
}
