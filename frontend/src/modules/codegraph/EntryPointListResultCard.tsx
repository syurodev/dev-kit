import { Code } from "lucide-react";
import { FramedCard, FramedCardHeader, FramedCardMore, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EntryPointMatch } from "./searchResult";

const MAX_ROWS = 10;

/** `codegraph.listEndpoints` — verb and target per row, each opening its entry point in Code Graph. */
export function EntryPointListResultCard({ entries }: { entries: EntryPointMatch[] }) {
  const shown = entries.slice(0, MAX_ROWS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Code}
        title="Entry points"
        meta={`${entries.length} entry point${entries.length === 1 ? "" : "s"}`}
      />
      <FramedCardPreview flush>
        {shown.map((entry) => (
          <FramedCardRow
            key={entry.id}
            label={
              <>
                <span className="text-code-12-medium">{entry.verb}</span>{" "}
                <span className="text-code-12">{entry.target}</span>
              </>
            }
            onClick={() =>
              void dispatchIntent(
                "codegraph.reveal",
                {
                  module: "codegraph",
                  // The ref kind stays "endpoint": agents and saved
                  // transcripts address entry points by it.
                  kind: "endpoint",
                  id: entry.id,
                  label: `${entry.verb} ${entry.target}`,
                },
                { placement: "beside" },
              )
            }
          />
        ))}
        <FramedCardMore count={entries.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
