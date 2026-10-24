import type { PendingPermission } from "@/bridge/agenthub";

/** Pick the oldest permission that is not already answerable on screen. */
export function selectToastPermission(
  queue: PendingPermission[],
  fallback: PendingPermission | null,
  isAskedInline: (sessionId: string) => boolean,
) {
  const byId = new Map<string, PendingPermission>();
  for (const item of [...queue, ...(fallback ? [fallback] : [])]) byId.set(item.id, item);
  return [...byId.values()]
    .filter((item) => !isAskedInline(item.sessionId))
    .sort((a, b) => {
      const aTime = Date.parse(a.createdAt);
      const bTime = Date.parse(b.createdAt);
      return (Number.isFinite(aTime) ? aTime : 0) - (Number.isFinite(bTime) ? bTime : 0);
    })[0] ?? null;
}

/** A corner card would cover the action row of an inline permission card. */
export function shouldCollapseToastPermission(
  queue: PendingPermission[],
  isAskedInline: (sessionId: string) => boolean,
) {
  return queue.some((item) => isAskedInline(item.sessionId));
}
