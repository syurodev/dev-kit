import { Link2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { EntityRef } from "@/workbench/intents";

/**
 * A cross-module ref attached to a turn (docs/dev-kit/cross-module.mdx §
 * Primitive 1). Placeholder for the Figma "Shared / Entity Chip" — that one
 * varies its glyph and preview per `kind`; this one is generic on purpose
 * until a resolver exists for every kind agent.attach can carry.
 */
export type AgentRefChipProps = {
  entityRef: EntityRef;
  onRemove?: () => void;
  className?: string;
};

export function AgentRefChip({ entityRef, onRemove, className }: AgentRefChipProps) {
  return (
    <span
      data-testid="agent-ref-chip"
      className={cn(
        "inline-flex h-6 max-w-full shrink-0 items-center gap-1 rounded-sm bg-(--state-soft) px-2 py-1 select-none",
        className,
      )}
      title={`${entityRef.module}/${entityRef.kind}`}
    >
      <Link2 className="size-(--icon-12) shrink-0 text-text-tertiary" />
      <span className="truncate text-code-12 text-text-secondary">{entityRef.label || entityRef.id}</span>
      {onRemove ? (
        <button
          type="button"
          aria-label={`Remove ${entityRef.label || entityRef.id}`}
          onClick={onRemove}
          className="shrink-0 text-text-tertiary hover:text-text-secondary"
        >
          ×
        </button>
      ) : null}
    </span>
  );
}
