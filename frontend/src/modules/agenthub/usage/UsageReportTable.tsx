import {
  DataTable,
  type DataTableColumn,
} from "@/components/ui/data-table/DataTable";
import { Skeleton } from "@/components/ui/skeleton";
import type { UsageReport } from "@/bridge/agentUsage";
import {
  QUOTA_NOTE,
  cacheReadShare,
  completeTotal,
  dataNotes,
  formatMetric,
  recentDays,
  rowsOf,
  sessionRowsOf,
  workspaceName,
  type UsageRow,
} from "./usageModel";

import type { AgentAccount } from "@/bridge/agentAccounts";
import { usageAccountName } from "./measurementModel";

const DAYS_SHOWN = 14;

export type UsageReportTableProps = {
  vendorName: string;
  accounts?: AgentAccount[];
  accountsError?: boolean;
  /** Undefined while the first read is on its way. */
  report: UsageReport | undefined;
  loadError: string;
  /** Workspace id → name, from the workspace list the window already has. */
  workspaceNames: Record<string, string>;
};

/**
 * Settings → Agents → Accounts & limits, token usage for one vendor. It only draws:
 * the page reads the report and the workspace names and passes them in. It shows
 * tokens and says they are not a share of the subscription limit.
 */
export function UsageReportTable({
  vendorName,
  report,
  loadError,
  workspaceNames,
  accounts,
  accountsError,
}: UsageReportTableProps) {
  const noData =
    report !== undefined &&
    report.totals.turns === report.totals.turnsWithoutData;
  return (
    <section
      className="flex flex-col gap-3"
      aria-label={`${vendorName} token usage`}
    >
      <h3 className="text-body-14-medium text-text-primary">Token usage</h3>
      <p className="text-body-12 text-text-tertiary">{QUOTA_NOTE}</p>
      {report === undefined && !loadError ? (
        <Skeleton className="h-24 w-full rounded-control" />
      ) : null}
      {loadError ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {loadError}
        </p>
      ) : null}
      {report !== undefined && noData ? (
        <p className="rounded-control border border-dashed border-border-subtle px-3.5 py-3 text-body-13 text-text-secondary">
          No token data yet for {vendorName}.
        </p>
      ) : null}
      {report !== undefined && !noData ? (
        <ReportBody
          report={report}
          workspaceNames={workspaceNames}
          accounts={accounts}
          accountsError={accountsError}
        />
      ) : null}
    </section>
  );
}

function ReportBody({
  report,
  workspaceNames,
  accounts,
  accountsError,
}: {
  report: UsageReport;
  workspaceNames: Record<string, string>;
  accounts?: AgentAccount[];
  accountsError?: boolean;
}) {
  const grand = completeTotal(report.totals);
  const named = (key: string) => workspaceName(key, workspaceNames);
  return (
    <div className="flex flex-col gap-4">
      <p className="text-body-13 text-text-primary">
        {formatMetric(report.totals, "total")} tokens over {report.totals.turns}{" "}
        turns · {cacheReadShare(report.totals)} cache reads
      </p>
      {dataNotes(report).map((note) => (
        <p key={note} className="text-body-12 text-text-tertiary">
          {note}
        </p>
      ))}
      <UsageTable
        title="By account"
        nameHeader="Account"
        rows={rowsOf(
          report.byAccount.map((a) => ({
            key: JSON.stringify([a.accountId, a.generation]),
            totals: a.totals,
          })),
          (key) => {
            const [id, gen] = JSON.parse(key) as [string, string];
            return usageAccountName(id, gen, accounts, accountsError);
          },
          grand,
        )}
      />
      <UsageTable
        title="By workspace"
        nameHeader="Workspace"
        rows={rowsOf(report.byWorkspace, named, grand)}
      />
      <UsageTable
        title={`By day (last ${DAYS_SHOWN}, UTC)`}
        nameHeader="Day"
        rows={rowsOf(recentDays(report.byDay, DAYS_SHOWN), (key) => key, grand)}
      />
      <UsageTable
        title="By model"
        nameHeader="Model"
        rows={rowsOf(report.byModel, (key) => key, grand)}
      />
      <UsageTable
        title="Heaviest sessions"
        nameHeader="Session"
        rows={sessionRowsOf(report.topSessions, workspaceNames, grand)}
      />
    </div>
  );
}

const NUMBER_COLUMNS = [
  "Turns",
  "Total",
  "Fresh input",
  "Cache read",
  "Cache write",
  "Output",
  "Share",
];

function UsageTable({
  title,
  nameHeader,
  rows,
}: {
  title: string;
  nameHeader: string;
  rows: UsageRow[];
}) {
  if (rows.length === 0) return null;
  const columns: DataTableColumn[] = [
    { key: "name", header: nameHeader, nowrap: true },
    ...NUMBER_COLUMNS.map((header) => ({
      key: header,
      header,
      align: "right" as const,
    })),
  ];
  return (
    <div className="flex flex-col gap-1.5">
      <h4 className="text-body-13-medium text-text-secondary">{title}</h4>
      <DataTable
        dense
        fill
        caption={title}
        initialWidths={[280, 80, 120, 130, 130, 130, 110, 100]}
        columns={columns}
        rows={rows.map((row) => [
          row.name,
          String(row.turns),
          row.total,
          row.freshInput,
          row.cacheRead,
          row.cacheWrite,
          row.output,
          row.share,
        ])}
      />
    </div>
  );
}
