import { errorMessage } from "@/bridge/errors";
import { vendorInfo } from "../vendors";
import { UsageReportTable } from "./UsageReportTable";
import { useAgentUsage, useWorkspaceNames } from "./useAgentUsage";

/**
 * Settings → Agents → Accounts & limits, token usage for one vendor. Reads through
 * the UI-only bridge method; the table below only draws. Keyed by vendor, so a
 * Claude | Codex switch never carries one vendor's report over to the other.
 */
export function AgentUsageSetting({ vendor }: { vendor: string }) {
  return <VendorUsage key={vendor} vendor={vendor} />;
}

function VendorUsage({ vendor }: { vendor: string }) {
  const usage = useAgentUsage(vendor);
  const names = useWorkspaceNames();
  return (
    <UsageReportTable
      vendorName={vendorInfo(vendor).name}
      report={usage.data}
      loadError={usage.error ? errorMessage(usage.error) : ""}
      workspaceNames={names.data ?? {}}
    />
  );
}
