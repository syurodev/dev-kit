import { Code } from "lucide-react";
import { FramedCard, FramedCardHeader, FramedCardMore, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointMatch } from "./searchResult";

const MAX_ROWS = 10;

/** `codegraph.listEndpoints` — method and path per row, each opening its endpoint in Code Graph. */
export function EndpointListResultCard({ endpoints }: { endpoints: EndpointMatch[] }) {
  const shown = endpoints.slice(0, MAX_ROWS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Code}
        title="Endpoints"
        meta={`${endpoints.length} endpoint${endpoints.length === 1 ? "" : "s"}`}
      />
      <FramedCardPreview flush>
        {shown.map((endpoint) => (
          <FramedCardRow
            key={endpoint.id}
            label={
              <>
                <span className="text-code-12-medium">{endpoint.httpMethod}</span>{" "}
                <span className="text-code-12">{endpoint.path}</span>
              </>
            }
            onClick={() =>
              void dispatchIntent(
                "codegraph.reveal",
                {
                  module: "codegraph",
                  kind: "endpoint",
                  id: endpoint.id,
                  label: `${endpoint.httpMethod} ${endpoint.path}`,
                },
                { placement: "beside" },
              )
            }
          />
        ))}
        <FramedCardMore count={endpoints.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
