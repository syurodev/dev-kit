import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { StaleEntry } from "@/bridge/harness";
import { HarnessReviewBody } from "./HarnessReviewBody";
import { canRepair, type StaleState } from "./staleRepairController";
import { outcomeNotice } from "./workspaceHarnessModel";

/** One stale setting as a sentence: what it was, and so what removing it forgets. */
export function describeStaleEntry(e: StaleEntry): string {
  switch (e.kind) {
    case "group":
      return `Tool group “${e.name}”: saved ${e.on === false ? "off" : "on"}`;
    case "toolBudget":
    case "groupBudget": {
      const what = e.kind === "toolBudget" ? "Tool" : "Tool group";
      const parts: string[] = [];
      if (e.bytes != null) {
        parts.push(`size ${e.bytes} bytes`);
      }
      if (e.cut) {
        parts.push(`cut ${e.cut}`);
      }
      return `${what} “${e.name}”: result limit saved (${parts.join(", ") || "no value"})`;
    }
  }
  return `“${e.name}”`;
}

/**
 * The recovery state of the workspace harness dialog: shown when the stored profile
 * names tool groups or tools this build does not have. It lists every one with the
 * saved value that would be forgotten, explains what a repair does and does not do,
 * and offers Repair (which prepares a review and asks through the shared
 * confirmation) or Cancel. It renders nothing for a healthy profile. A failure to
 * read the diagnosis is a plain error with Reload: no repair is offered for it.
 */
export function StaleRepairView({
  state,
  onPrepare,
  onBack,
  onApply,
  onReload,
  onClose,
}: {
  state: StaleState;
  onPrepare: () => void;
  onBack: () => void;
  onApply: () => void;
  onReload: () => void;
  onClose: () => void;
}) {
  const { diagnosis, review, outcome, busy, error, readError } = state;
  const notice = outcome ? outcomeNotice(outcome) : null;
  // The content region scrolls inside the dialog's height limit and the actions sit
  // below it, so a list near the 64-entry bound never pushes them out of reach. With
  // nothing to show the root takes no room (the ordinary editor owns the dialog).
  const showing = review !== null || diagnosis?.state === "stale";

  return (
    <div className={`flex min-h-0 flex-col gap-3 ${showing ? "flex-1" : ""}`} data-testid="stale-repair">
      {readError ? (
        <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
          Could not check this workspace’s saved settings: {readError}{" "}
          <button type="button" className="underline" onClick={onReload}>
            Reload
          </button>
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
          {error} Nothing was changed by that attempt; the saved settings were read again.
        </p>
      ) : null}
      {notice ? (
        <div role={notice.tone === "danger" || notice.tone === "warning" ? "alert" : "status"} className="rounded-lg border border-border-subtle bg-state-soft px-3 py-2 text-body-12">
          <p className="font-medium text-text-primary">{notice.title}</p>
          <p className="mt-1 text-text-secondary">{notice.body}</p>
        </div>
      ) : null}

      {diagnosis?.state === "unrepairable" ? (
        <p role="status" className="rounded-lg border border-border-subtle px-3 py-2 text-body-12 text-text-secondary">
          This workspace’s saved harness profile has a problem the app cannot repair: {diagnosis.message} New sessions here may not start until it is restored.
        </p>
      ) : null}

      {review ? (
        <>
          <div className="min-h-0 flex-1 overflow-y-auto" data-testid="stale-repair-scroll">
            <HarnessReviewBody review={review} />
          </div>
          <div className="flex shrink-0 justify-end gap-2" data-testid="stale-repair-actions">
            <Button type="button" variant="outline" autoFocus disabled={busy === "apply"} onClick={onBack}>
              Cancel
            </Button>
            <Button type="button" disabled={busy !== ""} onClick={onApply}>
              {busy === "apply" ? <Loader2 className="size-4 animate-spin" /> : null}
              {busy === "apply" ? "Waiting for confirmation…" : "Repair"}
            </Button>
          </div>
        </>
      ) : diagnosis?.state === "stale" ? (
        <section aria-label="Stale saved settings" className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto" data-testid="stale-repair-scroll">
          <p className="text-body-13 font-medium text-text-primary">
            This workspace’s saved settings refer to {diagnosis.total} tool group{diagnosis.total === 1 ? "" : "s"} or tool{diagnosis.total === 1 ? "" : "s"} this build does not have.
          </p>
          <p className="text-body-12 text-text-secondary">
            That stops new sessions from starting here, and other edits are refused until it is fixed. Repair removes exactly the settings below and nothing else; you review and confirm first.
            Sessions already running keep what they opened with. If one of these returns later (a module or build adds it back) its removed setting is not remembered: a group
            uses the default, which is available. To keep them, cancel and restore the missing module or build instead.
          </p>
          <ul aria-label="Settings to remove" className="space-y-1.5">
            {(diagnosis.entries ?? []).map((e, i) => (
              <li key={`${e.kind}-${e.name}-${i}`} className="rounded-md border border-border-subtle px-3 py-2 text-body-12 text-text-primary break-words">
                {describeStaleEntry(e)}
              </li>
            ))}
          </ul>
          {diagnosis.truncated ? (
            <p role="status" className="text-body-12 text-text-secondary">
              {diagnosis.total} settings are stale, too many to review in one step, so no repair is offered here.
            </p>
          ) : null}
          </div>
          <div className="flex shrink-0 justify-end gap-2" data-testid="stale-repair-actions">
            <Button type="button" variant="outline" disabled={busy !== ""} onClick={onClose}>
              Cancel
            </Button>
            <Button type="button" disabled={busy !== "" || !canRepair(state)} onClick={onPrepare}>
              {busy === "prepare" ? <Loader2 className="size-4 animate-spin" /> : null}
              Review repair
            </Button>
          </div>
        </section>
      ) : null}
    </div>
  );
}

/** True when the repair has taken over the groups tab (the ordinary editor is hidden). */
export function repairing(state: StaleState): boolean {
  return canRepair(state) || state.review !== null || state.busy !== "" || (state.diagnosis?.state === "stale" && state.diagnosis.truncated === true);
}
