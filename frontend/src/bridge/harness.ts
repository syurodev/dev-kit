import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  GroupEdit,
  GroupRow,
  Outcome,
  ProfileView,
  Review,
  SessionState,
} from "../../bindings/devkit/internal/harnessconfig/models";
import { callBridge } from "./client";

export type { GroupEdit, GroupRow, Outcome, ProfileView, Review, SessionState };

/** The three states a workspace profile can give a group. */
export type GroupSettingValue = "inherit" | "on" | "off";

/** Whether the tool-group controls are wired. The entry points show only from this. */
export function harnessAvailable(): Promise<boolean> {
  return callBridge(async () => (await AppService.HarnessAvailable()) === true);
}

export function workspaceProfile(workspaceId: string): Promise<ProfileView> {
  return callBridge(() => AppService.HarnessWorkspaceProfile(workspaceId));
}

export function sessionProfile(
  workspaceId: string,
  sessionId: string,
): Promise<ProfileView> {
  return callBridge(() => AppService.HarnessSessionProfile(workspaceId, sessionId));
}

/** Writes nothing: the returned review is host-authored and still needs approval. */
export function prepareWorkspaceGroups(
  workspaceId: string,
  edits: GroupEdit[],
): Promise<Review> {
  return callBridge(() => AppService.HarnessPrepareWorkspaceGroups(workspaceId, edits));
}

export function prepareRevoke(
  workspaceId: string,
  sessionId: string,
  groups: string[],
): Promise<Review> {
  return callBridge(() => AppService.HarnessPrepareRevoke(workspaceId, sessionId, groups));
}

const changeListeners = new Set<() => void>();

/**
 * Something in this window may have changed a workspace's or a session's tool
 * groups (an apply, a reconcile). Views that show them read the backend again; the
 * event carries no data, so none of them can be handed a stale copy.
 */
export function onHarnessChanged(listener: () => void): () => void {
  changeListeners.add(listener);
  return () => changeListeners.delete(listener);
}

function harnessChanged(): void {
  for (const listener of [...changeListeners]) {
    listener();
  }
}

/** Asks through the shared confirmation flow, then commits. */
export function applyReview(id: string): Promise<Outcome> {
  return callBridge(() => AppService.HarnessApply(id)).finally(harnessChanged);
}

export function cancelReview(id: string): Promise<void> {
  return callBridge(() => AppService.HarnessCancel(id) as Promise<void>);
}

export function reconcileSession(
  workspaceId: string,
  sessionId: string,
): Promise<Outcome> {
  return callBridge(() => AppService.HarnessReconcile(workspaceId, sessionId)).finally(harnessChanged);
}
