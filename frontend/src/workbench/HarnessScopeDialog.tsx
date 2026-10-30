import { useState } from "react";
import { Loader2, Wrench } from "lucide-react";
import type { PendingInteraction } from "@/bridge/interaction";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type Props = {
  pending: PendingInteraction;
  busy?: boolean;
  error?: string;
  waiting?: number;
  onCancel: () => void;
  onChoose: (scope: string) => void;
  portal?: boolean;
  /** Wording for the other selection that shares this layout (a session's request for more groups). */
  title?: string;
  fallbackSummary?: string;
  choiceLabel?: string;
  confirmLabel?: string;
};

/**
 * Where an agent's request for tool groups is saved. This is a choice, not an
 * approval: the exact change is prepared for the scope picked here and the person
 * is asked again before anything is saved. Nothing is preselected and Cancel has
 * the focus, so Enter can never pick a scope on the person's behalf. The agent's
 * own words, if any, are inside the host-written summary under an "untrusted text"
 * label.
 *
 * The same layout answers a session's request for more groups (allow, decline, or
 * decline and block); only the strings differ (props). Cancel there declines that
 * one request and blocks nothing.
 */
export function HarnessScopeDialog({
  pending,
  busy,
  error,
  waiting = 0,
  onCancel,
  onChoose,
  portal = true,
  title = "Where should this be saved?",
  fallbackSummary = "An agent asks to turn on tool groups.",
  choiceLabel = "Where to save",
  confirmLabel = "Review the change",
}: Props) {
  const [scope, setScope] = useState("");
  return (
    <Dialog open onOpenChange={() => {}}>
      <DialogContent
        portal={portal}
        showCloseButton={false}
        className="gap-4 rounded-2xl p-5 sm:max-w-lg"
        aria-describedby="harness-scope-description"
      >
        <DialogHeader className="gap-2">
          <div className="flex items-center gap-3">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-border-subtle bg-state-soft text-text-secondary">
              <Wrench aria-hidden className="size-4" />
            </div>
            <DialogTitle className="text-title-16">{title}</DialogTitle>
          </div>
          <DialogDescription id="harness-scope-description" asChild>
            <p className="max-h-56 overflow-y-auto whitespace-pre-wrap break-words text-body-12 leading-relaxed text-text-secondary">
              {pending.summary || fallbackSummary}
            </p>
          </DialogDescription>
          <p className="text-caption-11 text-text-tertiary">
            Requested by <code>{pending.caller || "unknown"}</code>
            {waiting > 0 ? ` · ${waiting} more waiting` : ""}
          </p>
        </DialogHeader>
        <RadioGroup aria-label={choiceLabel} value={scope} onValueChange={setScope} disabled={busy} className="gap-2">
          {pending.options.map((option) => (
            <label
              key={option.id}
              htmlFor={`harness-scope-${option.id}`}
              className="flex cursor-pointer items-center gap-3 rounded-xl border border-border-subtle p-3 hover:bg-state-soft"
            >
              <span className="min-w-0 flex-1 text-body-13 font-medium text-text-primary">{option.label}</span>
              <RadioGroupItem id={`harness-scope-${option.id}`} value={option.id} />
            </label>
          ))}
        </RadioGroup>
        {error ? (
          <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
            {error}
          </p>
        ) : null}
        <DialogFooter className="-mx-5 -mb-5 rounded-b-2xl bg-transparent px-5 py-4">
          <Button type="button" variant="outline" autoFocus disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" disabled={busy || !scope} onClick={() => onChoose(scope)}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
