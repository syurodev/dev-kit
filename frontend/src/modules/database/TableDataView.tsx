import { useEffect } from "react";
import { useShallow } from "zustand/react/shallow";
import { listColumns, listIndexes } from "@/bridge/database";
import { ResultGrid } from "./grid/ResultGrid";
import { resizeColumn } from "./grid/gridColumns";
import { TableToolbar } from "./components/TableToolbar";
import { DatabaseOverlays } from "./DatabaseOverlays";
import { BROWSE_PAGE_SIZE, useDatabaseStore } from "./databaseStore";
import { useDatabaseChrome } from "./useDatabaseChrome";
import { browseKeyOfTab, useBrowseLifetime } from "./useConsoleLifetime";
import { useIsActiveDocument } from "./useIsActiveDocument";
import { openConsoleTab } from "./documents";
import { buildBrowseQuery } from "./sql/browseQuery";
import { pageKeyFor } from "./tableKey";

/**
 * One table's data, as its own document.
 *
 * No SQL editor. Opening a table is a request to see rows, and the statement
 * behind them is composed from the filter, the sort and the page — the three
 * things a person browsing actually changes. "Open in console" hands the
 * composed statement to a console tab for anything past that.
 */
export function TableDataView({ params }: { params?: Record<string, unknown> }) {
  const schema = typeof params?.schema === "string" ? params.schema : "";
  const table = typeof params?.table === "string" ? params.table : "";
  // A22: through the shared rule, not spelled again here — useBrowseLifetime
  // computes the same key from a TAB, and two copies would drift into exactly
  // the leak that comment warns about.
  const key = browseKeyOfTab("database.table", { schema, table }) ?? "";

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
    setPageKey,
    loadBrowsePage,
    stepBrowsePage,
    setInspectedCell,
    setSql,
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
      setPageKey: s.setPageKey,
      loadBrowsePage: s.loadBrowsePage,
      stepBrowsePage: s.stepBrowsePage,
      setInspectedCell: s.setInspectedCell,
      setSql: s.setSql,
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
        }
      })
      .catch(() => {
        // A catalogue read we could not complete is not a reason to refuse the
        // table: unkeyed is the safe reading, and it pages by offset.
        if (!cancelled) {
          setPageKey(key, null);
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
  }, [hasRows, keyResolved, selectedProfileId, key, schema, table, loadBrowsePage]);

  const engine = profiles.find((p) => p.id === selectedProfileId)?.engine;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <TableToolbar
        where={browse.where}
        onWhereChange={(value) => setBrowseWhere(key, value)}
        onApply={() => void loadBrowsePage(key, { schema, table })}
        orderLabel={
          browse.orderBy
            ? `ORDER BY ${browse.orderBy.column} ${browse.orderBy.direction.toUpperCase()}`
            : "ORDER BY —"
        }
        pageIndex={browse.pageIndex}
        pageSize={BROWSE_PAGE_SIZE}
        rowCount={browse.rows?.rows.length ?? 0}
        hasNext={browse.hasNext}
        keyed={pageKey !== null}
        loading={browse.loading}
        onStep={(step) => void stepBrowsePage(key, { schema, table }, step)}
        onRefresh={() => void loadBrowsePage(key, { schema, table })}
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
              pageSize: BROWSE_PAGE_SIZE,
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
          onResize={(index, delta) => setBrowseWidths(key, resizeColumn(browse.widths, index, delta))}
          onReorder={(next) => setBrowseOrderColumns(key, next)}
          onInspectCell={setInspectedCell}
          sort={
            browse.orderBy
              ? {
                  column: browse.rows.columns.indexOf(browse.orderBy.column),
                  direction: browse.orderBy.direction,
                }
              : null
          }
          onSort={(index) => {
            const column = browse.rows!.columns[index];
            const next =
              browse.orderBy?.column === column && browse.orderBy.direction === "asc"
                ? ("desc" as const)
                : ("asc" as const);
            setBrowseOrder(key, { column, direction: next });
            void loadBrowsePage(key, { schema, table });
          }}
        />
      ) : null}

      {isActive ? <DatabaseOverlays /> : null}
    </div>
  );
}

export default TableDataView;
