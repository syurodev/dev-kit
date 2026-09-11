import type { ReactNode } from "react";
import type { SchemaNamespace } from "@/bridge/database";
import {
  ArrowDownAZ,
  ArrowUpAZ,
  ChevronsUpDown,
  Download,
  RefreshCw,
  SquareArrowOutUpRight,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { WhereInput } from "./WhereInput";

/**
 * A table tab's controls: what to filter by, what to order by, what to do with
 * the rows, and where you are in them.
 *
 * Two rows, split by what they are. The first is the query — the filter you
 * type and the sort the grid is under, the two things that decide which rows
 * come back. The second is what you do with the rows that did: the actions on
 * the left, the pager on the right, where the row had dead space.
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
  engine,
  indexed,
  schema,
  table,
  orderBy,
  columns,
  onOrderByChange,
  rowCount,
  loading,
  onRefresh,
  onExport,
  onOpenInConsole,
  pager,
}: {
  where: string;
  onWhereChange: (value: string) => void;
  onApply: () => void;
  /** Everything the filter box completes against — see WhereInput. */
  engine: string | undefined;
  indexed: SchemaNamespace | undefined;
  schema: string;
  table: string;
  /** The sort the grid is under, or null for the table's own order. */
  orderBy: { column: string; direction: "asc" | "desc" } | null;
  /** The result's columns, to pick one from. Empty before the first page lands. */
  columns: readonly string[];
  onOrderByChange: (next: { column: string; direction: "asc" | "desc" } | null) => void;
  /** Rows on this page. Only Export reads it — with none there is nothing to write. */
  rowCount: number;
  loading: boolean;
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
   * The pager, as a slot. Ten pagination props threaded through a toolbar that
   * reads none of them is prop drilling with extra steps — the toolbar owes
   * this one a place to sit and nothing else.
   */
  pager?: ReactNode;
}) {
  return (
    <div className="flex shrink-0 flex-col gap-2 pb-2">
      <div className="flex items-center gap-2">
        <span className="shrink-0 text-code-11 text-text-tertiary">WHERE</span>
        <WhereInput
          value={where}
          onChange={onWhereChange}
          onApply={onApply}
          engine={engine}
          indexed={indexed}
          schema={schema}
          table={table}
          columns={columns}
        />
        {/* A control, not a label. It sits beside the WHERE box styled like
            one, so reading as a control and doing nothing was the worst of
            both: the header cells set a sort, but only for a column you can
            see, and a 74-column table scrolls most of them off the right. */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              disabled={columns.length === 0}
              className="h-(--control-height-md) shrink-0 gap-1.5 border border-border-subtle bg-surface-well px-3 text-code-12 font-normal text-text-secondary"
            >
              {orderBy ? (
                orderBy.direction === "asc" ? (
                  <ArrowUpAZ className="text-text-tertiary" />
                ) : (
                  <ArrowDownAZ className="text-text-tertiary" />
                )
              ) : null}
              ORDER BY {orderBy ? orderBy.column : "—"}
              <ChevronsUpDown className="text-text-tertiary" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="max-h-96 overflow-y-auto">
            <DropdownMenuLabel>Sort by</DropdownMenuLabel>
            <DropdownMenuItem onSelect={() => onOrderByChange(null)}>
              No sort — the table&rsquo;s own order
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {columns.map((column) => {
              // One row per column, both directions on it: two rows per column
              // is 148 items on a 74-column table, and the direction is a
              // property of the sort rather than a sort of its own.
              const active = orderBy?.column === column;
              return (
                <DropdownMenuItem
                  key={column}
                  className="justify-between gap-4"
                  onSelect={(event) => {
                    event.preventDefault();
                    onOrderByChange({
                      column,
                      direction: active && orderBy.direction === "asc" ? "desc" : "asc",
                    });
                  }}
                >
                  <span className="truncate">{column}</span>
                  {active ? (
                    orderBy.direction === "asc" ? (
                      <ArrowUpAZ className="text-text-tertiary" />
                    ) : (
                      <ArrowDownAZ className="text-text-tertiary" />
                    )
                  ) : null}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
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

        {pager}
      </div>
    </div>
  );
}
