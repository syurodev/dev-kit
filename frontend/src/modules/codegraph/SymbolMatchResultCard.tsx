import { Search } from "lucide-react";
import { FramedCard, FramedCardHeader, FramedCardMore, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { SymbolMatch } from "./searchResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

const MAX_ROWS = 10;

/**
 * `codegraph.searchSymbol` — the matches, each its own way into Code Graph.
 * No header action: there is no one thing to open, every row is one.
 */
export function SymbolMatchResultCard({ matches }: { matches: SymbolMatch[] }) {
  const shown = matches.slice(0, MAX_ROWS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Search} title="Symbols" meta={`${matches.length} match${matches.length === 1 ? "" : "es"}`} />
      <FramedCardPreview flush>
        {shown.map((match) => (
          <FramedCardRow
            key={match.nodeId}
            label={match.label}
            trailing={match.kind || undefined}
            detail={match.filePath ? `${fileName(match.filePath)}${match.line ? `:${match.line}` : ""}` : undefined}
            onClick={() =>
              void dispatchIntent(
                "codegraph.reveal",
                { module: "codegraph", kind: "node", id: match.nodeId, label: match.label },
                { placement: "beside" },
              )
            }
          />
        ))}
        <FramedCardMore count={matches.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
