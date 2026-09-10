import { useEffect } from "react";
import { useDatabaseStore } from "./databaseStore";
import { QueryConsole } from "./components/QueryConsole";
import { DatabaseOverlays } from "./DatabaseOverlays";
import { useIsActiveDocument } from "./useIsActiveDocument";

/**
 * One table's data, as its own document.
 *
 * Backed by a console rather than a second grid: the backend's only read path
 * is RunQuery, the result shape is the same, and a table tab is a console whose
 * SQL was written for you. Giving it its own console id is what keeps that SQL
 * — and its result — from colliding with a console you typed in yourself.
 */
export function TableDataView({ params }: { params?: Record<string, unknown> }) {
  const schema = typeof params?.schema === "string" ? params.schema : "";
  const table = typeof params?.table === "string" ? params.table : "";
  const consoleId = `table:${schema}.${table}`;

  const setSql = useDatabaseStore((s) => s.setSql);
  const executeQuery = useDatabaseStore((s) => s.executeQuery);
  const hasSql = useDatabaseStore((s) => Boolean(s.consoles[consoleId]?.sql));
  const isActive = useIsActiveDocument("database.table", { schema, table });

  // Seeded once. Re-seeding on every mount would throw away a WHERE the user
  // added, and this view remounts whenever its tab leaves the mount window.
  useEffect(() => {
    if (hasSql || !table) {
      return;
    }
    setSql(consoleId, `SELECT * FROM ${table} LIMIT 200;`);
    void executeQuery(consoleId);
  }, [consoleId, hasSql, table, setSql, executeQuery]);

  return (
    <>
      <QueryConsole consoleId={consoleId} />
      {isActive ? <DatabaseOverlays /> : null}
    </>
  );
}

export default TableDataView;
