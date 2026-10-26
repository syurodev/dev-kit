import { useQuery } from "@tanstack/react-query";
import {
  toolUsageReport,
  accountMoveUsageReport,
} from "@/bridge/agentUsageMetrics";
import { validWindow } from "./measurementModel";
export const measurementOptions = (
  vendor: string,
  from: string,
  to: string,
  cursor: string,
) => ({
  tools: {
    queryKey: ["agenthub", "usage", "tools", vendor, from, to],
    queryFn: () => toolUsageReport(vendor, { from, to, limit: 100 }),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    enabled: validWindow(from, to),
  },
  moves: {
    queryKey: ["agenthub", "usage", "moves", vendor, from, to, cursor],
    queryFn: () =>
      accountMoveUsageReport(vendor, { from, to, limit: 100, cursor }),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    enabled: validWindow(from, to),
  },
});
export function useUsageMeasurements(
  vendor: string,
  from: string,
  to: string,
  cursor: string,
) {
  const options = measurementOptions(vendor, from, to, cursor);
  return { tools: useQuery(options.tools), moves: useQuery(options.moves) };
}
