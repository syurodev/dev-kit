import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import {
  applyReview,
  cancelReview,
  onHarnessChanged,
  prepareBudgetEdits,
  workspaceBudgets,
  type BudgetPage,
  type GroupBudgetRow,
  type ToolBudgetRow,
} from "@/bridge/harness";
import { HarnessReviewBody } from "./HarnessReviewBody";
import { BudgetsController, type BudgetsOps } from "./budgetsController";
import {
  cutLabels,
  describeEntry,
  formatKiB,
  isBudgetDirty,
  parseKiB,
  pendingCount,
  samplePreview,
  sourceLabel,
  type BudgetDraftEntry,
  type CutValue,
  type Kind,
} from "./workspaceBudgetModel";
import { outcomeNotice } from "./workspaceHarnessModel";

const ops: BudgetsOps = {
  page: workspaceBudgets,
  prepare: prepareBudgetEdits,
  apply: applyReview,
  cancel: cancelReview,
};

const INHERIT = "inherit";

/**
 * Result limits of one workspace: per-group and per-tool size and cut mode, each
 * independently settable or resettable to what it inherits. It edits the saved
 * workspace profile only; a running session keeps the limits it opened with until
 * it is reloaded. All behavior is in BudgetsController (stale answers, drafts,
 * single-flight reads); the review of the exact limits before and after is written
 * by Go and the shared confirmation flow asks before anything is saved.
 */
