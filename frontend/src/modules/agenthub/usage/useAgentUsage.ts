import { useQuery } from "@tanstack/react-query";
import { agentUsageReport } from "@/bridge/agentUsage";
import { listWorkspaces } from "@/bridge/workspace";

/** Every vendor's usage report; one entry per vendor under it. */
export const AGENT_USAGE_KEY = ["agenthub", "usage"] as const;
export const agentUsageKey = (vendor: string) => [...AGENT_USAGE_KEY, vendor] as const;

/** What a vendor's turns spent, in tokens. Read when the page opens and again when the window regains focus. */
export const agentUsageOptions = (vendor: string) => ({
  queryKey: agentUsageKey(vendor),
  queryFn: () => agentUsageReport(vendor),
  staleTime: 30_000,
  // The shared QueryClient disables this; staleTime alone does not schedule reads.
  refetchOnWindowFocus: true,
});

export function useAgentUsage(vendor: string) {
  return useQuery(agentUsageOptions(vendor));
}

/** Workspace id → name. The report carries ids only; the window names them. */
export function useWorkspaceNames() {
  return useQuery({
    queryKey: ["workspaces", "names"],
    queryFn: async () => Object.fromEntries((await listWorkspaces()).map((workspace) => [workspace.id, workspace.name])),
    staleTime: 60_000,
  });
}
