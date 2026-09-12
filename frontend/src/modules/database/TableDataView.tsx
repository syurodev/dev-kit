import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { listColumns, listIndexes } from "@/bridge/database";
import { ResultGrid } from "./grid/ResultGrid";
import { TableToolbar } from "./components/TableToolbar";
import { BrowsePager } from "./components/BrowsePager";
import { DatabaseOverlays } from "./DatabaseOverlays";
import { useDatabaseStore } from "./databaseStore";
import { useDatabaseChrome } from "./useDatabaseChrome";
import { browseKeyOfTab, useBrowseLifetime } from "./useConsoleLifetime";
import { useIsActiveDocument } from "./useIsActiveDocument";
import { openConsoleTab } from "./documents";
import { buildBrowseQuery } from "./sql/browseQuery";
import { quoteIdentifier, quoteLiteral } from "./sql/identifier";
import { buildUpdate, updateRefusal } from "./sql/updateCell";
import { errorMessage, isCancelError } from "@/bridge/errors";
import { tableExportFilename } from "./export";
import { pageKeyFor } from "./tableKey";

/**
 * One table's data, as its own document.
 *
 * No SQL editor. Opening a table is a request to see rows, and the statement
 * behind them is composed from the filter, the sort and the page — the three
 * things a person browsing actually changes. "Open in console" hands the
 * composed statement to a console tab for anything past that.
 */
