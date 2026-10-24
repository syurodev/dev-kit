import type { PendingElicitation, PendingPermission } from "@/bridge/agenthub";

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

/** Any inline approval surface occupies the composer corner, regardless of kind. */
export function shouldCollapseToast(
  permissions: PendingPermission[],
  questions: PendingElicitation[],
  isAskedInline: (sessionId: string) => boolean,
) {
  return permissions.some((item) => isAskedInline(item.sessionId)) ||
    questions.some((item) => isAskedInline(item.sessionId));
}

export function heldToastIsLive(
  held: PendingPermission | PendingElicitation,
  permissions: PendingPermission[],
  questions: PendingElicitation[],
  isAskedInline: (sessionId: string) => boolean,
) {
  if (isAskedInline(held.sessionId)) return false;
  if ("expiresAt" in held) {
    return permissions.some((item) => item.id === held.id);
  }
  return questions.some((item) => item.id === held.id);
}
