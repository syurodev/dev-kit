import { useLayoutEffect, useRef, useState, type Ref } from "react";
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
  /**
   * "arrow": a branch's label sits at its arrowhead as just its order number
   * ("2" of "2 · if (ok)"), the rest in the tooltip. Only the flow document
   * asks for it; without it the label stays at the path's midpoint.
   */
  labelAt?: "arrow";
  /** Off the flow through the hovered card (focus.ts): its label fades with it. */
  dimmed?: boolean;
  /** On that flow: drawn in the primary ink, so the chain reads as one line. */
  lit?: boolean;
};

/** Room between a label at the arrowhead and the card: the arrow itself, so the chip stays clear of the card it leaves (columns are ~40px apart). */
const LABEL_ARROW_GAP = 14;

export function ConnectorEdge({ id, data, markerEnd, ...props }: EdgeProps) {
  const d = data as ConnectorEdgeData | undefined;
  const kind = d?.kind ?? "call";
  const showLabel = d?.showLabel ?? false;
  const label = d?.label;
  const dimmed = d?.dimmed ?? false;
  const lit = d?.lit ?? false;
  const title = d?.title;
  // An injection and a call into an override are both "not a call you wrote here".
  const dashed = kind === "di" || kind === "implements";

  const [path, midX, midY] = getSmoothStepPath({
    ...props,
    borderRadius: EDGE.elbowRadius,
  });
  // A branch enters its card from the left after running down beside the
  // column it leaves, and the path's midpoint lands behind the card below the
  // caller. Asked for, its label sits at the arrowhead instead, on the run
  // into the card — as the order number alone, small enough not to cover
  // the card beside it.
  const atArrow = d?.labelAt === "arrow" && props.targetPosition === Position.Left && !!label;
  const text = label ?? (kind === "di" ? "injects" : "calls");
  const shown = atArrow ? text.split(" · ")[0] : text;
  const tooltip = atArrow
    ? [text, title && title !== label ? title : ""].filter(Boolean).join("\n")
    : (title ?? label);

  // The label takes the pointer only when its tooltip says more than the pill
  // does: a guard behind a frame label, a number standing for its label, or a
  // pill the width cap clipped. Everything else stays click-through, and a
  // dimmed label never catches the pointer.
  const pill = useRef<HTMLSpanElement>(null);
  const [clipped, setClipped] = useState(false);
  useLayoutEffect(() => {
    const el = pill.current;
    setClipped(!!el && el.scrollWidth > el.clientWidth);
  }, [shown, showLabel]);
  const interactive = !dimmed && (clipped || (!!tooltip && tooltip !== shown));

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
            className={`nodrag nopan absolute transition-opacity duration-150 ${interactive ? "pointer-events-auto" : "pointer-events-none"}${atArrow ? " z-10" : ""}${dimmed ? " opacity-15" : ""}`}
            style={{
              transform: atArrow
                ? `translate(-100%, -50%) translate(${props.targetX - LABEL_ARROW_GAP}px, ${props.targetY}px)`
                : `translate(-50%, -50%) translate(${midX}px, ${midY}px)`,
            }}
          >
            <EdgeLabelPill ref={pill} kind={kind === "di" ? "di" : "call"} title={tooltip}>
              {shown}
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
  ref,
  kind,
  title,
  children,
}: {
  ref?: Ref<HTMLSpanElement>;
  kind: "call" | "di";
  title?: string;
  children: string;
}) {
  const blue = "var(--hue-blue)";
  return (
    <span
      ref={ref}
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
