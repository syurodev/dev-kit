import { useEffect, useState } from "react";
import { FileText, KeyRound, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  syncConflictDetail,
  syncResolveConflict,
  type ResolveStrategy,
  type SyncConflictDetail,
} from "../../bridge/sync";
import { errorMessage } from "../../bridge/errors";
import { clockOf } from "./SyncSidebar";

type Props = {
  recordId: string;
  detectedAt: string;
  onClose: () => void;
  onResolved: () => void;
};

const PANE =
  "flex min-h-0 flex-1 flex-col rounded-control border bg-surface-well pb-3";
const PANE_HEAD = "px-4 pt-3 pb-2.5 text-label-11-caps text-text-tertiary";
const PANE_BODY = "min-h-0 flex-1 overflow-auto px-4 text-code-12 whitespace-pre-wrap text-text-secondary";

/**
 * Figma `Sync / Resolve — Merge` (781:216) and `Choose a side` (782:451): the
 * record in the header with "both sides changed" and "detected" chips, Keep
 * mine / Keep theirs and — for a mergeable text record — Save merge ⌘S; then
 * LOCAL · THIS DEVICE, MERGED · EDITABLE (focus-ringed, a textarea) and
 * REMOTE · CLOUD side by side. A secret or a deletion has no merged pane:
 * there is nothing to line-merge, only a side to pick.
 */
export function ResolvePanel({ recordId, detectedAt, onClose, onResolved }: Props) {
  const [detail, setDetail] = useState<SyncConflictDetail | null>(null);
  const [merged, setMerged] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setDetail(null);
    setError("");
    syncConflictDetail(recordId)
      .then((loaded) => {
        if (!active) return;
        setDetail(loaded);
        setMerged(loaded.oursText);
      })
      .catch((caught) => active && setError(errorMessage(caught)));
    return () => {
      active = false;
    };
  }, [recordId]);

  async function resolve(strategy: ResolveStrategy) {
    setBusy(true);
    setError("");
    try {
      await syncResolveConflict(recordId, strategy, strategy === "merge" ? merged : "");
      onResolved();
    } catch (caught) {
      setError(errorMessage(caught));
      setBusy(false);
    }
  }

  const mergeable = !!detail && detail.mergeable && !detail.oursDeleted && !detail.theirsDeleted;
  const waiting = !!detail && !detail.theirsCaptured;
  const disabled = busy || !detail || waiting;
  const Glyph = detail?.oursDeleted || detail?.theirsDeleted ? Trash2 : detail?.recordType === "secret" ? KeyRound : FileText;

  const situation = !detail
    ? ""
    : waiting
      ? "The other version has not been pulled yet. Run Sync now to fetch “theirs”, then resolve."
      : mergeable
        ? "Both sides changed this record. Edit the merged pane, or take one side whole."
        : detail.oursDeleted && detail.theirsDeleted
          ? "Both sides deleted this record. Pick which deletion wins."
          : detail.oursDeleted
            ? "This device deleted the record; the other side kept editing it. Pick a side — a deletion is not line-merged."
            : detail.theirsDeleted
              ? "The other side deleted this record; this device kept editing it. Pick a side — a deletion is not line-merged."
              : "This is a secret. Pick which encrypted version to keep — secrets are never line-merged.";

  return (
    <div
      className="flex h-full flex-col gap-3 px-6 pt-5 pb-6"
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "s" && mergeable && !disabled) {
          e.preventDefault();
          void resolve("merge");
        }
      }}
    >
      <div className="flex h-9 shrink-0 items-center gap-3">
        <Glyph className="size-4.5 shrink-0 text-text-secondary" />
        <span className="truncate text-title-16 text-text-primary">
          {detail ? `${detail.recordType}/${detail.recordId}` : recordId}
        </span>
        <span className="flex h-6 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-raised px-2.5 text-code-11 text-text-secondary">
          <span className={cn("size-1.5 rounded-full", waiting ? "bg-text-tertiary" : "bg-status-warning")} />
          {waiting ? "waiting for theirs" : detail?.theirsDeleted || detail?.oursDeleted ? "deleted on one side" : "both sides changed"}
        </span>
        <span className="flex h-6 items-center rounded-md border border-border-subtle bg-surface-raised px-2.5 text-code-11 text-text-secondary">
          detected {clockOf(detectedAt)}
        </span>
        <span className="flex-1" />
        <Button variant="outline" className="bg-surface-raised" disabled={disabled} onClick={() => resolve("ours")}>
          Keep mine
        </Button>
        <Button variant="outline" className="bg-surface-raised" disabled={disabled} onClick={() => resolve("theirs")}>
          Keep theirs
        </Button>
        {mergeable && (
          <Button disabled={disabled} onClick={() => resolve("merge")}>
            Save merge
            <span className="text-code-11 opacity-65">⌘S</span>
          </Button>
        )}
        <span className="h-5 w-px bg-border-subtle" />
        <Button variant="ghost" size="icon-sm" aria-label="Close" onClick={onClose} disabled={busy}>
          <X className="size-3.5" />
        </Button>
      </div>

      <p className="shrink-0 text-body-13 text-text-secondary">
        {error ? <span className="text-status-danger" role="alert">{error}</span> : situation || "Loading…"}
      </p>

      {detail && (
        <div className="flex min-h-0 flex-1 gap-3">
          <section className={cn(PANE, "border-border-subtle")}>
            <span className={PANE_HEAD}>Local · this device</span>
            <pre className={PANE_BODY}>{detail.oursDeleted ? "(deleted on this device)" : detail.oursText}</pre>
          </section>
          {mergeable && (
            <section className={cn(PANE, "border-status-info")}>
              <span className={PANE_HEAD}>Merged · editable</span>
              <textarea
                value={merged}
                onChange={(event) => setMerged(event.target.value)}
                disabled={busy}
                spellCheck={false}
                aria-label="Merged result"
                className="min-h-0 flex-1 resize-none bg-transparent px-4 text-code-12 text-text-primary outline-none"
              />
            </section>
          )}
          <section className={cn(PANE, "border-border-subtle")}>
            <span className={PANE_HEAD}>Remote · cloud</span>
            <pre className={PANE_BODY}>
              {!detail.theirsCaptured
                ? "(not pulled yet)"
                : detail.theirsDeleted
                  ? "(deleted remotely)"
                  : detail.theirsText}
            </pre>
          </section>
        </div>
      )}
    </div>
  );
}
