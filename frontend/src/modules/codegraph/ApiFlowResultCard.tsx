import { Workflow } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { ApiFlow } from "./apiFlowResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

/**
 * ToolResultView for `codegraph.getApiFlow` (docs/dev-kit/tool-result-view.mdx).
 * The chain the tool walked, Controller → Service → Repository, reduced from
 * its own rawOutput — no re-fetch, no store lookup for the body. "Reveal in
 * Code Graph" goes through the existing `codegraph.reveal` intent, which does
 * look the endpoint up in the module's store, but only once the user asks.
 */
export function ApiFlowResultCard({ flow, endpointId }: { flow: ApiFlow; endpointId: string }) {
  return (
    <ToolResultCard
      icon={Workflow}
      title={`${flow.steps.length} step${flow.steps.length === 1 ? "" : "s"}: ${flow.steps.map((s) => s.label).join(" → ")}`}
      actionLabel="Reveal in Code Graph"
      onAction={() =>
        void dispatchIntent(
          "codegraph.reveal",
          {
            module: "codegraph",
            kind: "endpoint",
            id: endpointId,
            label: flow.steps[0]?.label ?? endpointId,
          },
          { placement: "beside" },
        )
      }
    >
      <ul className="flex flex-col gap-0.5">
        {flow.steps.map((step, i) => (
          <li key={`${step.filePath}:${step.line}:${i}`} className="truncate text-code-12 text-text-tertiary">
            {step.label}
            {step.filePath ? (
              <span className="text-text-tertiary/70">
                {" "}
                — {fileName(step.filePath)}
                {step.line ? `:${step.line}` : ""}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </ToolResultCard>
  );
}
