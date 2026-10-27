import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { errorMessage } from "@/bridge/errors";
import {
  applyReview,
  cancelReview,
  prepareWorkspaceGroups,
  workspaceProfile,
  type Outcome,
  type ProfileView,
  type Review,
} from "@/bridge/harness";
import { GroupSetting } from "../../bindings/devkit/internal/harnessconfig/models";
import { HarnessReviewBody } from "./HarnessReviewBody";
import { LatestRequest } from "./WorkspaceMemoryModel";
import {
  buildEdits,
  formatBytes,
  isDirty,
  outcomeNotice,
  pick,
  rebase,
  rows,
  settings,
  shownSetting,
  type Draft,
} from "./workspaceHarnessModel";

/**
 * "Agent tool groups": which DevKit tool groups sessions of this workspace get. It
 * edits the workspace profile's group entries and nothing else; the review is
 * written by Go and the shared confirmation flow asks before anything is saved. A
 * saved change reaches sessions opened or reloaded afterwards. Closing the dialog
 * is not closing a session and never clears a session override.
 */
export default function WorkspaceHarnessDialog({
  workspace,
  onClose,
  portal = true,
}: {
  workspace: { id: string; name: string };
  onClose: () => void;
  portal?: boolean;
}) {
  const [view, setView] = useState<ProfileView | null>(null);
  const [draft, setDraft] = useState<Draft>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [review, setReview] = useState<Review | null>(null);
  const [busy, setBusy] = useState<"" | "prepare" | "apply">("");
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const latest = useRef(new LatestRequest());
  const reviewRef = useRef<Review | null>(null);
  reviewRef.current = review;

  const load = useCallback(async () => {
    const current = latest.current.begin();
    setLoading(true);
    try {
      const next = await workspaceProfile(workspace.id);
      // A late answer for a request that was superseded, or for another workspace,
      // is ignored.
      if (!current() || next.workspaceId !== workspace.id) {
        return;
      }
      setView(next);
      setDraft((previous) => rebase(previous, next));
      setError("");
    } catch (e) {
      if (current()) {
        setError(errorMessage(e));
      }
    } finally {
      if (current()) {
        setLoading(false);
      }
    }
  }, [workspace.id]);

  useEffect(() => {
    void load();
    const generation = latest.current;
    return () => {
      generation.invalidate();
    };
  }, [load]);

  // A prepared review is only a lookup key held by Go; withdraw it if the dialog goes.
  useEffect(
    () => () => {
      const open = reviewRef.current;
      if (open) {
        void cancelReview(open.id).catch(() => {});
      }
    },
    [],
  );

  async function prepare() {
    setBusy("prepare");
    setError("");
    setOutcome(null);
    try {
      setReview(await prepareWorkspaceGroups(workspace.id, buildEdits(view, draft)));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy("");
    }
  }

  async function discardReview() {
    const open = review;
    setReview(null);
    if (open) {
      try {
        await cancelReview(open.id);
      } catch {
        /* Already spent or gone: nothing is pending either way. */
      }
    }
  }

  async function apply() {
    if (!review) {
      return;
    }
    setBusy("apply");
    setError("");
    const spent = review;
    try {
      // The review stays on screen while DevKit waits for the person; the busy
      // state disables every control, so it cannot be submitted a second time.
      const result = await applyReview(spent.id);
      setOutcome(result);
      setDraft({});
    } catch (e) {
      // A refusal before the write: nothing was saved. The saved profile is read
      // again and the unsaved picks stay for another review.
      setError(errorMessage(e));
    } finally {
      // The proposal is spent whatever the answer: it is never submitted again.
      setReview(null);
      reviewRef.current = null;
      await load();
      setBusy("");
    }
  }

  const notice = outcome ? outcomeNotice(outcome) : null;
  const list = rows(view);
  const dirty = isDirty(draft);

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent
        portal={portal}
        className="flex max-h-[85vh] flex-col gap-4 sm:max-w-2xl"
        aria-describedby="harness-dialog-description"
      >
        <DialogHeader>
          <DialogTitle>Agent tool groups · {workspace.name}</DialogTitle>
          <DialogDescription id="harness-dialog-description">
            Choose which DevKit tool groups agent sessions in this workspace get. A change is saved to the
            workspace and reaches sessions that are opened or reloaded afterwards; sessions already
            running keep the groups they opened with. Sizes are bytes of tool definitions, not tokens
            or cost.
          </DialogDescription>
        </DialogHeader>

        {error ? (
          <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
            {error}{" "}
            <button type="button" className="underline" onClick={() => void load()}>
              Reload
            </button>
          </p>
        ) : null}

        {notice ? (
          <div
            role={notice.tone === "danger" || notice.tone === "warning" ? "alert" : "status"}
            className="rounded-lg border border-border-subtle bg-state-soft px-3 py-2 text-body-12"
          >
            <p className="font-medium text-text-primary">{notice.title}</p>
            <p className="mt-1 text-text-secondary">{notice.body}</p>
          </div>
        ) : null}

        {review ? (
          <div className="min-h-0 flex-1 overflow-y-auto">
            <HarnessReviewBody review={review} />
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto">
            {loading && !view ? (
              <div role="status" className="flex items-center justify-center gap-2 p-8 text-body-12 text-text-tertiary">
                <Loader2 className="size-4 animate-spin" />
                Loading tool groups…
              </div>
            ) : (
              <ul aria-label="Tool groups" className="space-y-1.5">
                {list.map((row) => {
                  const shown = shownSetting(row, draft);
                  const changed = draft[row.name] !== undefined;
                  return (
                    <li
                      key={row.name}
                      className="flex items-center gap-3 rounded-lg border border-border-subtle px-3 py-2"
                      data-changed={changed || undefined}
                    >
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-body-13 font-medium text-text-primary" title={row.name}>
                          {row.name}
                          {changed ? <span className="ml-2 text-caption-11 text-text-tertiary">unsaved</span> : null}
                        </p>
                        <p className="truncate text-caption-11 text-text-tertiary">
                          {row.tools} {row.tools === 1 ? "tool" : "tools"} · {formatBytes(row.definitionBytes)} of definitions
                          {" · "}
                          {row.workspace === GroupSetting.SettingInherit ? "default (on)" : `saved ${row.workspace}`}
                        </p>
                      </div>
                      <NativeSelect
                        size="sm"
                        aria-label={`${row.name} setting`}
                        value={shown}
                        disabled={busy !== "" || loading}
                        onChange={(event) => setDraft((d) => pick(d, row, event.target.value as GroupSetting))}
                      >
                        {settings.map((option) => (
                          <NativeSelectOption key={option.value} value={option.value}>
                            {option.label}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </li>
                  );
                })}
                {list.length === 0 && !loading ? (
                  <li className="p-6 text-center text-body-12 text-text-tertiary">No tool groups are registered.</li>
                ) : null}
              </ul>
            )}
          </div>
        )}

        <DialogFooter className="items-center">
          {!review && dirty ? (
            <span className="mr-auto text-caption-11 text-text-tertiary">
              {Object.keys(draft).length} unsaved {Object.keys(draft).length === 1 ? "change" : "changes"}
            </span>
          ) : null}
          {review ? (
            <>
              <Button type="button" variant="outline" autoFocus disabled={busy === "apply"} onClick={() => void discardReview()}>
                Back
              </Button>
              <Button type="button" disabled={busy !== ""} onClick={() => void apply()}>
                {busy === "apply" ? <Loader2 className="size-4 animate-spin" /> : null}
                {busy === "apply" ? "Waiting for confirmation…" : "Apply"}
              </Button>
            </>
          ) : (
            <>
              {dirty ? (
                <Button type="button" variant="ghost" disabled={busy !== ""} onClick={() => setDraft({})}>
                  Discard changes
                </Button>
              ) : null}
              <Button type="button" variant="outline" disabled={busy !== ""} onClick={onClose}>
                Close
              </Button>
              <Button type="button" disabled={!dirty || busy !== "" || loading} onClick={() => void prepare()}>
                {busy === "prepare" ? <Loader2 className="size-4 animate-spin" /> : null}
                Review changes
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
