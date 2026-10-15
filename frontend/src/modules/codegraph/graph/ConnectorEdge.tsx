import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  Position,
  type EdgeProps,
} from "@xyflow/react";
import { EDGE } from "./tokens";

export type ConnectorEdgeData = {
  /** codegraph.Edge.kind — "di" renders dashed and labels itself "injects". */
  kind: string;
  /** Labels default off: a sequential call adds no information by saying "calls". */
  showLabel?: boolean;
  /**
   * Overrides the derived text. Plan edges carry a human-written `condition`
   * ("only when type == REFUND") — far more informative than "calls", and the
   * whole reason the plan canvas shows labels at all.
   */
  label?: string;
  /** The tooltip of the label: the full guard behind a short frame label. */
  title?: string;
  /** Off the flow through the hovered card (focus.ts): its label fades with it. */
  dimmed?: boolean;
  /** On that flow: drawn in the primary ink, so the chain reads as one line. */
  lit?: boolean;
};

/** Room between a label at the arrowhead and the card: the arrow itself. */
const LABEL_ARROW_GAP = 24;

export function ConnectorEdge({ id, data, markerEnd, ...props }: EdgeProps) {
  const kind = (data as ConnectorEdgeData | undefined)?.kind ?? "call";
  const showLabel = (data as ConnectorEdgeData | undefined)?.showLabel ?? false;
  const label = (data as ConnectorEdgeData | undefined)?.label;
  const dimmed = (data as ConnectorEdgeData | undefined)?.dimmed ?? false;
  const lit = (data as ConnectorEdgeData | undefined)?.lit ?? false;
  const title = (data as ConnectorEdgeData | undefined)?.title;
  // An injection and a call into an override are both "not a call you wrote here".
  const dashed = kind === "di" || kind === "implements";

  const [path, midX, midY] = getSmoothStepPath({
    ...props,
    borderRadius: EDGE.elbowRadius,
  });
  // A branch enters its card from the left after running down beside the
  // column it leaves, and the path's midpoint lands behind the card below the
  // caller. Its label sits at the arrowhead instead, on the run into the card.
  const atArrow = props.targetPosition === Position.Left;
  const transform = atArrow
    ? `translate(-100%, -50%) translate(${props.targetX - LABEL_ARROW_GAP}px, ${props.targetY}px)`
    : `translate(-50%, -50%) translate(${midX}px, ${midY}px)`;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: lit ? "var(--text-primary)" : "var(--icon-secondary)",
          strokeWidth: lit ? EDGE.strokeWidth + 0.5 : EDGE.strokeWidth,
          transition: "stroke 150ms, stroke-width 150ms",
          strokeDasharray: dashed ? EDGE.dashArray : undefined,
        }}
      />
      {showLabel && (
        <EdgeLabelRenderer>
          <div
            // Above the cards (z-10), and it takes the pointer so the tooltip
            // of a clipped label (a guard, a plan condition) can open; nodrag
            // and nopan keep a press on it from moving the canvas.
            className={`nodrag nopan pointer-events-auto absolute z-10 transition-opacity duration-150${dimmed ? " opacity-15" : ""}`}
            style={{ transform }}
          >
            <EdgeLabelPill kind={kind === "di" ? "di" : "call"} title={title ?? label}>
              {label ?? (kind === "di" ? "injects" : "calls")}
            </EdgeLabelPill>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

/**
 * The DI variant stacks a tint over an opaque base rather than using one
 * low-opacity fill: the canvas has a dot grid behind it, and a translucent chip
 * lets the dots read straight through the label.
 *
 * A plan edge's condition is a written sentence ("rotate credential → xoá
 * cache platform client id"), not a word like "calls" — unbounded, that
 * stretches the pill into an oversized capsule that overlaps whatever else
 * shares the canvas. Capped and truncated, with the full text as a tooltip.
 */
export function EdgeLabelPill({
  kind,
  title,
  children,
}: {
  kind: "call" | "di";
  title?: string;
  children: string;
}) {
  const blue = "var(--hue-blue)";
  return (
    <span
      title={title}
      className="block max-w-52 truncate rounded-full border px-2.25 py-1 text-code-11 shadow-canvas-chip"
      style={
        kind === "di"
          ? {
              backgroundColor: `color-mix(in srgb, ${blue} 12%, var(--surface-raised))`,
              borderColor: `color-mix(in srgb, ${blue} 55%, transparent)`,
              color: "var(--text-primary)",
            }
          : {
              backgroundColor: "var(--surface-raised)",
              borderColor: "var(--border-subtle)",
              color: "var(--text-secondary)",
            }
      }
    >
      {children}
    </span>
  );
}
