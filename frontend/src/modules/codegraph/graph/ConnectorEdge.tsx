import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
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

export function ConnectorEdge({ id, data, markerEnd, ...props }: EdgeProps) {
  const kind = (data as ConnectorEdgeData | undefined)?.kind ?? "call";
  const showLabel = (data as ConnectorEdgeData | undefined)?.showLabel ?? false;
  const label = (data as ConnectorEdgeData | undefined)?.label;
  const dimmed = (data as ConnectorEdgeData | undefined)?.dimmed ?? false;
  const lit = (data as ConnectorEdgeData | undefined)?.lit ?? false;
  const title = (data as ConnectorEdgeData | undefined)?.title;
  // An injection and a call into an override are both "not a call you wrote here".
  const dashed = kind === "di" || kind === "implements";

  const titled = !!title && title !== label;

  const [path, labelX, labelY] = getSmoothStepPath({
    ...props,
    borderRadius: EDGE.elbowRadius,
  });

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
            // A label that hides more than it shows (a clipped guard) takes
            // the pointer so its tooltip can open; the rest stay click-through.
            className={`nodrag nopan absolute transition-opacity duration-150${titled ? " pointer-events-auto" : " pointer-events-none"}${dimmed ? " opacity-15" : ""}`}
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
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
