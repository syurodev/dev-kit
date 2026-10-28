import { GroupSetting } from "../../bindings/devkit/internal/harnessconfig/models";
import type {
  GroupEdit,
  GroupRow,
  Outcome,
  ProfileView,
  SessionState,
} from "../bridge/harness";

/**
 * The editor keeps only what the person changed: group name → the setting they
 * picked. Everything else is read from the saved profile each time, so a refresh
 * or a conflict can never write back a stale copy of the rest of the profile.
 */
export type Draft = Record<string, GroupSetting>;

export const settings: { value: GroupSetting; label: string }[] = [
  { value: GroupSetting.SettingInherit, label: "Default" },
  { value: GroupSetting.SettingOn, label: "On" },
  { value: GroupSetting.SettingOff, label: "Off" },
];

export function rows(view: ProfileView | null): GroupRow[] {
  return view?.groups ?? [];
}

/**
 * Groups the session's saved override still names that this build does not have.
 * Revoke on one of them prepares the removal-only repair Go derives; nothing is
 * decided here. With no group list at all nothing is claimed.
 */
export function unavailableOverrides(view: ProfileView | null): string[] {
  const known = new Set(rows(view).map((r) => r.name));
  if (known.size === 0) {
    return [];
  }
  return (view?.session?.overrides ?? []).filter((g) => !known.has(g));
}

/** The setting shown for a row: the draft's if the person changed it, else the saved one. */
export function shownSetting(row: GroupRow, draft: Draft): GroupSetting {
  return draft[row.name] ?? row.workspace;
}

/** Records a pick; picking what is already saved removes the pending change. */
export function pick(draft: Draft, row: GroupRow, setting: GroupSetting): Draft {
  const next = { ...draft };
  if (setting === row.workspace) {
    delete next[row.name];
  } else {
    next[row.name] = setting;
  }
  return next;
}

/**
 * Keeps unsaved picks across a refresh: a pick that now equals the saved setting
 * is done, and a pick for a group the server no longer registers is dropped. Nothing
 * else is discarded without the person asking.
 */
export function rebase(draft: Draft, view: ProfileView | null): Draft {
  const byName = new Map(rows(view).map((row) => [row.name, row]));
  const next: Draft = {};
  for (const [name, setting] of Object.entries(draft)) {
    const row = byName.get(name);
    if (row && row.workspace !== setting) {
      next[name] = setting;
    }
  }
  return next;
}

export function isDirty(draft: Draft): boolean {
  return Object.keys(draft).length > 0;
}

/** Which of the dialog's three writers is preparing or applying something. */
export type HarnessPending = {
  /** The ordinary group editor. */
  editor: "" | "prepare" | "apply";
  /** The stale-settings repair. */
  repair: "" | "prepare" | "apply";
  /** The Result limits tab (its own controller reports whether it prepares or applies). */
  limits: boolean;
};

/**
 * True while any of the dialog's writers is preparing or applying: the dialog must
 * not close then (a write that waits for the person's confirmation would finish
 * with nobody to show its outcome to). Every way out of the dialog (the corner
 * button, Escape, and each tab's Close or Cancel) uses this one rule.
 */
export function closeBlocked(pending: HarnessPending): boolean {
  return pending.editor !== "" || pending.repair !== "" || pending.limits;
}

/** The group edits to prepare, in the registry's order. Only differences. */
export function buildEdits(view: ProfileView | null, draft: Draft): GroupEdit[] {
  return rows(view)
    .filter((row) => draft[row.name] !== undefined && draft[row.name] !== row.workspace)
    .map((row) => ({ group: row.name, setting: draft[row.name] }));
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

export type Notice = {
  tone: "success" | "info" | "warning" | "danger";
  title: string;
  body: string;
  /** The change is saved but the session needs reconciling: never propose again. */
  reconcile: boolean;
};

/**
 * What to tell the person after an apply or a reconcile. It says "live" only when
 * the outcome says the current endpoint acknowledged it, and a saved-but-paused or
 * unknown outcome is a warning that points to Reconcile, never to trying again.
 */
export function outcomeNotice(outcome: Outcome): Notice {
  const reload = outcome.reloadRequired
    ? " Other running sessions keep the profile they opened with until they are reloaded."
    : "";
  switch (outcome.status) {
    case "paused":
      return {
        tone: "warning",
        title: "Saved, but the session's tool access is paused",
        body: outcome.recovery || "Reconcile the session to finish applying the change.",
        reconcile: true,
      };
    case "unknown":
      return {
        tone: "danger",
        title: "DevKit could not confirm the change was saved",
        body: outcome.recovery || "Reconcile the session to read the result back.",
        reconcile: true,
      };
    case "staged":
      return {
        tone: "info",
        title: "Saved and prepared",
        body: (outcome.recovery || "The session's runtime has not confirmed it yet.") + reload,
        reconcile: false,
      };
    default:
      return outcome.live
        ? {
            tone: "success",
            title: "Saved and live in the running session",
            body:
              "The session's tool connection serves the new groups. Whether the agent's own tool list refreshes depends on the agent; reload the session if it does not." +
              reload,
            reconcile: false,
          }
        : {
            tone: "info",
            title: "Saved",
            body: (outcome.recovery || "The change is saved.") + reload,
            reconcile: false,
          };
  }
}

/** True when the session card should offer Reconcile. */
export function needsReconcile(session: SessionState | null | undefined): boolean {
  return Boolean(session && (session.pending || session.paused));
}

/** One line for the session card's status; "Live" only when acknowledged. */
export function sessionStatusLine(session: SessionState | null | undefined): string {
  if (!session) {
    return "Not available";
  }
  if (session.paused) {
    return "Paused — reconcile to restore tool access";
  }
  if (session.pending) {
    return "A saved change is waiting to be applied";
  }
  if (session.newSessionRequired) {
    return "Start a new session to adopt the saved configuration";
  }
  if (session.live) {
    return session.reloadRequired ? "Live — reload to pick up saved changes" : "Live";
  }
  if (session.active) {
    return "Starting — not confirmed yet";
  }
  return session.reloadRequired ? "Saved — reload to apply" : "Not running";
}
