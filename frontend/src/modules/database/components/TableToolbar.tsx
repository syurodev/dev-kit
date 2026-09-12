import type { ReactNode } from "react";
import type { SchemaNamespace } from "@/bridge/database";
import {
  ArrowDownAZ,
  ArrowUpAZ,
  ArrowUpDown,
  Check,
  Download,
  FileCode,
  Filter,
  RefreshCw,
  SquareArrowOutUpRight,
  Undo2,
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
import { cn } from "@/lib/utils";
import { WhereInput } from "./WhereInput";

/**
 * A table tab's controls: what to do with the rows, then what decides which
 * rows they are.
 *
 * Two rows, in DataGrip's order. The first is a dense strip of icons with the
 * pager at its right — every one of them acts on the tab. The second is the
 * query: the filter you type and the sort the grid is under, split across the
 * full width with a rule between them.
 *
 * Neither box on the second row is drawn until you touch it. A border around
 * each makes them read as two widgets bolted to a toolbar; without one they
 * read as the single line of query they are, which is the whole of what makes
 * DataGrip's header look half the size of ours at the same height.
 *
 * Two boxes rather than a SQL editor. Opening a table is a request to see its
 * data, and an editor spending most of the height on a statement you did not
 * ask to write inverts that — the two things anyone actually changes while
 * browsing are the filter and the sort. Anything beyond them opens in a
 * console, which is what the last button is for.
 */

/** A vertical rule between groups of the icon strip. */
function Rule() {
  return <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border-subtle" />;
}

/**
 * A control that is drawn but not wired up yet.
 *
 * Disabled rather than inert-but-clickable, and the reason is in the tooltip:
 * a button that answers a click with nothing is indistinguishable from one
 * that is broken, which is exactly the report that started this work. The
 * `<span>` carries the title because the Button sets `pointer-events: none`
 * when disabled, and an element the pointer cannot reach has no tooltip.
 */
function Pending({ title, children }: { title: string; children: ReactNode }) {
  return (
    <span title={`${title} — not wired up yet`} className="flex shrink-0 items-center">
      {children}
    </span>
  );
}

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
    <div className="flex shrink-0 flex-col pb-2">
      <div className="flex h-(--control-height-md) items-center gap-0.5">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRefresh}
          disabled={loading}
          title="Reload this page of rows"
        >
          <RefreshCw className={loading ? "animate-spin" : undefined} />
        </Button>

        <Rule />

        {/* Transaction control. Drawn now, wired up later — the shape of the
            strip is what is being settled, and leaving a hole where these go
            would settle it wrong. */}
        <Pending title="Commit the open transaction">
          <Button variant="ghost" size="icon-sm" disabled>
            <Check />
          </Button>
        </Pending>
        <Pending title="Roll back the open transaction">
          <Button variant="ghost" size="icon-sm" disabled>
            <Undo2 />
          </Button>
        </Pending>
        <Pending title="Transaction mode">
          <Button
            variant="ghost"
            size="sm"
            disabled
            className="gap-1 px-2 text-code-12 font-normal"
          >
            <ArrowUpDown />
            Tx: Auto
          </Button>
        </Pending>
        <Pending title="Show this table's DDL">
          <Button
            variant="ghost"
            size="sm"
            disabled
            className="gap-1 px-2 text-code-12 font-normal"
          >
            <FileCode />
            DDL
          </Button>
        </Pending>

        <Rule />

        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onExport}
          disabled={rowCount === 0}
          title="Export the rows on this page"
        >
          <Download />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onOpenInConsole}
          title="Open this query in a console"
        >
          <SquareArrowOutUpRight />
        </Button>

        <span className="flex-1" />

        {pager}
      </div>

      <div className="flex h-(--control-height-md) items-center gap-2 border-t border-border-subtle">
        <div className="flex min-w-0 flex-[3] items-center gap-1.5">
          <Filter aria-hidden className="size-3 shrink-0 text-text-tertiary" />
          <span className="shrink-0 text-code-11 tracking-wider text-text-tertiary">WHERE</span>
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
        </div>

        <span aria-hidden className="h-4 w-px shrink-0 bg-border-subtle" />

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
              className="min-w-0 flex-[2] justify-start gap-1.5 px-1.5 font-normal"
            >
              {orderBy ? (
                orderBy.direction === "asc" ? (
                  <ArrowUpAZ className="text-text-tertiary" />
                ) : (
                  <ArrowDownAZ className="text-text-tertiary" />
                )
              ) : (
                <ArrowUpDown className="text-text-tertiary" />
              )}
              <span className="shrink-0 text-code-11 tracking-wider text-text-tertiary">
                ORDER BY
              </span>
              <span
                className={cn(
                  "min-w-0 truncate text-code-12",
                  orderBy ? "text-text-primary" : "text-text-tertiary",
                )}
              >
                {orderBy ? orderBy.column : ""}
              </span>
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
    </div>
  );
}
