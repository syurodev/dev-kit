import { Handle, Position, type NodeProps } from "@xyflow/react";
import { LAYERS, layerOf } from "./tokens";
import { cn } from "@/lib/utils";

export type ApiNodeData = {
  layer: string;
  title: string;
  detail: string;
  /** Full text for the native tooltip, since the card truncates both lines. */
  titleTooltip?: string;
  detailTooltip?: string;
};

/**
 * State uses a neutral border ramp (subtle -> tertiary -> white 2px) rather
 * than the layer colour. `--border-focus` in the design system resolves to the
 * same hex as `--status-info`, so a focus-coloured ring is invisible on a
 * Controller node; and any layer-coloured ring would make "selected" and "which
 * layer" say the same thing. Layer meaning lives in the icon and label only.
 */
export function ApiNode({ data, selected }: NodeProps & { data: ApiNodeData }) {
  const visual = LAYERS[layerOf(data.layer)];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;

  return (
    <div
      className={cn(
        "group w-67 rounded-xl bg-surface-raised p-3",
        "flex flex-col gap-2.5",
        selected
          ? "border-2 border-text-primary shadow-[0_6px_16px_rgba(0,0,0,0.45)]"
          : "border border-border-subtle shadow-[0_1px_3px_rgba(0,0,0,0.18)] hover:border-text-tertiary hover:shadow-[0_4px_12px_rgba(0,0,0,0.35)]",
      )}
    >
      {/* Handles are the anchors the connector geometry attaches to: a straight
          run enters the top, a branch enters the left. Both are invisible —
          this is a read-only view, nobody draws edges by hand. */}
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span
          className="flex size-7.5 shrink-0 items-center justify-center rounded-control group-hover:brightness-125"
          style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)` }}
        >
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span
            className="font-mono text-[9px] font-medium tracking-[0.06em]"
            style={{ color: accent }}
          >
            {visual.label}
          </span>
          <span
            title={data.titleTooltip ?? data.title}
            className="truncate text-title-14 text-text-primary"
          >
            {data.title}
          </span>
        </span>
      </div>
      <span
        title={data.detailTooltip ?? data.detail}
        className="truncate rounded-lg bg-surface-tinted px-2.5 py-2 text-code-11 text-text-secondary"
      >
        {data.detail}
      </span>
    </div>
  );
}
