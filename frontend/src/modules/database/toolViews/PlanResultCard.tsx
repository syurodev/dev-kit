import { useMemo } from "react";
import { Network } from "lucide-react";
import type { PlanNode, QueryPlan } from "@/bridge/database";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { PlanDiagram } from "../plan/PlanDiagram";
import { planGraph } from "../plan/planGraph";
import { openAgentSql } from "./openAgentSql";

function stepCount(nodes: PlanNode[]): number {
  return nodes.reduce((sum, node) => sum + 1 + stepCount(node.children ?? []), 0);
}

/**
 * `db.explain` — the plan, drawn the way the console's Explain draws it.
 * A fixed height because React Flow measures its parent: a canvas in a box
 * sized by its content draws nothing.
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
  const steps = stepCount(plan.nodes);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Network}
        title={heading ?? "Plan"}
        meta={`${steps} ${steps === 1 ? "step" : "steps"}${plan.analyzed ? " · analyzed" : ""}`}
        action={
          sql ? (
            <FramedCardAction onClick={() => openAgentSql(sql, profileId)}>Open in Database</FramedCardAction>
          ) : null
        }
      />
      <FramedCardPreview flush className="py-0">
        <div className="flex h-72 overflow-hidden">
          <PlanDiagram graph={graph} />
        </div>
      </FramedCardPreview>
    </FramedCard>
  );
}
