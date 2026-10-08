import * as React from "react";

import { cn } from "@/lib/utils";
import { CodeBlock } from "@/components/code/CodeBlock";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogDescription,
  DialogFooter,
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
 * So the two halves are the point and the order is the argument: what DevKit
 * understood, then everything it did not, unedited. The panel says so itself —
 * a reader who cannot tell the raw block is complete cannot trust the
 * normalized one above it.
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

  const copyTimer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  React.useEffect(() => {
    setCopied(false);
    return () => { if (copyTimer.current) clearTimeout(copyTimer.current); };
  }, [raw, open]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopied(true);
      if (copyTimer.current) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(false), 1500);
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
          "w-136 max-w-gutter max-h-[85vh] overflow-y-auto overscroll-contain gap-4 sm:max-w-136",
          className,
        )}
      >
        <DialogHeader className="w-full items-start gap-1">
          <DialogTitle className="min-w-0 flex-1 text-body-13-medium text-text-primary">
            Turn detail
          </DialogTitle>
          <DialogDescription className="w-full break-words text-caption-11">{subtitle || "Usage and original vendor metadata for this turn."}</DialogDescription>
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
            className="h-6 rounded-sm border-border-subtle px-2 text-caption-11-medium text-text-secondary"
          >
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>

        <CodeBlock
          data-testid="meta-inspector-raw"
          code={raw || "{}"}
          language="json"
          className={cn(
            "max-h-60 w-full overflow-auto rounded-control p-3",
            "border border-border-subtle bg-surface-well",
            "text-code-11 whitespace-pre-wrap text-text-secondary select-text",
          )}
        />

        <p className="w-full text-caption-11 text-text-tertiary">
          The adapter normalizes what it understands and keeps the rest verbatim.
          Anything a vendor adds that DevKit has no field for is still here.
        </p>
        <DialogFooter><Button type="button" variant="outline" size="sm" onClick={() => onOpenChange(false)}>Done</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
