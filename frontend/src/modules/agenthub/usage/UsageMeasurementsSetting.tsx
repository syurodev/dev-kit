import { useState } from "react";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DataTable,
  type DataTableColumn,
} from "@/components/ui/data-table/DataTable";
import {
  initialWindow,
  validWindow,
  formatBytes,
  moveStatus,
  usageAccountName,
} from "./measurementModel";
import { useUsageMeasurements } from "./useUsageMeasurements";
const columns = (names: string[]): DataTableColumn[] =>
  names.map((header, i) => ({
    key: header,
    header,
    nowrap: true,
    align: i ? "right" : "left",
  }));
export function UsageMeasurementsSetting({
  vendor,
  accounts,
  accountsError,
}: {
  vendor: string;
  accounts?: AgentAccount[];
  accountsError: boolean;
}) {
  const [window, setWindow] = useState(initialWindow);
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  const { tools, moves } = useUsageMeasurements(
    vendor,
    window.from,
    window.to,
    cursor,
  );
  const change = (field: "from" | "to", value: string) => {
    setWindow((w) => ({ ...w, [field]: value }));
    setCursors([""]);
  };
  const valid = validWindow(window.from, window.to);
  return (
    <section
      className="mt-5 flex flex-col gap-3"
      aria-label="Host usage measurements"
    >
      <h3 className="text-body-14-medium text-text-primary">
        Host usage measurements
      </h3>
      <div className="flex flex-wrap items-center gap-2">
        <label className="text-body-12">
          From (UTC)
          <Input
            type="date"
            value={window.from}
            onChange={(e) => change("from", e.target.value)}
          />
        </label>
        <label className="text-body-12">
          To (UTC)
          <Input
            type="date"
            value={window.to}
            onChange={(e) => change("to", e.target.value)}
          />
        </label>
      </div>
      {!valid ? (
        <p role="alert" className="text-body-12 text-status-danger">
          Choose a valid range of at most 90 UTC days.
        </p>
      ) : (
        <>
          <h4 className="text-body-13-medium">MCP tool result text</h4>
          <p className="text-body-12 text-text-tertiary">
            UTF-8 text bytes before and after the host result budget. Excludes
            image/resource bytes, arguments and duplicate structured content.
            These are not tokens or subscription savings. Hub sessions only;
            collection starts with this version.
          </p>
          {tools.error ? (
            <p role="alert">{errorMessage(tools.error)}</p>
          ) : tools.data ? (
            <>
              <p className="text-body-12 text-text-tertiary">
                Collector started: {tools.data.health.startedAt || "—"} · Saved
                through: {tools.data.health.savedThrough || "not yet saved"}.
                Global collector health: {tools.data.health.dropped} dropped
                observations, {tools.data.health.writeFailures} write failures,{" "}
                {tools.data.health.uncleanRuns} unclean runs. A process crash
                may lose the unflushed tail.
              </p>
              {!tools.data.rows.length ? (
                <p className="text-body-12">
                  No saved tool measurements in this window.
                </p>
              ) : (
                <DataTable
                  dense
                  fill
                  caption="MCP tool result text"
                  initialWidths={[
                    240, 80, 90, 150, 120, 120, 140, 160, 210, 90, 180,
                  ]}
                  columns={columns([
                    "Tool",
                    "Calls",
                    "Results",
                    "Text / non-text",
                    "Before",
                    "After",
                    "Byte reduction",
                    "Cut rate",
                    "Max before / after",
                    "Exempt",
                    "Tool / handler errors",
                  ])}
                  rows={tools.data.rows.map(({ tool, stats: s }) => [
                    tool,
                    s.calls,
                    s.results,
                    `${s.textCalls} / ${s.nonTextCalls}`,
                    s.results ? formatBytes(s.beforeBytes) : "—",
                    s.results ? formatBytes(s.afterBytes) : "—",
                    s.results ? formatBytes(s.beforeBytes - s.afterBytes) : "—",
                    s.results
                      ? `${((100 * s.cuts) / s.results).toFixed(1)}% (${s.cuts}/${s.results})`
                      : "—",
                    s.results
                      ? `${formatBytes(s.maxBefore)} / ${formatBytes(s.maxAfter)}`
                      : "—",
                    s.exemptCalls,
                    `${s.toolErrors} / ${s.handlerErrors}`,
                  ])}
                />
              )}
              {tools.data.hasMore ? (
                <p className="text-body-12">
                  Showing the 100 largest tools by text bytes.
                </p>
              ) : null}
            </>
          ) : (
            <p className="text-body-12">Loading tool measurements…</p>
          )}
          <h4 className="mt-3 text-body-13-medium">Account moves</h4>
          <p className="text-body-12 text-text-tertiary">
            Cache write on the first recorded turn after a verified runtime
            move. Correlation does not establish that the move caused a cache
            miss. Missing accounting stays unknown; legacy notices cannot verify
            a reopened runtime.
          </p>
          {moves.error ? (
            <p role="alert">{errorMessage(moves.error)}</p>
          ) : moves.data ? (
            <>
              {!moves.data.rows.length ? (
                <p className="text-body-12">No account moves in this window.</p>
              ) : (
                <DataTable
                  dense
                  fill
                  caption="Account moves"
                  initialWidths={[190, 180, 160, 300, 240, 190, 110]}
                  columns={columns([
                    "Time (UTC)",
                    "Session",
                    "From ID",
                    "To",
                    "Accounting",
                    "First turn (UTC)",
                    "Cache write",
                  ])}
                  rows={moves.data.rows.map((m) => [
                    m.at,
                    m.sessionId,
                    m.from || "Unknown",
                    usageAccountName(
                      m.to,
                      m.toGeneration,
                      accounts,
                      accountsError,
                    ),
                    moveStatus[m.status] ?? "Unverified",
                    m.turnStartedAt || "—",
                    m.cacheWrite == null ? "—" : m.cacheWrite.toLocaleString(),
                  ])}
                />
              )}
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={cursors.length === 1}
                  onClick={() => setCursors((c) => c.slice(0, -1))}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!moves.data.nextCursor}
                  onClick={() =>
                    setCursors((c) => [...c, moves.data.nextCursor])
                  }
                >
                  Next
                </Button>
              </div>
            </>
          ) : (
            <p className="text-body-12">Loading account moves…</p>
          )}
        </>
      )}
    </section>
  );
}
