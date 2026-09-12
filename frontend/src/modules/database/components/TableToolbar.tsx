import type { ReactNode } from "react";
import type { SchemaNamespace } from "@/bridge/database";
import {
  ArrowDownAZ,
  ArrowUpAZ,
  ArrowUpDown,
  Check,
  Download,
  FileCode,
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
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { errorMessage } from "@/bridge/errors";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { useDatabaseStore } from "../databaseStore";
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
 * Commit, Roll back, and the Auto/Manual switch.
 *
 * The transaction is the CONNECTION's, not this tab's: DevKit keeps one
 * connection per profile, so a console and a table tab on the same profile are
 * inside the same transaction — which is what every database tool does, and
 * why this row is disabled rather than hidden when there is no connection.
 *
 * Manual opens a transaction as soon as it is chosen, so the next statement is
 * inside one rather than the first silently landing outside. Commit and Roll
 * back end that transaction and open the next, because ending one is not the
 * same as leaving the mode; leaving the mode COMMITS what is open, since
 * discarding someone's work is what the button beside it is for.
 */
/**
 * A control that is drawn but not connected yet — DDL is the last of them.
 *
 * Disabled, so it stays out of the tab order and is announced as unavailable,
 * but NOT at the Button's `disabled:opacity-38`: that put it at 1.80:1 in
 * light, and a control nobody can read cannot say "coming soon" either.
 */
const PENDING = "disabled:opacity-100 disabled:text-text-tertiary";

function TransactionControls() {
  const { txManual, txOpen, txBusy, selectedProfileId, setTxManual, endTransaction } =
    useDatabaseStore(
      useShallow((s) => ({
        txManual: s.txManual,
        txOpen: s.txOpen,
        txBusy: s.txBusy,
        selectedProfileId: s.selectedProfileId,
        setTxManual: s.setTxManual,
        endTransaction: s.endTransaction,
      })),
    );

  const idle = !selectedProfileId || txBusy;
  const end = (action: "commit" | "rollback") => {
    endTransaction(action).catch((error: unknown) => {
      toast.error(action === "commit" ? "Could not commit" : "Could not roll back", {
        description: errorMessage(error),
      });
    });
  };

  return (
    <>
      <ActionTooltip
        label={txOpen ? "Commit the open transaction" : "Nothing to commit — Tx is on Auto"}
      >
        <span className="flex shrink-0 items-center">
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={idle || !txOpen}
            onClick={() => end("commit")}
          >
            <Check />
          </Button>
        </span>
      </ActionTooltip>
      <ActionTooltip
        label={txOpen ? "Roll back the open transaction" : "Nothing to roll back — Tx is on Auto"}
      >
        <span className="flex shrink-0 items-center">
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={idle || !txOpen}
            onClick={() => end("rollback")}
          >
            <Undo2 />
          </Button>
        </span>
      </ActionTooltip>

      <DropdownMenu>
        <ActionTooltip
          label={
            txManual
              ? "Manual commit — statements wait for Commit"
              : "Auto commit — every statement lands on its own"
          }
        >
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              disabled={idle}
              className={cn("gap-1 px-2 text-code-12 font-normal", txOpen && "text-status-warning")}
            >
              <ArrowUpDown />
              Tx: {txManual ? "Manual" : "Auto"}
              {/* An open transaction is holding locks on the user's server.
                  The dot is the only thing on screen that says so. */}
              {txOpen ? <span className="size-1.5 rounded-full bg-status-warning" /> : null}
            </Button>
          </DropdownMenuTrigger>
        </ActionTooltip>
        {/* Wide enough for the two lines: at the menu's own width they wrapped
            mid-word and the explanation was cut. */}
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel>Transaction mode</DropdownMenuLabel>
          <DropdownMenuItem
            onSelect={() => {
              setTxManual(false).catch((error: unknown) =>
                toast.error("Could not commit what was open", {
                  description: errorMessage(error),
                }),
              );
            }}
          >
            Auto — commit every statement
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              setTxManual(true).catch((error: unknown) =>
                toast.error("Could not open a transaction", { description: errorMessage(error) }),
              );
            }}
          >
            Manual — commit when I say
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
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
        <ActionTooltip label="Reload this page of rows">
          <Button variant="ghost" size="icon-sm" onClick={onRefresh} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
          </Button>
        </ActionTooltip>

        <Rule />

        <TransactionControls />

        <ActionTooltip label="Show this table's DDL — coming soon">
          <span className="flex shrink-0 items-center">
            <Button
              variant="ghost"
              size="sm"
              disabled
              className={cn("gap-1 px-2 text-code-12 font-normal", PENDING)}
            >
              <FileCode />
              DDL
            </Button>
          </span>
        </ActionTooltip>

        <Rule />

        <ActionTooltip label="Export the rows on this page">
          <Button variant="ghost" size="icon-sm" onClick={onExport} disabled={rowCount === 0}>
            <Download />
          </Button>
        </ActionTooltip>
        <ActionTooltip label="Open this query in a console">
          <Button variant="ghost" size="icon-sm" onClick={onOpenInConsole}>
            <SquareArrowOutUpRight />
          </Button>
        </ActionTooltip>

        <span className="flex-1" />

        {pager}
      </div>

      <div className="flex h-(--control-height-md) items-center gap-2 border-t border-border-subtle">
        {/* No glyph before the word. A funnel beside "WHERE" and a sort arrow
            beside "ORDER BY" say a second time what the word already says —
            they came from DataGrip, whose boxes carry no border and no label.
            Ours carry the label; 36px of the narrowest row in the header go
            back to the clause. */}
        <div className="flex min-w-0 flex-[3] items-center gap-2">
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
              <span className="shrink-0 text-code-11 tracking-wider text-text-tertiary">
                ORDER BY
              </span>
              <span className="min-w-0 truncate text-code-12 text-text-primary">
                {orderBy ? orderBy.column : ""}
              </span>
              {/* The direction arrow stays — after the column name, where it
                  qualifies something rather than labelling the control. */}
              {orderBy ? (
                orderBy.direction === "asc" ? (
                  <ArrowUpAZ className="shrink-0 text-text-tertiary" />
                ) : (
                  <ArrowDownAZ className="shrink-0 text-text-tertiary" />
                )
              ) : null}
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
