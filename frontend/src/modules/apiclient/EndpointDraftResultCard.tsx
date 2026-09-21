import { Send } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointDraft } from "./endpointDraftResult";

/**
 * ToolResultView for `api.fromEndpoint` (docs/dev-kit/tool-result-view.mdx).
 * Method + URL, the note about the body (if any) — never the body itself,
 * which can carry real request data; "Try in API Client" opens the drafted
 * request through the existing `api.fromEndpoint` intent.
 */
export function EndpointDraftResultCard({ draft, endpointId }: { draft: EndpointDraft; endpointId: string }) {
  return (
    <ToolResultCard
      icon={Send}
      title={`${draft.method} ${draft.url}`}
      subtitle={draft.note || (draft.bodyInferred ? "Body inferred from source" : undefined)}
      actionLabel="Try in API Client"
      onAction={() =>
        void dispatchIntent(
          "api.fromEndpoint",
          { module: "codegraph", kind: "endpoint", id: endpointId, label: `${draft.method} ${draft.url}` },
          { placement: "beside" },
        )
      }
    />
  );
}
