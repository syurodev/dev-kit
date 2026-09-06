import { useEffect, useState } from "react";

import type { PendingPermission } from "../../../bridge/agenthub";

/**
 * usePermissionDeadline renders the deadline the backend already set. It does
 * not create one.
 *
 * The gate is enforced in Go (`Service.mediatePermission` bounds the wait with
 * `PermissionTimeout`, default `interaction.DefaultTimeout`); the request ships
 * `expiresAt` so the surface can show it. Two things follow, and both were
 * wrong in the first implementation:
 *
 * 1. The countdown starts at `expiresAt`, not at mount. A toast routinely
 *    mounts late — the rail was closed, the user was in another module, the
 *    lazy chunk was still loading — and a clock started at mount shows time the
 *    gate no longer has. A remount would restart it and show even more.
 * 2. Reaching zero does NOT resolve the request. A client that sends a cancel
 *    at zero is a second authority racing the real one, and in the mount-late
 *    case it fires early and kills a request the user still had time to
 *    approve. The gate closes itself; the pending item then disappears from the
 *    event stream on its own.
 */
export function usePermissionDeadline(
  pending: Pick<PendingPermission, "id" | "createdAt" | "expiresAt">,
  fallbackSeconds = 60,
): { secondsLeft: number; expired: boolean } {
  const [now, setNow] = useState(() => Date.now());

  const deadline = resolvePermissionDeadline(pending, fallbackSeconds);

  useEffect(() => {
    setNow(Date.now());
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [pending.id, deadline]);

  const secondsLeft = Math.max(0, Math.ceil((deadline - now) / 1000));
  return { secondsLeft, expired: secondsLeft === 0 };
}

export function resolvePermissionDeadline(
  pending: Pick<PendingPermission, "createdAt" | "expiresAt">,
  fallbackSeconds: number,
): number {
  const expires = Date.parse(pending.expiresAt ?? "");
  if (!Number.isNaN(expires)) {
    return expires;
  }
  // An older backend may only stamp createdAt. Deriving from it is still
  // anchored to when the request arrived, which is the part that matters.
  const created = Date.parse(pending.createdAt ?? "");
  if (!Number.isNaN(created)) {
    return created + fallbackSeconds * 1000;
  }
  // Nothing to anchor to: show a full window rather than a wrong one, and let
  // the backend be the thing that ends it.
  return Date.now() + fallbackSeconds * 1000;
}
