import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  BudgetEdit,
  BudgetPage,
  GroupBudgetRow,
  GroupEdit,
  GroupRow,
  Outcome,
  ProfileView,
  Review,
  SessionState,
  ToolBudgetRow,
} from "../../bindings/devkit/internal/harnessconfig/models";
import type { AgentHarnessPolicyState } from "../../bindings/devkit/internal/bridge/models";
import { callBridge } from "./client";

export type {
  AgentHarnessPolicyState,
  BudgetEdit,
  BudgetPage,
  GroupBudgetRow,
  GroupEdit,
  GroupRow,
  Outcome,
  ProfileView,
  Review,
  SessionState,
  ToolBudgetRow,
};

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

/**
 * A bounded page of a workspace's result limits. The cursor is valid for the same
 * group filter only; the first page also carries the groups.
 */
export function workspaceBudgets(
  workspaceId: string,
  group = "",
  cursor = "",
): Promise<BudgetPage> {
  return callBridge(() => AppService.HarnessWorkspaceBudgets(workspaceId, group, cursor));
}

/** Writes nothing: the review (exact limits before and after) is written by Go. */
export function prepareBudgetEdits(
  workspaceId: string,
  edits: BudgetEdit[],
): Promise<Review> {
  return callBridge(() => AppService.HarnessPrepareBudgetEdits(workspaceId, edits));
}

/** The person's own opt-in for an agent's narrowing result-budget edits. */
export function harnessPolicy(): Promise<AgentHarnessPolicyState> {
  return callBridge(() => AppService.AgentHarnessPolicyGet());
}

/** Fails with a "changed" conflict when expectRevision is not the stored one. */
export function setAutoNarrowing(
  expectRevision: number,
  on: boolean,
): Promise<AgentHarnessPolicyState> {
  return callBridge(() => AppService.AgentHarnessAutoNarrowingSet(expectRevision, on));
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
