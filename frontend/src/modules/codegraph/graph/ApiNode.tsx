import { Handle, Position, type NodeProps } from "@xyflow/react";
import { EFFECT_CHIP, ICON_BADGE, LAYERS, layerOf } from "./tokens";
import { cn } from "@/lib/utils";

export type ApiNodeData = {
  layer: string;
  title: string;
  detail: string;
  /** Full text for the native tooltip, since the card truncates both lines. */
  titleTooltip?: string;
  detailTooltip?: string;
  /** The small line above the title: the class a method belongs to. */
  caption?: string;
  /** The doc comment's first sentence; takes the detail well's place when there is one. */
  summary?: string;
  /** What an extractor saw in the body: "Kafka produce", "HTTP call". */
  effects?: string[];
  /** Calls the outline folds away (logging, metrics, helpers), counted since they are not drawn. */
  hiddenCalls?: number;
};

/**
 * Figma `API Node` (162:183): 268 wide, radius 14, padding 12, a 30px icon
 * ring, a known layer's name in caps beside the class, the method as the
 * title, and the doc comment's first sentence (or the file chip when there is
 * none).
 *
 * State uses a neutral border ramp (subtle -> tertiary -> primary 2px) rather
 * than the layer colour. The focus ring is --text-primary, and a layer-coloured
 * ring would make "selected" and "which layer" say the same thing. Layer
 * meaning lives in the icon and label only (controller = --hue-blue, service =
 * --hue-green, repository = --hue-yellow).
 *
 * Keyboard: the React Flow wrapper around this card is the focusable element
 * (a named role="button", see FlowCanvas), so the ring is drawn from its
 * focus-visible state rather than by a second tab stop inside the card.
 */
export function ApiNode({ data, selected }: NodeProps & { data: ApiNodeData }) {
  const layer = layerOf(data.layer);
  const visual = LAYERS[layer];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;
  // "UNKNOWN" on most cards of a Camel or plain-Java service said nothing and
  // took a line; a known layer keeps its colour and caps label.
  const knownLayer = layer !== "unknown";

  return (
    <div
      className={cn(
        "group flex w-67 flex-col gap-2.5 rounded-lg bg-surface-raised p-3 in-focus-visible:ring-2 in-focus-visible:ring-text-primary",
        selected
          ? "border-2 border-text-primary shadow-canvas-node-selected"
          : "border border-border-subtle shadow-canvas-node hover:border-text-tertiary hover:shadow-canvas-node-hover",
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
          {(knownLayer || data.caption) && (
            <span className="flex min-w-0 items-center gap-1.5">
              {knownLayer && (
                <span className="shrink-0 text-label-11-caps" style={{ color: accent }}>
                  {visual.label}
                </span>
              )}
              {data.caption && (
                <span title={data.titleTooltip} className="truncate text-caption-11 text-text-tertiary">
                  {data.caption}
                </span>
              )}
            </span>
          )}
          <span title={data.titleTooltip ?? data.title} className="truncate text-title-14 text-text-primary">
            {data.title}
          </span>
        </span>
      </div>
      {data.summary ? (
        <span title={data.detailTooltip ?? data.detail} className="line-clamp-2 text-body-12 text-text-secondary">
          {data.summary}
        </span>
      ) : (
        <span
          title={data.detailTooltip ?? data.detail}
          className="truncate rounded-md bg-state-soft px-2.5 py-1.75 text-code-11 text-text-secondary"
        >
          {data.detail}
        </span>
      )}
      {data.effects?.length || data.hiddenCalls ? (
        <span className="flex flex-wrap items-center gap-1.5 text-caption-11 text-text-tertiary">
          {data.effects?.map((effect) => (
            <span key={effect} className={EFFECT_CHIP}>
              {effect}
            </span>
          ))}
          {data.hiddenCalls ? (
            <span>
              +{data.hiddenCalls} hidden {data.hiddenCalls === 1 ? "call" : "calls"}
            </span>
          ) : null}
        </span>
      ) : null}
    </div>
  );
}
