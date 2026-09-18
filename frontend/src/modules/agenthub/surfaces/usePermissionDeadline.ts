import { useEffect, useState } from "react";

import type { PendingPermission } from "../../../bridge/agenthub";

/**
 * usePermissionDeadline renders the deadline the backend already set. It does
 * not create one.
 *
 * A permission usually has no deadline: the gate waits for the person, the way
 * the vendors' own apps do, and the request then ships no `expiresAt` — so
 * there is nothing to count and no clock is drawn. When a deadline IS
 * configured (`Service.PermissionTimeout`), the request carries it and the
 * surface shows it. Two things follow:
 *
 * 1. The countdown starts at `expiresAt`, not at mount. A toast routinely mounts
 *    late — rail closed, another module, lazy chunk still loading — and a clock
 *    started at mount shows time the gate does not have; a remount shows more.
 * 2. Reaching zero does NOT resolve the request. Cancelling at zero is a second
 *    authority racing the real one, and when the mount was late it fires early
 *    and kills a request the user could still approve. The gate closes itself,
 *    and the pending item leaves the event stream on its own.
 */
export function usePermissionDeadline(
  pending: Pick<PendingPermission, "id" | "createdAt" | "expiresAt">,
): { secondsLeft?: number; expired: boolean } {
  const [now, setNow] = useState(() => Date.now());

  const deadline = resolvePermissionDeadline(pending);

  useEffect(() => {
    if (deadline === null) return;
    setNow(Date.now());
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [pending.id, deadline]);

  if (deadline === null) return { expired: false };
  const secondsLeft = Math.max(0, Math.ceil((deadline - now) / 1000));
  return { secondsLeft, expired: secondsLeft === 0 };
}

/** When this request stops being answerable, or null when nothing ends it. */
export function resolvePermissionDeadline(
  pending: Pick<PendingPermission, "createdAt" | "expiresAt">,
): number | null {
  const expires = Date.parse(pending.expiresAt ?? "");
  return Number.isNaN(expires) ? null : expires;
}
