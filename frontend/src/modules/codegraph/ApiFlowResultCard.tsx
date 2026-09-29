import { Workflow } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardMore, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { ApiFlow } from "./apiFlowResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

const MAX_STEPS = 6;

/**
 * `codegraph.getApiFlow` — the chain from the entry point down, one step per row
 * with where it lives. Reveal opens the whole flow in Code Graph, beside the
 * conversation.
 */
export function ApiFlowResultCard({ flow, entryPointId }: { flow: ApiFlow; entryPointId: string }) {
  const first = flow.steps[0];
  const shown = flow.steps.slice(0, MAX_STEPS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Workflow}
        title="API flow"
        meta={`${flow.steps.length} step${flow.steps.length === 1 ? "" : "s"}`}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "codegraph.reveal",
                { module: "codegraph", kind: "endpoint", id: entryPointId, label: first?.label ?? entryPointId },
                { placement: "beside" },
              )
            }
          >
            Reveal
          </FramedCardAction>
        }
      />
      <FramedCardPreview flush>
        {shown.map((step, index) => (
          <FramedCardRow
            key={`${step.label}:${index}`}
            label={
              <>
                <span className="text-text-tertiary tabular-nums">{index + 1}. </span>
                {step.label}
              </>
            }
            trailing={step.filePath ? `${fileName(step.filePath)}${step.line ? `:${step.line}` : ""}` : undefined}
            detail={step.summary}
          />
        ))}
        <FramedCardMore count={flow.steps.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
