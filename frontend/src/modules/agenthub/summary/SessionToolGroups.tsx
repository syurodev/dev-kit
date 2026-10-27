import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/bridge/errors";
import {
  applyReview,
  cancelReview,
  harnessAvailable,
  prepareRevoke,
  reconcileSession,
  sessionProfile,
  type Outcome,
  type ProfileView,
  type Review,
} from "@/bridge/harness";
import { agentHubResumeSession } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { HarnessReviewBody } from "@/workbench/HarnessReviewBody";
import { LatestRequest } from "@/workbench/WorkspaceMemoryModel";
import { needsReconcile, outcomeNotice, rows, sessionStatusLine } from "@/workbench/workspaceHarnessModel";

/**
 * The session's tool groups: what it is actually served, what is saved, and the
 * recovery controls. Revoke goes through the same review and confirmation as any
 * other change (deleting an override is not exempt just because the person asked);
 * Reconcile finishes a saved change whose publication failed and never proposes it
 * again; Reload is the existing explicit runtime reload. Nothing here closes or
 * reopens the session on its own, and leaving the card does not clear an override.
 */
export function SessionToolGroups({ sessionId }: { sessionId: string }) {
  const workspaceId = useAgentSessionsStore((state) => state.sessionsById[sessionId]?.workspaceId ?? "");
  const [wired, setWired] = useState(false);
  const [view, setView] = useState<ProfileView | null>(null);
  const [review, setReview] = useState<Review | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const latest = useRef(new LatestRequest());
  const reviewRef = useRef<Review | null>(null);
  reviewRef.current = review;

  const load = useCallback(async () => {
    if (!workspaceId) {
      return;
    }
    const current = latest.current.begin();
    try {
      const next = await sessionProfile(workspaceId, sessionId);
      // A late answer for another session (the card was re-pointed) is dropped.
      if (current() && next.sessionId === sessionId) {
        setView(next);
        setError("");
      }
    } catch (e) {
      if (current()) {
        setError(errorMessage(e));
      }
    }
  }, [workspaceId, sessionId]);

  useEffect(() => {
    let live = true;
    setView(null);
    setReview(null);
    setOutcome(null);
    setError("");
    harnessAvailable()
      .then((ok) => {
        if (live) {
          setWired(ok);
          if (ok) {
            void load();
          }
        }
      })
      .catch(() => live && setWired(false));
    const generation = latest.current;
    return () => {
      live = false;
      generation.invalidate();
    };
  }, [load]);

  // A prepared review is a lookup key held by Go; withdraw it when the card goes.
  useEffect(
    () => () => {
      const open = reviewRef.current;
      if (open) {
        void cancelReview(open.id).catch(() => {});
      }
    },
    [],
  );

  if (!wired || !workspaceId || !view) {
    return null;
  }
  const session = view.session ?? null;
  const overrides = session?.overrides ?? [];
  const reconcile = needsReconcile(session);
  const notice = outcome ? outcomeNotice(outcome) : null;
  // Reload is the runtime's own explicit action. An executable change cannot be
  // adopted by a reload, so the card offers it only when a reload can help.
  const reload = Boolean(session?.reloadRequired && !session.newSessionRequired);

  async function run(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    try {
      await action();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy("");
    }
  }

  const revoke = (group: string) =>
    run("prepare", async () => {
      setOutcome(null);
      setReview(await prepareRevoke(workspaceId, sessionId, [group]));
    });

  const back = async () => {
    const open = review;
    setReview(null);
    if (open) {
      await cancelReview(open.id).catch(() => {});
    }
  };

  const apply = () =>
    run("apply", async () => {
      if (!review) {
        return;
      }
      const spent = review;
      // Spent from here on, whatever the answer; never submitted a second time.
      setReview(null);
      reviewRef.current = null;
      try {
        setOutcome(await applyReview(spent.id));
      } finally {
        await load();
      }
    });

  const reconcileNow = () =>
    run("reconcile", async () => {
      setOutcome(await reconcileSession(workspaceId, sessionId));
      await load();
    });

  const reloadNow = () =>
    run("reload", async () => {
      await agentHubResumeSession(sessionId, true);
      await load();
    });

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
      {error ? (
        <p role="alert" className="text-caption-11 text-status-danger">
          {error}
        </p>
      ) : null}

      {review ? (
        <div className="space-y-2">
          <HarnessReviewBody review={review} />
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="outline" autoFocus disabled={busy === "apply"} onClick={() => void back()}>
              Back
            </Button>
            <Button type="button" size="sm" disabled={busy !== ""} onClick={() => void apply()}>
              {busy === "apply" ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {busy === "apply" ? "Waiting for confirmation…" : "Apply"}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {overrides.length > 0 ? (
            <ul aria-label="Groups turned on for this session" className="flex flex-col gap-1">
              {overrides.map((group) => {
                const row = rows(view).find((r) => r.name === group);
                return (
                  <li key={group} className="flex items-center gap-2 text-body-12">
                    <span className="min-w-0 flex-1 truncate text-text-primary" title={group}>
                      {group}
                      <span className="ml-2 text-caption-11 text-text-tertiary">
                        {row ? `running ${row.running}` : "this session only"}
                      </span>
                    </span>
                    <Button type="button" size="sm" variant="outline" disabled={busy !== ""} onClick={() => void revoke(group)} aria-label={`Revoke ${group}`}>
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
              <Button type="button" size="sm" disabled={busy !== ""} onClick={() => void reconcileNow()}>
                {busy === "reconcile" ? <Loader2 className="size-3.5 animate-spin" /> : null}
                Reconcile
              </Button>
            ) : null}
            {reload ? (
              <Button type="button" size="sm" variant="outline" disabled={busy !== ""} onClick={() => void reloadNow()}>
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
