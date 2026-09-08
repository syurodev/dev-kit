import { Handle, Position, type NodeProps } from "@xyflow/react";
import { ICON_BADGE, LAYERS, layerOf } from "./tokens";
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
 * Figma `API Node` (162:183): 268 wide, radius 14, padding 12, a 30px icon
 * ring, the layer name in caps over the title, and a state-soft detail well.
 *
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
        "group flex w-67 flex-col gap-2.5 rounded-lg bg-surface-raised p-3",
        selected
          ? "border-2 border-text-primary shadow-[0_6px_8px_rgba(0,0,0,0.45)]"
          : "border border-border-subtle shadow-[0_1px_1.5px_rgba(0,0,0,0.18)] hover:border-text-tertiary hover:shadow-[0_4px_6px_rgba(0,0,0,0.35)]",
      )}
    >
      {/* Handles are the anchors the connector geometry attaches to: a straight
          run enters the top, a branch enters the left. Both are invisible —
          this is a read-only view, nobody draws edges by hand. */}
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span className={ICON_BADGE}>
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="text-label-11-caps" style={{ color: accent }}>
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
        className="truncate rounded-md bg-state-soft px-2.5 py-1.75 text-code-11 text-text-secondary"
      >
        {data.detail}
      </span>
    </div>
  );
}
