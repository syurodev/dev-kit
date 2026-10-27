import { errorMessage } from "@/bridge/errors";
import type { BudgetPage, Outcome, Review } from "@/bridge/harness";
import {
  buildBudgetEdits,
  isBudgetDirty,
  rebaseBudgets,
  savedRule,
  setIntent,
  type BudgetDraft,
  type CutValue,
  type Intent,
  type Kind,
} from "./workspaceBudgetModel";

/**
 * The workspace result-limit editor's behavior, free of React so it can be driven
 * with deferred promises. It holds the pages read so far, the person's draft (only
 * the fields they changed, never a copy of the profile), the prepared review and
 * the last outcome. Every asynchronous operation captures the epoch it started in;
 * an answer that arrives after the workspace changed or the editor was closed is
 * never shown, and a proposal prepared too late is withdrawn. An Apply that is
 * already waiting for the person is not cancelled by leaving: that would close the
 * open question, and what it did is read back from the backend, not inferred.
 *
 * Nothing is resolved here. Effective limits and their sources arrive with each
 * row; the exact before and after of an edit comes in the review, written by Go.
 */

export type BudgetsOps = {
  page(workspaceId: string, group: string, cursor: string): Promise<BudgetPage>;
  prepare(workspaceId: string, edits: ReturnType<typeof buildBudgetEdits>): Promise<Review>;
  apply(id: string): Promise<Outcome>;
  cancel(id: string): Promise<void>;
};

export type BudgetsState = {
  workspaceId: string;
  group: string;
  /** First page's metadata and groups, with every tool loaded so far. */
  page: BudgetPage | null;
  nextCursor: string;
  draft: BudgetDraft;
  review: Review | null;
  outcome: Outcome | null;
  busy: "" | "load" | "prepare" | "apply";
  error: string;
  loadError: string;
};

const initial = (workspaceId = "", group = ""): BudgetsState => ({
  workspaceId, group, page: null, nextCursor: "", draft: {}, review: null, outcome: null, busy: "", error: "", loadError: "",
});
const quiet = () => {};

export class BudgetsController {
  private state: BudgetsState = initial();
  private readonly listeners = new Set<() => void>();
  private epoch = 0;
  private running: Promise<void> | null = null;
  private runningEpoch = -1;
  private dirty = false;

  constructor(private readonly ops: BudgetsOps) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
  getState = (): BudgetsState => this.state;

  private set(patch: Partial<BudgetsState>): void {
    this.state = { ...this.state, ...patch };
    for (const listener of [...this.listeners]) {
      listener();
    }
  }

  /** Points the editor at a workspace. The same workspace is a no-op. */
  setWorkspace(workspaceId: string): void {
    if (workspaceId === this.state.workspaceId) {
      return;
    }
    this.epoch++;
    this.dirty = false;
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

  /** Stops the editor: nothing it started may write after this. */
  dispose(): void {
    this.setWorkspace("");
    this.epoch++;
  }

  /** Filters the tool list to one group (or all). The draft is kept. */
  setGroup(group: string): void {
    if (group === this.state.group) {
      return;
    }
    this.epoch++;
    this.dirty = false;
    this.set({ group, page: null, nextCursor: "", loadError: "" });
    void this.refresh();
  }

  /**
   * Reads the first page again (single-flight: a request made while one runs is
   * folded into one more read after it). The draft is rebased onto what was read;
   * it is never discarded by a read, nor by a failed one.
   */
  refresh(): Promise<void> {
    const { workspaceId, group } = this.state;
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
          const page = await this.ops.page(workspaceId, group, "");
          if (epoch !== this.epoch) {
            return;
          }
          if (page.workspaceId !== workspaceId) {
            continue; // an answer about another workspace is not this editor's
          }
          this.set({ page, nextCursor: page.nextCursor ?? "", draft: rebaseBudgets(this.state.draft, page), loadError: "" });
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

  /** Appends the next page of tools. Only on the person's request: reads stay bounded. */
  async loadMore(): Promise<void> {
    const { workspaceId, group, nextCursor, page, busy } = this.state;
    if (!workspaceId || !nextCursor || !page || busy !== "") {
      return;
    }
    const epoch = this.epoch;
    this.set({ busy: "load" });
    try {
      const more = await this.ops.page(workspaceId, group, nextCursor);
      if (epoch !== this.epoch || more.workspaceId !== workspaceId) {
        return;
      }
      const merged: BudgetPage = { ...page, tools: [...(page.tools ?? []), ...(more.tools ?? [])], nextCursor: more.nextCursor };
      this.set({ page: merged, nextCursor: more.nextCursor ?? "", draft: rebaseBudgets(this.state.draft, merged), loadError: "" });
    } catch (e) {
      if (epoch === this.epoch) {
        this.set({ loadError: errorMessage(e) });
      }
    } finally {
      if (epoch === this.epoch) {
        this.set({ busy: "" });
      }
    }
  }

  /** Records one field's intent for a tool or group. Undefined clears it. */
  edit(kind: Kind, name: string, field: "bytes" | "cut", intent: Intent<number> | Intent<CutValue> | undefined): void {
    if (this.state.busy === "apply") {
      return;
    }
    const saved = savedRule(this.state.page, kind, name);
    this.set({ draft: setIntent(this.state.draft, kind, name, field, intent, saved), outcome: null });
  }

  discard(): void {
    if (this.state.busy !== "apply") {
      this.set({ draft: {} });
    }
  }

  /** Prepares the draft for review. Writes nothing. */
  async prepare(): Promise<void> {
    const { workspaceId, draft, busy } = this.state;
    if (!workspaceId || busy !== "" || !isBudgetDirty(draft)) {
      return;
    }
    const epoch = this.epoch;
    this.set({ busy: "prepare", error: "", outcome: null });
    try {
      const review = await this.ops.prepare(workspaceId, buildBudgetEdits(draft));
      if (epoch !== this.epoch) {
        void this.ops.cancel(review.id).catch(quiet); // not left waiting for the wrong editor
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

  /** Back from the review to editing. The draft is kept. */
  async back(): Promise<void> {
    const held = this.state.review;
    if (!held || this.state.busy === "apply") {
      return;
    }
    this.set({ review: null });
    await this.ops.cancel(held.id).catch(quiet);
  }

  /** Asks through the shared confirmation flow and commits. The review is spent either way. */
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
        return; // read back when the workspace is shown again, never inferred
      }
      this.set({ outcome, draft: {} });
    } catch (e) {
      // Refused before any write: the draft stays for another review.
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
