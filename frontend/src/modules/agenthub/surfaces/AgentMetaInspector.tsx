import * as React from "react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export type AgentMetaInspectorProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** "refund-worker · turn 3 · end_turn" — assembled from the three below. */
  agentName?: string;
  turn?: number;
  stopReason?: string;
  cost?: number;
  inputTokens?: number;
  outputTokens?: number;
  /** Whatever the vendor sent that DevKit has no field for. */
  meta?: unknown;
  className?: string;
};

/**
 * Agent / Meta Inspector — Figma `951:2270`.
 *
 * Figma's note: "FR-8 promises no vendor data is silently dropped, which is
 * only true for the user if there is somewhere to read it. Opened from a
 * message footer: normalized values above, the raw _meta verbatim below."
 *
 * So the two halves are the point, and the order is the argument: what DevKit
 * understood, then everything it did not, unedited. The closing line says so in
 * the panel itself, because a reader who does not know the raw block is
 * complete cannot trust the normalized one above it.
 *
 * Raw is pretty-printed rather than re-keyed — the moment this panel reorders
 * or renames a vendor's fields it stops being the verbatim copy it promises.
 */
function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex w-full items-start gap-3 text-code-11">
      <span className="min-w-0 flex-1 text-text-tertiary">{label}</span>
      <span className="shrink-0 whitespace-nowrap text-text-secondary">{value}</span>
    </div>
  );
}

export function AgentMetaInspector({
  open,
  onOpenChange,
  agentName,
  turn,
  stopReason,
  cost,
  inputTokens,
  outputTokens,
  meta,
  className,
}: AgentMetaInspectorProps) {
  const raw = React.useMemo(() => {
    if (meta === undefined || meta === null) return "";
    try {
      return JSON.stringify(meta, null, 2);
    } catch {
      // A cyclic or non-serialisable _meta is still worth admitting to.
      return String(meta);
    }
  }, [meta]);

  const [copied, setCopied] = React.useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard is unavailable in some webviews; the block is selectable.
    }
  };

  const subtitle = [
    agentName,
    typeof turn === "number" ? `turn ${turn}` : null,
    stopReason,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="agent-meta-inspector"
        showCloseButton={false}
        className={cn(
          "w-[544px] max-w-[calc(100%-2rem)] gap-4 rounded-[14px] p-5 sm:max-w-[544px]",
          "border border-(--glass-border) bg-(--surface-glass-modal) backdrop-blur-[16px]",
          "shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.5)] ring-0",
          className,
        )}
      >
        <DialogHeader className="w-full flex-row items-center gap-2 space-y-0 p-0">
          <DialogTitle className="min-w-0 flex-1 text-body-13-medium text-text-primary">
            Turn detail
          </DialogTitle>
          {subtitle ? (
            <p className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
              {subtitle}
            </p>
          ) : null}
        </DialogHeader>

        <div className="flex w-full flex-col gap-2">
          <p className="w-full text-caption-11-medium text-text-tertiary">Normalized</p>
          {typeof cost === "number" ? (
            <Row label="cost" value={`$${cost.toFixed(cost < 0.01 ? 4 : 2)}`} />
          ) : null}
          {typeof inputTokens === "number" ? (
            <Row label="input" value={`${inputTokens.toLocaleString()} tokens`} />
          ) : null}
          {typeof outputTokens === "number" ? (
            <Row label="output" value={`${outputTokens.toLocaleString()} tokens`} />
          ) : null}
          {stopReason ? <Row label="stopReason" value={stopReason} /> : null}
        </div>

        <div className="flex w-full items-center gap-2">
          <p className="min-w-0 flex-1 text-caption-11-medium text-text-tertiary">
            Raw _meta
          </p>
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={!raw}
            data-testid="meta-inspector-copy"
            onClick={handleCopy}
            className="h-6 rounded-[6px] border-border-subtle px-2 text-caption-11-medium text-text-secondary"
          >
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>

        <pre
          data-testid="meta-inspector-raw"
          className={cn(
            "max-h-[240px] w-full overflow-auto rounded-control p-3",
            "border border-border-subtle bg-surface-base",
            "text-code-11 whitespace-pre-wrap text-text-secondary select-text",
          )}
        >
          <code>{raw || "{}"}</code>
        </pre>

        <p className="w-full text-caption-11 text-text-tertiary">
          The adapter normalizes what it understands and keeps the rest verbatim.
          Anything a vendor adds that DevKit has no field for is still here.
        </p>
      </DialogContent>
    </Dialog>
  );
}
