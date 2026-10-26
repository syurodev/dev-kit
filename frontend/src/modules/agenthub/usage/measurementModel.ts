import type { AgentAccount } from "@/bridge/agentAccounts";

/** A reused slug must never inherit the old account's display name. */
export function usageAccountName(
  id: string,
  generation: string,
  accounts: AgentAccount[] | undefined,
  failed = false,
): string {
  if (!id || !generation) return "Unknown account (legacy)";
  if (!accounts)
    return `${id} (${failed ? "names unavailable" : "loading name"})`;
  const current = accounts.find(
    (a) => a.id === id && a.usageGeneration === generation,
  );
  return current
    ? current.label
    : `${id} (removed generation ${generation.slice(0, 8)})`;
}
export function formatBytes(n: number): string {
  if (n < 1024) return `${n.toLocaleString()} B`;
  return `${(n / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} KiB`;
}
export const moveStatus: Record<string, string> = {
  matched: "First recorded turn",
  no_accounting: "Accounting unavailable",
  legacy_unverified: "Legacy / unverified",
  binding_only: "Binding changed; runtime unopened",
  failed: "Move failed",
  ambiguous: "Ambiguous timing / attribution",
};
export function initialWindow(now = new Date()) {
  const to = now.toISOString().slice(0, 10);
  const from = new Date(`${to}T00:00:00Z`);
  from.setUTCDate(from.getUTCDate() - 29);
  return { from: from.toISOString().slice(0, 10), to };
}
export function validWindow(from: string, to: string): boolean {
  const a = Date.parse(from),
    b = Date.parse(to);
  return (
    /^\d{4}-\d{2}-\d{2}$/.test(from) &&
    /^\d{4}-\d{2}-\d{2}$/.test(to) &&
    Number.isFinite(a) &&
    Number.isFinite(b) &&
    new Date(a).toISOString().slice(0, 10) === from &&
    new Date(b).toISOString().slice(0, 10) === to &&
    b >= a &&
    b - a <= 89 * 86400000
  );
}
