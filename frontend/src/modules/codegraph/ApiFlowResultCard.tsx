import { Workflow } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { ApiFlow } from "./apiFlowResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

/**
 * ToolResultView for `codegraph.getApiFlow` (docs/dev-kit/tool-result-view.mdx).
 * The chain the tool walked, Controller → Service → Repository, as the row's
 * own label — reduced from its own rawOutput, no re-fetch, no store lookup
 * for the body. The whole row opens it in Code Graph; "Reveal" isn't a
 * separate action, which is what `codegraph.reveal` does look a store up
 * for, but only once clicked.
 */
export function ApiFlowResultCard({ flow, endpointId }: { flow: ApiFlow; endpointId: string }) {
  const first = flow.steps[0];
  return (
    <ToolResultRow
      icon={Workflow}
      label={flow.steps.map((s) => s.label).join(" → ")}
      trailing={`${flow.steps.length} step${flow.steps.length === 1 ? "" : "s"}`}
      detail={first?.filePath ? `${fileName(first.filePath)}${first.line ? `:${first.line}` : ""}` : undefined}
      onClick={() =>
        void dispatchIntent(
          "codegraph.reveal",
          { module: "codegraph", kind: "endpoint", id: endpointId, label: first?.label ?? endpointId },
          { placement: "beside" },
        )
      }
    />
  );
}
