import { useMemo, useState } from "react";
import { Copy, Table2 } from "lucide-react";
import { toast } from "sonner";
import { copyText } from "@/bridge/clipboard";
import { DataTable, type DataTableColumn } from "@/components/ui/data-table/DataTable";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { openAgentSql } from "./openAgentSql";
import { toCsv, writeSummary, type ParsedQueryResult } from "./parse";

/** Rows the card shows before Show all: enough to read, not a page. */
const PREVIEW_ROWS = 20;

/**
 * `db.query` — what the statement returned, as a table the reader can resize.
 * The cells are the strings the driver gave (F4: no column types), NULL for
 * SQL NULL. A write has no columns and says what it changed instead, and
 * offers nothing to open: a console holding an agent's write is one keystroke
 * from running it again, on whatever connection is selected here.
 */
export function QueryResultCard({
  result,
  sql,
  heading,
}: {
  result: ParsedQueryResult;
  sql: string;
  heading?: string;
}) {
  const [all, setAll] = useState(false);
  const write = result.columns.length === 0;
  const count = result.rows.length;
  const meta = write
    ? writeSummary(result.rowsAffected, sql)
    : `${count} ${count === 1 ? "row" : "rows"}${result.truncated ? " · truncated" : ""}`;
  const firstLine = sql.split("\n")[0] ?? "";
  const columns = useMemo<DataTableColumn[]>(
    () => result.columns.map((name, index) => ({ key: String(index), header: name, label: name, mono: true, nowrap: true })),
    [result.columns],
  );
  const rows = useMemo(() => (all ? result.rows : result.rows.slice(0, PREVIEW_ROWS)), [all, result.rows]);

  const copyCsv = () =>
    void copyText(toCsv(result.columns, result.rows))
      .then(() => toast.success("CSV copied"))
      .catch(() => toast.error("Could not copy CSV"));

  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Table2}
        title={heading ?? "Query"}
        meta={meta}
        action={
          write ? null : (
            <>
              <FramedCardAction opens={false} onClick={copyCsv}>
                <Copy aria-hidden="true" />
                Copy CSV
              </FramedCardAction>
              {sql ? (
                <FramedCardAction onClick={() => void openAgentSql(sql, undefined)}>Open in Database</FramedCardAction>
              ) : null}
            </>
          )
        }
      />
      <FramedCardPreview>
        {firstLine ? (
          <span title={sql} className="block min-w-0 truncate text-code-12 text-text-secondary">
            {firstLine}
          </span>
        ) : null}
        {write ? null : (
          <>
            {/* Measured from every returned row (DataTable reads a sample of
                them), not from the twenty on screen, so the sample — and the
                widths — are the same folded and opened: Show all adds rows
                without moving a column. */}
            <DataTable
              columns={columns}
              rows={rows}
              cellText={result.rows}
              dense
              maxHeight={all ? "max-h-120" : undefined}
              data-testid="query-result-table"
            />
            {count > PREVIEW_ROWS ? (
              <button
                type="button"
                onClick={() => setAll((open) => !open)}
                className="self-start rounded-sm px-(--space-micro) text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
              >
                {all ? `Show first ${PREVIEW_ROWS}` : `Show all ${count}`}
              </button>
            ) : null}
          </>
        )}
      </FramedCardPreview>
    </FramedCard>
  );
}
