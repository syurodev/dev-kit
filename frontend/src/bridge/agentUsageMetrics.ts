import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  Report,
  Query,
} from "../../bindings/devkit/internal/usagemetrics/models";
import type {
  AccountMoveReport,
  AccountMoveQuery,
} from "../../bindings/devkit/internal/agenthub/models";
import { callBridge } from "./client";
export type ToolReport = Omit<Report, "rows"> & {
  rows: NonNullable<Report["rows"]>;
};
export type MoveReport = Omit<AccountMoveReport, "rows"> & {
  rows: NonNullable<AccountMoveReport["rows"]>;
};
export const toolUsageReport = (
  vendor: string,
  query: Query,
): Promise<ToolReport> =>
  callBridge(async () => {
    const raw = await AppService.AgentToolUsageReport(vendor, query);
    return { ...raw, rows: raw.rows ?? [] };
  });
export const accountMoveUsageReport = (
  vendor: string,
  query: AccountMoveQuery,
): Promise<MoveReport> =>
  callBridge(async () => {
    const raw = await AppService.AgentAccountMoveUsageReport(vendor, query);
    return { ...raw, rows: raw.rows ?? [] };
  });
