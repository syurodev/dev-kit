import { ChevronLeft, ChevronRight, Download, RefreshCw, SquareArrowOutUpRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/**
 * A table tab's controls: what to filter by, what to order by, and where in the
 * table you are.
 *
 * Two boxes rather than a SQL editor. Opening a table is a request to see its
 * data, and an editor spending most of the height on a statement you did not
 * ask to write inverts that — the two things anyone actually changes while
 * browsing are the filter and the sort. Anything beyond them opens in a
 * console, which is what the last button is for.
 */
export function TableToolbar({
  where,
  onWhereChange,
  onApply,
  orderLabel,
  pageIndex,
  rowCount,
  hasNext,
  keyed,
  loading,
  onStep,
  onRefresh,
  onExport,
  onOpenInConsole,
  // A14: destructured, not only typed. The verbatim pattern stopped above and
  // the row range below referred to a name that did not exist.
  pageSize,
}: {
  where: string;
  onWhereChange: (value: string) => void;
  onApply: () => void;
  orderLabel: string;
  pageIndex: number;
  rowCount: number;
  hasNext: boolean;
  keyed: boolean;
  loading: boolean;
  onStep: (step: 1 | -1) => void;
  onRefresh: () => void;
  /**
   * A28: exports the rows this tab already has. The table tab lost Export when
   * it stopped being a console, which left "export this table" meaning open a
   * console, re-run the query, then export — a round trip for rows that are
   * already on screen. Disabled with no rows, since there is nothing to write.
   */
  onExport: () => void;
  onOpenInConsole: () => void;
  /**
   * A2: rows per page, from the store's BROWSE_PAGE_SIZE rather than a literal
   * 200 spelled here. The row range is the one thing in this component that
   * would go on claiming 200 after the page size changed, and it would look
   * right while being wrong.
   */
  pageSize: number;
}) {
  const first = pageIndex * pageSize + 1;
  const last = pageIndex * pageSize + rowCount;

  return (
    <div className="flex shrink-0 flex-col gap-2 pb-2">
      <div className="flex items-center gap-2">
        <span className="shrink-0 text-code-11 text-text-tertiary">WHERE</span>
        <Input
          value={where}
          onChange={(event) => onWhereChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              onApply();
            }
          }}
          placeholder="status = 'open'"
          aria-label="Filter rows"
          className="h-(--control-height-md) border-border-subtle bg-surface-well text-code-12"
        />
        <span className="shrink-0 text-code-11 text-text-tertiary">{orderLabel}</span>
      </div>

      <div className="flex items-center gap-2">
        <Button variant="ghost" size="xs" onClick={onRefresh} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : undefined} />
          Refresh
        </Button>
        <Button variant="ghost" size="xs" onClick={onExport} disabled={rowCount === 0}>
          <Download />
          Export
        </Button>
        <Button variant="ghost" size="xs" onClick={onOpenInConsole}>
          <SquareArrowOutUpRight />
          Open in console
        </Button>

        <span className="flex-1" />

        {/* Row numbers, not a total: a COUNT(*) on a large table is a full scan
            every time you open it, and "of 501+" is the honest shape when the
            answer is only known one page ahead. */}
        <span className="text-code-11 text-text-tertiary tabular-nums">
          {rowCount === 0 ? "no rows" : `${first}–${last}${hasNext ? "+" : ""}`}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Previous page"
          onClick={() => onStep(-1)}
          disabled={loading || pageIndex === 0}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Next page"
          onClick={() => onStep(1)}
          disabled={loading || !hasNext}
        >
          <ChevronRight />
        </Button>
        {!keyed ? (
          // Worth saying: without a key the walk is by offset, which the server
          // pays for page by page, and a row inserted meanwhile shifts the
          // window under you.
          <span
            className="text-code-11 text-text-tertiary"
            title="This table has no primary or unique key, so pages are counted rather than walked. Deep pages are slower, and rows inserted while you page can shift the window."
          >
            unkeyed
          </span>
        ) : null}
      </div>
    </div>
  );
}
