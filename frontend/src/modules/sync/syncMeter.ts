import type { SyncReport } from "../../bridge/sync";

export type MeterSegment = { key: string; label: string; count: number; className: string };

/**
 * Figma `meter/PUSH` and `meter/PULL` (779:127, 779:153): one track per
 * direction, split by outcome. A segment with nothing in it is left out so the
 * track never shows a zero-width sliver and the legend never says "0 retried".
 */
export function pushSegments(r: SyncReport): MeterSegment[] {
  return [
    { key: "sent", label: "sent", count: r.pushSent, className: "bg-status-success" },
    { key: "retried", label: "retried", count: r.pushRetried, className: "bg-status-info" },
    { key: "conflicts", label: "conflicts", count: r.pushConflicts, className: "bg-status-warning" },
    { key: "dead", label: "dead-lettered", count: r.pushFailed, className: "bg-status-danger" },
  ].filter((s) => s.count > 0);
}

export function pullSegments(r: SyncReport): MeterSegment[] {
  const held = Math.max(0, r.pulled - r.applied);
  return [
    { key: "applied", label: "applied", count: r.applied, className: "bg-status-success" },
    { key: "held", label: "held by conflicts", count: held, className: "bg-status-warning" },
  ].filter((s) => s.count > 0);
}
