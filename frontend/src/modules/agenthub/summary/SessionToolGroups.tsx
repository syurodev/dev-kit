import { useEffect, useState, useSyncExternalStore } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  applyReview,
  cancelReview,
  harnessAvailable,
  onHarnessChanged,
  prepareRevoke,
  reconcileSession,
  sessionProfile,
} from "@/bridge/harness";
import { agentHubResumeSession } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { HarnessReviewBody } from "@/workbench/HarnessReviewBody";
import { needsReconcile, outcomeNotice, rows, sessionStatusLine, unavailableOverrides } from "@/workbench/workspaceHarnessModel";
import { keepFresh, ToolGroupsController, type ToolGroupsOps } from "./toolGroupsController";

/** How often an open card reads the backend again while the window is visible. */
const REFRESH_MS = 4000;

const ops: ToolGroupsOps = {
  profile: sessionProfile,
  prepareRevoke,
  apply: applyReview,
  cancel: cancelReview,
  reconcile: reconcileSession,
  reload: (sessionId) => agentHubResumeSession(sessionId, true),
};

/**
 * The session's tool groups: what it is actually served, what is saved, and the
 * recovery controls. All behavior lives in ToolGroupsController (stale answers for
 * a session the card has left are never shown, and a change made elsewhere is read
 * back); this is its view. Revoke goes through the same review and confirmation as
 * any other change; Reconcile finishes a saved change whose publication failed and
 * never proposes it again; Reload is the existing explicit runtime reload. Nothing
 * here closes or reopens the session, and leaving the card does not clear an
 * override.
 */
export function SessionToolGroups({ sessionId }: { sessionId: string }) {
  const workspaceId = useAgentSessionsStore((state) => state.sessionsById[sessionId]?.workspaceId ?? "");
  const [controller] = useState(() => new ToolGroupsController(ops));
  const [wired, setWired] = useState(false);
  const state = useSyncExternalStore(controller.subscribe, controller.getState, controller.getState);

  // Whether the host wired the controls at all. Until it says so, nothing is read.
  useEffect(() => {
    let live = true;
    harnessAvailable()
      .then((ok) => live && setWired(ok))
      .catch(() => live && setWired(false));
    return () => {
      live = false;
    };
  }, []);

  // Point the controller at this session. Re-pointing (or unmounting) invalidates
  // everything it still has in flight and withdraws a review it was holding.
  useEffect(() => {
    controller.setTarget(wired && workspaceId ? { workspaceId, sessionId } : null);
  }, [controller, wired, workspaceId, sessionId]);
  useEffect(() => () => controller.dispose(), [controller]);

  // Changes made outside the card: this window's editor and recovery actions, and
  // anything else (an agent's request, a failed publication, a reconcile elsewhere)
  // through a periodic read while the window is visible and when it wakes.
  useEffect(() => {
    if (!wired || !workspaceId) {
      return;
    }
    const stopEvents = onHarnessChanged(() => void controller.refresh());
    const stopPolling = keepFresh(controller, {
      intervalMs: REFRESH_MS,
      setInterval: (handler, ms) => window.setInterval(handler, ms),
      clearInterval: (handle) => window.clearInterval(handle as number),
      visible: () => document.visibilityState === "visible",
      onWake: (callback) => {
        window.addEventListener("focus", callback);
        document.addEventListener("visibilitychange", callback);
        return () => {
          window.removeEventListener("focus", callback);
          document.removeEventListener("visibilitychange", callback);
        };
      },
    });
    return () => {
      stopEvents();
      stopPolling();
    };
  }, [controller, wired, workspaceId, sessionId]);

  const { view, review, outcome, busy, error, loadError } = state;
  // A view read for another session is never drawn on this card.
  if (!wired || !workspaceId || !view || state.target?.sessionId !== sessionId || view.sessionId !== sessionId) {
    return null;
  }
  const session = view.session ?? null;
  const overrides = session?.overrides ?? [];
  const unavailable = unavailableOverrides(view);
  const reconcile = needsReconcile(session);
  const notice = outcome ? outcomeNotice(outcome) : null;
  // Reload is the runtime's own explicit action. An executable change cannot be
  // adopted by a reload, so the card offers it only when a reload can help.
  const reload = Boolean(session?.reloadRequired && !session.newSessionRequired);
  const shownError = error || loadError;

  return (
    <section
      data-testid="summary-toolGroups"
      aria-label="Agent tool groups"
      className="flex flex-col gap-2 border-t border-border-subtle px-3.5 py-2.5"
    >
      <h4 className="text-body-13 text-text-secondary">Tool groups</h4>
      <p data-testid="summary-toolGroups-status" className="text-caption-11 text-text-tertiary">
        {sessionStatusLine(session)}
      </p>

      {notice ? (
        <div role={notice.tone === "warning" || notice.tone === "danger" ? "alert" : "status"} className="rounded-md bg-state-soft px-2.5 py-2 text-caption-11">
          <p className="font-medium text-text-primary">{notice.title}</p>
          <p className="mt-0.5 text-text-secondary">{notice.body}</p>
        </div>
      ) : null}
      {shownError ? (
        <p role="alert" className="text-caption-11 text-status-danger">
          {shownError}
        </p>
      ) : null}

      {review ? (
        <div className="space-y-2">
          <HarnessReviewBody review={review} />
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="outline" autoFocus disabled={busy === "apply"} onClick={() => void controller.back()}>
              Back
            </Button>
            <Button type="button" size="sm" disabled={busy !== ""} onClick={() => void controller.apply()}>
              {busy === "apply" ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {busy === "apply" ? "Waiting for confirmation…" : "Apply"}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {unavailable.length > 0 ? (
            <p data-testid="summary-toolGroups-unavailable" role="status" className="text-caption-11 text-text-secondary">
              This session has a group turned on that this build does not have, so it cannot start. Revoke it to remove it
              (you confirm first); nothing is turned on by that.
            </p>
          ) : null}
          {overrides.length > 0 ? (
            <ul aria-label="Groups turned on for this session" className="flex flex-col gap-1">
              {overrides.map((group) => {
                const row = rows(view).find((r) => r.name === group);
                return (
                  <li key={group} className="flex items-center gap-2 text-body-12">
                    <span className="min-w-0 flex-1 truncate text-text-primary" title={group}>
                      {group}
                      <span className="ml-2 text-caption-11 text-text-tertiary">
                        {row ? `running ${row.running}` : unavailable.includes(group) ? "not available in this build" : "this session only"}
                      </span>
                    </span>
                    <Button type="button" size="sm" variant="outline" disabled={busy !== ""} onClick={() => void controller.revoke(group)} aria-label={`Revoke ${group}`}>
                      Revoke
                    </Button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="text-caption-11 text-text-tertiary">No groups were turned on for this session alone.</p>
          )}
          <div className="flex flex-wrap gap-2">
            {reconcile ? (
              <Button type="button" size="sm" disabled={busy !== ""} onClick={() => void controller.reconcile()}>
                {busy === "reconcile" ? <Loader2 className="size-3.5 animate-spin" /> : null}
                Reconcile
              </Button>
            ) : null}
            {reload ? (
              <Button type="button" size="sm" variant="outline" disabled={busy !== ""} onClick={() => void controller.reload()}>
                {busy === "reload" ? <Loader2 className="size-3.5 animate-spin" /> : null}
                Reload session
              </Button>
            ) : null}
          </div>
        </>
      )}
    </section>
  );
}
