import { useCallback, useMemo, useState } from "react";
import { Network } from "lucide-react";
import type { PlanNode, QueryPlan } from "@/bridge/database";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { PlanDiagram } from "../plan/PlanDiagram";
import { planGraph } from "../plan/planGraph";
import { openAgentSql } from "./openAgentSql";

/** Room around the graph inside the card's box: the canvas's 12px fit padding, each side. */
const FIT_PADDING_PX = 24;
/** The box never shrinks below a short single step's worth, or grows past h-112. */
const MIN_HEIGHT_PX = 160;
const MAX_HEIGHT_PX = 448;

/** The box's height for a graph of `height` px: the graph at 100%, within the bounds above. */
export function planBoxHeight(height: number): number {
  return Math.min(MAX_HEIGHT_PX, Math.max(MIN_HEIGHT_PX, Math.ceil(height) + FIT_PADDING_PX));
}

function stepCount(nodes: PlanNode[]): number {
  return nodes.reduce((sum, node) => sum + 1 + stepCount(node.children ?? []), 0);
}

/**
 * `db.explain` — the plan, drawn the way the console's Explain draws it, on
 * the canvas's compact face (no toolbar strip, never zoomed past 100%).
 * A concrete height because React Flow measures its parent: a canvas in a box
 * sized by its content draws nothing. It starts at h-72 and, once the graph is
 * laid out, takes the graph's own height, so a two-step plan reads at full
 * size instead of shrunk into 288px, and a deep one stops at h-112 and zooms
 * out.
 */
export function PlanResultCard({
  plan,
  sql,
  profileId,
  heading,
}: {
  plan: QueryPlan;
  sql: string;
  /** The connection the agent planned it on — compared with the console's when it opens. */
  profileId?: string;
  heading?: string;
}) {
  const graph = useMemo(() => planGraph(plan), [plan]);
  const [height, setHeight] = useState<number | null>(null);
  const onGraphSize = useCallback((size: { height: number }) => setHeight(planBoxHeight(size.height)), []);
  const steps = stepCount(plan.nodes);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Network}
        title={heading ?? "Plan"}
        meta={`${steps} ${steps === 1 ? "step" : "steps"}${plan.analyzed ? " · analyzed" : ""}`}
        action={
          sql ? (
            <FramedCardAction onClick={() => void openAgentSql(sql, profileId)}>Open in Database</FramedCardAction>
          ) : null
        }
      />
      <FramedCardPreview flush className="py-0">
        <div className="flex h-72 overflow-hidden" style={height === null ? undefined : { height }}>
          <PlanDiagram graph={graph} compact onGraphSize={onGraphSize} />
        </div>
      </FramedCardPreview>
    </FramedCard>
  );
}
