import { Send } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointDraft } from "./endpointDraftResult";

/**
 * `api.fromEndpoint` — the request drafted from an endpoint: method and URL,
 * and a note about the body. Never the body itself: it may hold real data.
 */
export function EndpointDraftResultCard({ draft, endpointId }: { draft: EndpointDraft; endpointId: string }) {
  const note = draft.note || (draft.bodyInferred ? "Body inferred from source" : "");
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Send}
        title="Request draft"
        meta={draft.name || undefined}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "api.fromEndpoint",
                { module: "codegraph", kind: "endpoint", id: endpointId, label: `${draft.method} ${draft.url}` },
                { placement: "beside" },
              )
            }
          >
            Try in API Client
          </FramedCardAction>
        }
      />
      <FramedCardPreview>
        <p className="min-w-0 truncate text-code-12 text-text-primary" title={`${draft.method} ${draft.url}`}>
          <span className="text-code-12-medium">{draft.method}</span> {draft.url}
        </p>
        {note ? <p className="text-caption-11 text-text-tertiary">{note}</p> : null}
      </FramedCardPreview>
    </FramedCard>
  );
}
