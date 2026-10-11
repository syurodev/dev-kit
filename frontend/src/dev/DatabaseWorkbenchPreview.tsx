import { useEffect, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { QueryConsole } from "@/modules/database/components/QueryConsole";
import { DatabaseDocumentLayout } from "@/modules/database/components/DatabaseDocumentLayout";
import {
  EMPTY_CONSOLE,
  useDatabaseStore,
} from "@/modules/database/databaseStore";
import { DatabaseSidebar } from "@/modules/database/components/DatabaseSidebar";

const client = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: Infinity } },
});
const readTransactionState = async () => ({ manual: false, open: false });
const sql = `SELECT e.code, t.work_date, t.day_type, t.shift_id,\n       s.start_time AS ca_bat_dau, s.end_time AS ca_ket_thuc\nFROM his_employees e\nJOIN his_timesheet_days t ON t.employee_id = e.id\nLEFT JOIN his_shifts s ON s.id = t.shift_id\nWHERE e.code = 'NV000015';`;
const result = {
  columns: [
    "code",
    "work_date",
    "day_type",
    "shift_id",
    "ca_bat_dau",
    "ca_ket_thuc",
  ],
  rows: [
    ["NV000015", "2026-10-09", "1", "2", "09:00", "18:00"],
    ["NV000016", "2026-10-09", "1", "2", "09:00", null],
  ],
  rowCount: 2,
  truncated: false,
};
/** Real console/grid and explorer; all sample rows remain local. */
export default function DatabaseWorkbenchPreview() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const profile = {
      id: "preview-prod",
      name: "Chiro Prod",
      host: "example.test",
      port: 5432,
      database: "chiro",
      username: "reader",
      sslMode: "require",
      env: "prod",
      readOnly: true,
      engine: "postgres",
    };
    const dev = {
      ...profile,
      id: "preview-dev",
      name: "Chiro Dev",
      env: "dev",
      readOnly: false,
    };
    const snapshot = {
      namespace: {
        public: {
          his_employees: ["code", "name"],
          his_timesheet_days: ["employee_id", "work_date"],
        },
      },
      columns: {},
      indexedAt: "",
    };
    for (const p of [profile, dev])
      client.setQueryData(["database", "console-schema", p.id], snapshot);
    useDatabaseStore.setState({
      profiles: [profile, dev],
      selectedProfileId: profile.id,
      schemaSnapshot: snapshot,
      serverDatabases: {
        [profile.id]: ["chiro", "analytics"],
        [dev.id]: ["chiro"],
      },
      expandedProfiles: { [profile.id]: true },
      discoverDatabases: async () => {},
      setSelectedProfileId: (id) =>
        useDatabaseStore.setState({ selectedProfileId: id }),
      consoles: {
        preview: {
          ...EMPTY_CONSOLE,
          targetProfileId: profile.id,
          sql,
          results: [{ statement: sql, result }],
          queryDurationMs: 148,
          queryStatus: "2 rows",
          lastStatement: sql,
        },
      },
      executeQuery: async () => "skipped",
      runScript: async () => {},
      copyFormat: "json",
    });
    setReady(true);
    return () => useDatabaseStore.getState().setInspectedCell(null);
  }, []);
  if (!ready) return null;
  return (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <main className="flex h-svh min-w-0 flex-col bg-surface-base text-text-primary">
          <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border-subtle px-3 text-body-12">
            <span className="mr-auto">Database workbench · local preview</span>
            <Button
              size="xs"
              variant="ghost"
              onClick={() =>
                useDatabaseStore.setState((s) => ({
                  consoles: {
                    ...s.consoles,
                    preview: {
                      ...s.consoles.preview,
                      queryError:
                        "ERROR: relation does not exist\nCheck the table name and schema.",
                    },
                  },
                }))
              }
            >
              Show error
            </Button>
            <Button
              size="xs"
              variant="ghost"
              onClick={() =>
                useDatabaseStore.setState((s) => ({
                  consoles: {
                    ...s.consoles,
                    preview: {
                      ...s.consoles.preview,
                      queryError: "",
                      results: [{ statement: sql, result }],
                      queryStatus: "2 rows",
                      queryDurationMs: 148,
                    },
                  },
                }))
              }
            >
              Restore result
            </Button>
          </div>
          <div className="flex min-h-0 flex-1">
            <div className="hidden w-60 shrink-0 border-r border-border-subtle p-2 md:block">
              <DatabaseSidebar loadData={false} />
            </div>
            <div className="min-w-0 flex-1">
              <DatabaseDocumentLayout active>
                <QueryConsole
                  consoleId="preview"
                  readTransactionState={readTransactionState}
                />
              </DatabaseDocumentLayout>
            </div>
          </div>
        </main>
      </TooltipProvider>
    </QueryClientProvider>
  );
}
