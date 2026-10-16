import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type { Item, State } from "../../bindings/devkit/internal/prwatch/models";
import { callBridge } from "./client";

export type { Item as PullRequestItem, State as PullRequestState } from "../../bindings/devkit/internal/prwatch/models";
export type {
  ChangeRequest,
  Check,
  CheckSummary,
} from "../../bindings/devkit/internal/forge/models";

/** Emitted by Go when a pull request was attached to a session (pr.create, or by hand). */
export const PULL_REQUESTS_CHANGED_EVENT = "prwatch:changed";

/**
 * What the pull (or merge) request of each folder's branch looks like now, plus
 * the ones attached to the session. Go chooses the account and calls the host;
 * nothing credential-shaped is in the answer. `nextPollMs` says when to ask
 * again (-1: not until something happens).
 */
export function pullRequestState(sessionId: string, folders: string[]): Promise<State> {
  return callBridge(() => AppService.PullRequestState(sessionId, folders));
}

/** Ties a pull request, named by its address, to a session. Rejects with the reason. */
export function attachPullRequest(sessionId: string, folder: string, url: string): Promise<Item> {
  return callBridge(() => AppService.PullRequestAttach(sessionId, folder, url));
}

/** Forgets an attachment; closes and deletes nothing on the host. */
export function detachPullRequest(sessionId: string, host: string, path: string, number: number): Promise<void> {
  return callBridge(() => AppService.PullRequestDetach(sessionId, host, path, number));
}

/** Subscribes to attachments made outside this window. Returns the unsubscribe. */
export function onPullRequestsChanged(cb: (sessionId: string) => void): () => void {
  return Events.On(PULL_REQUESTS_CHANGED_EVENT, (event) => {
    cb(String((event.data as { sessionId?: unknown } | undefined)?.sessionId ?? ""));
  });
}