export function TableDataView({
  params,
}: {
  params?: Record<string, unknown>;
}) {
  const schema = typeof params?.schema === "string" ? params.schema : "";
  const table = typeof params?.table === "string" ? params.table : "";
  // A22: through the shared rule, not spelled again here — useBrowseLifetime
  // computes the same key from a TAB, and two copies would drift into exactly
  // the leak that comment warns about.
  const key = browseKeyOfTab("database.table", { schema, table }) ?? "";

  // The catalogue read below already returns every column's type — it is read
  // for the key. Kept rather than discarded: it is what the header glyphs are
  // drawn from, and asking for it again would be a second round trip for bytes
  // already in hand.
  const [columnTypes, setColumnTypes] = useState<Record<string, string>>({});

  useDatabaseChrome();
  useBrowseLifetime(key);
  const isActive = useIsActiveDocument("database.table", { schema, table });

  const {
    selectedProfileId,
    profiles,
    browse,
    pageKey,
    keyResolved,
    setBrowseWhere,
    setBrowseOrder,
    setBrowseWidths,
    setBrowseOrderColumns,
    setBrowseFrozen,
    setPageKey,
    loadBrowsePage,
    applyBrowseEdit,
    goToBrowsePage,
    countBrowseRows,
    lastPage,
    schemaNamespace,
    pageSize,
    setBrowsePageSize,
    setInspectedCell,
    setSql,
    openExportModal,
    openDDL,
  } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      profiles: s.profiles,
      browse: s.browse[key] ?? s.browseState(key),
      pageKey: s.pageKeys[key] ?? null,
      // A15: whether the index read has ANSWERED, which `pageKey` cannot say —
      // a null means both "no key" and "not asked yet", and loading the first
      // page during the second one leaves the cursor trail with no page 0 entry.
      keyResolved: key in s.pageKeys,
      setBrowseWhere: s.setBrowseWhere,
      setBrowseOrder: s.setBrowseOrder,
      setBrowseWidths: s.setBrowseWidths,
      setBrowseOrderColumns: s.setBrowseOrderColumns,
      setBrowseFrozen: s.setBrowseFrozen,
      setPageKey: s.setPageKey,
      loadBrowsePage: s.loadBrowsePage,
      applyBrowseEdit: s.applyBrowseEdit,
      goToBrowsePage: s.goToBrowsePage,
      countBrowseRows: s.countBrowseRows,
      lastPage: s.lastBrowsePage(key),
      schemaNamespace: s.schemaSnapshot?.namespace,
      pageSize: s.browsePageSize,
      setBrowsePageSize: s.setBrowsePageSize,
      setInspectedCell: s.setInspectedCell,
      setSql: s.setSql,
      openExportModal: s.openExportModal,
      openDDL: s.openDDL,
    })),
  );

  // The key decides whether this table can be walked or has to be counted
  // through, so it is read before the first page.
  useEffect(() => {
    if (!selectedProfileId) {
      // Nothing to ask yet, and nothing loads either: the page effect below is
      // gated on the same profile, so the key stays unresolved only while the
      // table could not have been read anyway, and selecting one re-runs this.
      return;
    }
    // A25: an identifier we do not have resolves the key as null rather than
    // not at all. The page below opens on `key in pageKeys`, so returning
    // early here left such a table blank forever — no rows, no spinner, no
    // error, which is the worst of the outcomes available. Null is "unkeyed",
    // the normal path runs, and buildBrowseQuery refuses the empty identifier
    // inside loadBrowsePage's try, so the document says so in the error banner
    // it already renders.
    if (!schema || !table) {
      setPageKey(key, null);
      return;
    }
    let cancelled = false;
    // A24: the columns come with the indexes, because a unique index is only a
    // key when none of its columns is nullable, and nothing else here knows
    // that — the sidebar's cache is only populated for a table someone expanded.
    void Promise.all([
      listIndexes(selectedProfileId, schema),
      listColumns(selectedProfileId, schema, table),
    ])
      .then(([indexes, columns]) => {
        if (!cancelled) {
          setPageKey(key, pageKeyFor(indexes, columns, schema, table));
          setColumnTypes(Object.fromEntries(columns.map((c) => [c.name, c.dataType])));
        }
      })
      .catch(() => {
        // A catalogue read we could not complete is not a reason to refuse the
        // table: unkeyed is the safe reading, and it pages by offset. The
        // header simply draws the neutral glyph, which is what an unknown type
        // means anyway.
        if (!cancelled) {
          setPageKey(key, null);
          setColumnTypes({});
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selectedProfileId, schema, table, key, setPageKey]);

  const hasRows = browse.rows !== null;
  useEffect(() => {
    // A25: no `table` check. It was the same silent refusal the key resolution
    // above had, one effect further on — a table document missing an
    // identifier would sit blank rather than load and fail. The load is
    // allowed to run and to report what is wrong with it.
    if (!hasRows && keyResolved && selectedProfileId) {
      void loadBrowsePage(key, { schema, table });
    }
  }, [
    hasRows,
    keyResolved,
    selectedProfileId,
    key,
    schema,
    table,
    loadBrowsePage,
  ]);

  // ponytail: one COUNT(*) per table tab and per filter, beside the first page
  // rather than inside it — the rows are what the tab is for and they are not
  // held up waiting for a number. It is the price of naming a last page at
  // all; if it is too slow on a huge table, move it behind the Last button.
  const where = browse.where;
  useEffect(() => {
    if (selectedProfileId && schema && table) {
      void countBrowseRows(key, { schema, table });
    }
  }, [selectedProfileId, schema, table, key, where, countBrowseRows]);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const engine = profile?.engine;
  // Asked once, here, rather than at each cell: the answer is a property of
  // the connection and the table, not of which cell was double-clicked.
  const editRefusal = updateRefusal({
    keyColumns: pageKey,
    readOnly: Boolean(profile?.readOnly),
    schema,
    table,
  });

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <TableToolbar
        where={browse.where}
        onWhereChange={(value) => setBrowseWhere(key, value)}
        onApply={() => void loadBrowsePage(key, { schema, table })}
        engine={engine}
        indexed={schemaNamespace}
        schema={schema}
        table={table}
        orderBy={browse.orderBy}
        columns={browse.rows?.columns ?? EMPTY_ARRAY}
        onOrderByChange={(next) => {
          setBrowseOrder(key, next);
          void loadBrowsePage(key, { schema, table });
        }}
        rowCount={browse.rows?.rows.length ?? 0}
        loading={browse.loading}
        onRefresh={() => void loadBrowsePage(key, { schema, table })}
        pager={
          browse.rows ? (
            <BrowsePager
              pageIndex={browse.pageIndex}
              pageSize={pageSize}
              rowCount={browse.rows.rows.length}
              totalRows={browse.totalRows}
              lastPage={lastPage}
              hasNext={browse.hasNext}
              keyed={pageKey !== null}
              loading={browse.loading}
              onGoToPage={(page) =>
                void goToBrowsePage(key, { schema, table }, page)
              }
              onPageSizeChange={(size) => {
                setBrowsePageSize(size);
                void loadBrowsePage(key, { schema, table });
              }}
            />
          ) : null
        }
        // A28: the page already in hand, not a query re-run to fetch it again.
        // The toolbar disables this with no rows, and the guard here is what
        // makes that a courtesy rather than the only thing holding it up.
        onExport={() => {
          if (browse.rows) {
            openExportModal(browse.rows, tableExportFilename(table));
          }
        }}
        onShowDDL={() => openDDL(schema, table)}
        onOpenInConsole={() => {
          const consoleId = `console-${key}`;
          setSql(
            consoleId,
            buildBrowseQuery({
              schema,
              table,
              engine,
              where: browse.where,
              orderBy: browse.orderBy,
              pageKey,
              cursor: null,
              pageSize,
            }).sql,
          );
          openConsoleTab(consoleId, table);
        }}
      />

      {browse.error ? (
        <div className="mb-2 shrink-0 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) p-3 text-body-12 text-status-danger">
          {browse.error}
        </div>
      ) : null}

      {browse.rows ? (
        <ResultGrid
          result={browse.rows}
          widths={browse.widths}
          columnOrder={browse.order}
          onResize={(next) => setBrowseWidths(key, next)}
          onReorder={(next) => setBrowseOrderColumns(key, next)}
          frozen={browse.frozen}
          onFreeze={(next) => {
            // Pinning moves the column as well as pinning it, so both land
            // together — the order first, or the grid paints one frame with
            // the new run length over the old arrangement.
            setBrowseOrderColumns(key, next.order);
            setBrowseFrozen(key, next.frozen);
          }}
          onInspectCell={setInspectedCell}
          columnTypes={columnTypes}
          keyColumns={pageKey ?? undefined}
          // Rows are numbered through the table, not through the page.
          rowOffset={browse.pageIndex * pageSize}
          // The grid hands up a column and a value; the engine's quoting rules
          // live here, with the rest of the SQL this tab composes.
          onFilterByValue={({ column, value, negate }) => {
            const lhs = quoteIdentifier(column, engine);
            // An empty cell is NULL, and NULL never equals anything — `= ''`
            // would match the empty string instead and quietly return nothing.
            const clause =
              value === ""
                ? `${lhs} IS ${negate ? "NOT " : ""}NULL`
                : `${lhs} ${negate ? "<>" : "="} ${quoteLiteral(value, engine)}`;
            const next = browse.where.trim() ? `${browse.where.trim()} AND ${clause}` : clause;
            setBrowseWhere(key, next);
            void loadBrowsePage(key, { schema, table });
          }}
          // Editing is the table tab's alone: a console result can be a join
          // or an expression, and there is no single table behind it to write
          // to. Even here it is refused without a key — see updateRefusal.
          editRefusal={editRefusal}
          onEditCells={({ rows: rowIndices, columns: columnIndices, value }) => {
            if (editRefusal || !browse.rows || !pageKey) {
              return;
            }
            const columns = browse.rows.columns;
            const sql = buildUpdate({
              schema,
              table,
              engine,
              keyColumns: pageKey,
              // The row's OWN key values, read out of the page in hand. The
              // grid names cells by position; only this side knows which
              // columns identify a row.
              rows: rowIndices.map((rowIndex) => ({
                values: pageKey.map((key) => {
                  const at = columns.indexOf(key);
                  const cell = at === -1 ? "" : (browse.rows!.rows[rowIndex][at] ?? "");
                  return cell === "" ? null : cell;
                }),
              })),
              columns: columnIndices.map((index) => columns[index]),
              value,
            });
            const cells = rowIndices.length * columnIndices.length;
            applyBrowseEdit(key, { schema, table }, sql)
              .then(() => toast.success(cells === 1 ? "Cell updated" : `${cells} cells updated`))
              .catch((error: unknown) => {
                if (isCancelError(error)) return;
                // A toast, not the tab's error banner: the banner is about the
                // rows on screen, and they are still exactly what the server
                // last sent.
                toast.error("Could not apply the edit", { description: errorMessage(error) });
              });
          }}
          sort={
            browse.orderBy
              ? {
                  column: browse.rows.columns.indexOf(browse.orderBy.column),
                  direction: browse.orderBy.direction,
                }
              : null
          }
          // Three states, not two: asc, desc, and back to the table's own
          // order. A two-state toggle is a sort you cannot take off — once
          // any header is clicked the table is sorted by something forever,
          // and "no ORDER BY" is a real answer the grid could no longer give.
          onSort={(index) => {
            const column = browse.rows!.columns[index];
            const current =
              browse.orderBy?.column === column
                ? browse.orderBy.direction
                : null;
            const next =
              current === null ? "asc" : current === "asc" ? "desc" : null;
            setBrowseOrder(
              key,
              next === null ? null : { column, direction: next },
            );
            void loadBrowsePage(key, { schema, table });
          }}
        />
      ) : null}

      {isActive ? <DatabaseOverlays /> : null}
    </div>
  );
}

export default TableDataView;