export function WorkspaceBudgetsPanel({
  workspaceId,
  active,
  onPendingChange,
}: {
  workspaceId: string;
  active: boolean;
  /** True while this tab prepares or applies a change (not while it only reads). The host must not close then. */
  onPendingChange?: (pending: boolean) => void;
}) {
  const [controller] = useState(() => new BudgetsController(ops));
  const state = useSyncExternalStore(controller.subscribe, controller.getState, controller.getState);
  const groupNames = useRef<string[]>([]);

  useEffect(() => {
    controller.setWorkspace(workspaceId);
  }, [controller, workspaceId]);
  useEffect(() => () => controller.dispose(), [controller]);
  // This window's own saves, and a wake of the window, read the backend again.
  useEffect(() => {
    const read = () => void controller.refresh();
    const stop = onHarnessChanged(read);
    window.addEventListener("focus", read);
    return () => {
      stop();
      window.removeEventListener("focus", read);
    };
  }, [controller]);

  const { page, draft, review, outcome, busy, error, loadError, nextCursor, group } = state;
  const pending = busy === "prepare" || busy === "apply";
  useEffect(() => {
    onPendingChange?.(pending);
    return () => onPendingChange?.(false);
  }, [pending, onPendingChange]);
  if (page && group === "") {
    groupNames.current = (page.groups ?? []).map((g) => g.group);
  }
  const dirty = isBudgetDirty(draft);
  const notice = outcome ? outcomeNotice(outcome) : null;
  const shownError = error || loadError;

  return (
    <div hidden={!active} className="flex min-h-0 flex-1 flex-col gap-3">
      {shownError ? (
        <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
          {shownError}{" "}
          <button type="button" className="underline" onClick={() => void controller.refresh()}>
            Reload
          </button>
        </p>
      ) : null}
      {notice ? (
        <div role={notice.tone === "warning" || notice.tone === "danger" ? "alert" : "status"} className="rounded-lg border border-border-subtle bg-state-soft px-3 py-2 text-body-12">
          <p className="font-medium text-text-primary">{notice.title}</p>
          <p className="mt-1 text-text-secondary">{notice.body}</p>
        </div>
      ) : null}

      {review ? (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <HarnessReviewBody review={review} />
        </div>
      ) : (
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto">
          <p className="text-caption-11 text-text-tertiary">
            A limit is the most host text one tool result may put in an agent's context, in bytes (not tokens or cost). A change is saved to the workspace and applies when a
            session is opened or reloaded; running sessions keep what they have. Size and cut mode are separate: leave a field on “Inherit” to use the group's, then the default
            ({page ? formatKiB(page.defaultBytes) : "24 KiB"}, start only).
          </p>
          <div className="flex items-center gap-2">
            <label htmlFor="budget-group-filter" className="text-body-12 text-text-secondary">Show</label>
            <NativeSelect size="sm" id="budget-group-filter" value={group} disabled={busy === "apply"} onChange={(e) => controller.setGroup(e.target.value)}>
              <NativeSelectOption value="">All groups</NativeSelectOption>
              {groupNames.current.map((name) => (
                <NativeSelectOption key={name} value={name}>{name}</NativeSelectOption>
              ))}
            </NativeSelect>
            {busy === "load" || (!page && !loadError) ? <Loader2 role="status" aria-label="Loading" className="size-4 animate-spin text-text-tertiary" /> : null}
          </div>

          {(page?.groups ?? []).length > 0 ? (
            <section aria-label="Group limits" className="space-y-1.5">
              <h4 className="text-body-13 font-medium text-text-primary">Groups</h4>
              {(page?.groups ?? []).map((g) => (
                <GroupRow key={g.group} row={g} page={page!} draft={draft[`group:${g.group}`]} controller={controller} disabled={busy === "apply"} />
              ))}
            </section>
          ) : null}

          {page ? (
            <section aria-label="Tool limits" className="space-y-1.5">
              <h4 className="text-body-13 font-medium text-text-primary">Tools</h4>
              {(page.tools ?? []).map((t) => (
                <ToolRow key={t.tool} row={t} draft={draft[`tool:${t.tool}`]} controller={controller} disabled={busy === "apply"} />
              ))}
              {nextCursor ? (
                <Button type="button" size="sm" variant="outline" disabled={busy !== ""} onClick={() => void controller.loadMore()}>
                  Show more tools
                </Button>
              ) : null}
            </section>
          ) : null}

          <section aria-label="Illustration" className="rounded-lg border border-dashed border-border-subtle p-3">
            <h4 className="text-body-12 font-medium text-text-secondary">What the two cut modes keep (made-up sample text)</h4>
            <div className="mt-2 grid gap-3 sm:grid-cols-2">
              {(["head", "headTail"] as CutValue[]).map((mode) => (
                <div key={mode}>
                  <p className="text-caption-11 text-text-tertiary">{cutLabels[mode]}</p>
                  <pre className="mt-1 overflow-x-auto rounded bg-state-soft p-2 text-code-11 leading-snug">{samplePreview(mode, 24 * 1024).join("\n")}</pre>
                </div>
              ))}
            </div>
          </section>
        </div>
      )}

      <div className="flex items-center justify-end gap-2 border-t border-border-subtle pt-3">
        {!review && dirty ? (
          <span className="mr-auto text-caption-11 text-text-tertiary">
            {pendingCount(draft)} unsaved {pendingCount(draft) === 1 ? "change" : "changes"}
          </span>
        ) : null}
        {review ? (
          <>
            <Button type="button" variant="outline" autoFocus disabled={busy === "apply"} onClick={() => void controller.back()}>
              Back
            </Button>
            <Button type="button" disabled={busy !== ""} onClick={() => void controller.apply()}>
              {busy === "apply" ? <Loader2 className="size-4 animate-spin" /> : null}
              {busy === "apply" ? "Waiting for confirmation…" : "Apply"}
            </Button>
          </>
        ) : (
          <>
            {dirty ? (
              <Button type="button" variant="ghost" disabled={busy !== ""} onClick={() => controller.discard()}>
                Discard changes
              </Button>
            ) : null}
            <Button type="button" disabled={!dirty || busy !== ""} onClick={() => void controller.prepare()}>
              {busy === "prepare" ? <Loader2 className="size-4 animate-spin" /> : null}
              Review limit changes
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

type RowProps<R> = { row: R; draft?: BudgetDraftEntry; controller: BudgetsController; disabled: boolean };

function GroupRow({ row, page, draft, controller, disabled }: Omit<RowProps<GroupBudgetRow>, never> & { page: BudgetPage }) {
  const exempt = row.cuttable === 0;
  return (
    <div className="rounded-lg border border-border-subtle px-3 py-2" data-changed={draft ? true : undefined}>
      <div className="flex items-baseline gap-2">
        <span className="text-body-13 font-medium text-text-primary">{row.group}</span>
        <span className="text-caption-11 text-text-tertiary">
          {row.tools} {row.tools === 1 ? "tool" : "tools"}, {row.cuttable} can be cut
        </span>
        {draft ? <span className="text-caption-11 text-text-tertiary">unsaved: {describeEntry(draft)}</span> : null}
      </div>
      {exempt ? (
        <p className="mt-1 text-caption-11 text-text-tertiary">Results from this group are never cut, so it has no limit to set.</p>
      ) : (
        <Fields kind="group" name={row.group} savedBytes={row.bytes ?? undefined} savedCut={row.cut || undefined} draft={draft} controller={controller} disabled={disabled} page={page} />
      )}
    </div>
  );
}

function ToolRow({ row, draft, controller, disabled }: RowProps<ToolBudgetRow>) {
  const [open, setOpen] = useState(false);
  return (
    <div className="rounded-lg border border-border-subtle px-3 py-2" data-changed={draft ? true : undefined}>
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-body-12 text-text-primary" title={row.tool}>{row.tool}</span>
        {row.exempt ? (
          <span className="text-caption-11 text-text-tertiary">never cut</span>
        ) : (
          <>
            <span className="text-caption-11 text-text-tertiary">
              {formatKiB(row.effectiveBytes ?? 0)} ({sourceLabel(row.bytesFrom)}) · {cutLabels[(row.effectiveCut || "head") as CutValue] ?? row.effectiveCut} ({sourceLabel(row.cutFrom)})
            </span>
            <Button type="button" size="sm" variant="ghost" aria-expanded={open} aria-label={`Edit the limit of ${row.tool}`} onClick={() => setOpen((o) => !o)}>
              {open || draft ? "Hide" : "Edit"}
            </Button>
          </>
        )}
      </div>
      {draft ? <p className="mt-1 text-caption-11 text-text-tertiary">unsaved: {describeEntry(draft)}</p> : null}
      {!row.exempt && (open || draft) ? (
        <Fields kind="tool" name={row.tool} savedBytes={row.toolBytes ?? undefined} savedCut={row.toolCut || undefined} draft={draft} controller={controller} disabled={disabled} />
      ) : null}
    </div>
  );
}

/** Size and cut mode of one rule, each with its own inherit/reset. */
function Fields({ kind, name, savedBytes, savedCut, draft, controller, disabled }: {
  kind: Kind; name: string; savedBytes?: number; savedCut?: string; draft?: BudgetDraftEntry; controller: BudgetsController; disabled: boolean; page?: BudgetPage;
}) {
  const shownBytes = draft?.bytes ? (draft.bytes.op === "set" ? String(draft.bytes.value / 1024) : "") : savedBytes !== undefined ? String(savedBytes / 1024) : "";
  const [text, setText] = useState(shownBytes);
  const [problem, setProblem] = useState("");
  useEffect(() => setText(shownBytes), [shownBytes]);
  const inheritsBytes = draft?.bytes ? draft.bytes.op === "reset" : savedBytes === undefined;
  const mode = draft?.cut ? (draft.cut.op === "set" ? draft.cut.value : INHERIT) : savedCut ?? INHERIT;

  function commit() {
    if (text.trim() === "") {
      setProblem("");
      return; // leaving it blank changes nothing; use Inherit to remove the rule's size
    }
    const parsed = parseKiB(text);
    if (!parsed.ok) {
      setProblem(parsed.error);
      return;
    }
    setProblem("");
    controller.edit(kind, name, "bytes", { op: "set", value: parsed.bytes });
  }

  return (
    <div className="mt-2 flex flex-wrap items-start gap-4">
      <div className="space-y-1">
        <label htmlFor={`size-${kind}-${name}`} className="text-caption-11 text-text-secondary">Size (KiB)</label>
        <div className="flex items-center gap-1.5">
          <Input
            id={`size-${kind}-${name}`}
            inputMode="decimal"
            className="h-8 w-24"
            value={text}
            placeholder={inheritsBytes ? "inherit" : ""}
            disabled={disabled}
            aria-invalid={problem ? true : undefined}
            onChange={(e) => setText(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => e.key === "Enter" && commit()}
          />
          <Button type="button" size="sm" variant="outline" disabled={disabled || (inheritsBytes && !draft?.bytes)} onClick={() => { setProblem(""); controller.edit(kind, name, "bytes", { op: "reset" }); }}>
            Inherit
          </Button>
        </div>
        {problem ? <p role="alert" className="text-caption-11 text-status-danger">{problem}</p> : null}
      </div>
      <div className="space-y-1">
        <label htmlFor={`cut-${kind}-${name}`} className="text-caption-11 text-text-secondary">Cut</label>
        <NativeSelect
          size="sm"
          id={`cut-${kind}-${name}`}
          value={mode}
          disabled={disabled}
          onChange={(e) => {
            const v = e.target.value;
            controller.edit(kind, name, "cut", v === INHERIT ? { op: "reset" } : { op: "set", value: v as CutValue });
          }}
        >
          <NativeSelectOption value={INHERIT}>Inherit</NativeSelectOption>
          <NativeSelectOption value="head">{cutLabels.head}</NativeSelectOption>
          <NativeSelectOption value="headTail">{cutLabels.headTail}</NativeSelectOption>
        </NativeSelect>
      </div>
    </div>
  );
}
