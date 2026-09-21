import { Send } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointDraft } from "./endpointDraftResult";

/**
 * ToolResultView for `api.fromEndpoint` (docs/dev-kit/tool-result-view.mdx).
 * Method + URL, the note about the body (if any) — never the body itself,
 * which can carry real request data. The whole row opens the draft through
 * the existing `api.fromEndpoint` intent.
 */
export function EndpointDraftResultCard({ draft, endpointId }: { draft: EndpointDraft; endpointId: string }) {
  return (
    <ToolResultRow
      icon={Send}
      label={
        <>
          <span className="text-code-12-medium">{draft.method}</span> {draft.url}
        </>
      }
      detail={draft.note || (draft.bodyInferred ? "Body inferred from source" : undefined)}
      onClick={() =>
        void dispatchIntent(
          "api.fromEndpoint",
          { module: "codegraph", kind: "endpoint", id: endpointId, label: `${draft.method} ${draft.url}` },
          { placement: "beside" },
        )
      }
    />
  );
}
