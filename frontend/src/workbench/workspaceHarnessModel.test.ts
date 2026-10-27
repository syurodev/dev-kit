import { describe, expect, it } from "bun:test";

import { GroupSetting, OutcomeStatus } from "../../bindings/devkit/internal/harnessconfig/models";
import type { GroupRow, Outcome, ProfileView, SessionState } from "../bridge/harness";
import {
  buildEdits,
  formatBytes,
  isDirty,
  needsReconcile,
  outcomeNotice,
  pick,
  rebase,
  sessionStatusLine,
  shownSetting,
} from "./workspaceHarnessModel";

const row = (name: string, workspace: GroupSetting): GroupRow => ({
  name,
  tools: 3,
  definitionBytes: 2048,
  workspace,
  session: false,
  saved: "on" as GroupRow["saved"],
  running: "unknown" as GroupRow["running"],
});
const view = (...groups: GroupRow[]): ProfileView => ({
  workspaceId: "w",
  profileRevision: 1,
  profilePresent: true,
  groups,
});
const outcome = (patch: Partial<Outcome>): Outcome =>
  ({ status: "applied", commit: "committed", live: false, ...patch }) as Outcome;

describe("workspace group drafts", () => {
  const v = view(row("git", GroupSetting.SettingOff), row("db", GroupSetting.SettingInherit));

  it("keeps only differences and drops a pick that equals the saved setting", () => {
    let draft = pick({}, v.groups![0], GroupSetting.SettingOn);
    expect(isDirty(draft)).toBe(true);
    expect(shownSetting(v.groups![0], draft)).toBe(GroupSetting.SettingOn);
    draft = pick(draft, v.groups![0], GroupSetting.SettingOff);
    expect(isDirty(draft)).toBe(false);
  });

  it("builds edits for changed groups only, in registry order", () => {
    let draft = pick({}, v.groups![1], GroupSetting.SettingOff);
    draft = pick(draft, v.groups![0], GroupSetting.SettingInherit);
    expect(buildEdits(v, draft)).toEqual([
      { group: "git", setting: GroupSetting.SettingInherit },
      { group: "db", setting: GroupSetting.SettingOff },
    ]);
    expect(buildEdits(v, {})).toEqual([]);
  });

  it("never writes a group the registry no longer has", () => {
    expect(buildEdits(v, { gone: GroupSetting.SettingOn })).toEqual([]);
  });

  it("keeps unsaved picks across a refresh and forgets only what became true", () => {
    const draft = { git: GroupSetting.SettingOn, db: GroupSetting.SettingOff, gone: GroupSetting.SettingOn };
    // Someone else saved db=off and removed a group; the git pick is still pending.
    const refreshed = view(row("git", GroupSetting.SettingOff), row("db", GroupSetting.SettingOff));
    expect(rebase(draft, refreshed)).toEqual({ git: GroupSetting.SettingOn });
  });

  it("treats a missing view as empty", () => {
    expect(buildEdits(null, { git: GroupSetting.SettingOn })).toEqual([]);
    expect(rebase({ git: GroupSetting.SettingOn }, null)).toEqual({});
  });
});

describe("outcome wording", () => {
  it("says live only when the outcome says so", () => {
    expect(outcomeNotice(outcome({ live: true })).title).toContain("live");
    expect(outcomeNotice(outcome({ live: false })).title).toBe("Saved");
    expect(outcomeNotice(outcome({ status: OutcomeStatus.OutcomeStaged })).tone).toBe("info");
  });

  it("points a saved-but-paused change and an unknown commit to reconcile, never to trying again", () => {
    for (const status of [OutcomeStatus.OutcomePaused, OutcomeStatus.OutcomeUnknown]) {
      const n = outcomeNotice(outcome({ status, live: true }));
      expect(n.reconcile).toBe(true);
      expect(n.title.toLowerCase()).not.toContain("live");
      expect(n.tone === "warning" || n.tone === "danger").toBe(true);
    }
  });

  it("mentions the pinned profile of other sessions after a workspace change", () => {
    expect(outcomeNotice(outcome({ reloadRequired: true })).body).toContain("until they are reloaded");
  });
});

describe("session card", () => {
  const session = (patch: Partial<SessionState>): SessionState =>
    ({ state: "current", live: false, active: false, paused: false, pending: false, reloadRequired: false, newSessionRequired: false, overrides: [], ...patch }) as SessionState;

  it("offers reconcile for a paused or pending session only", () => {
    expect(needsReconcile(session({ paused: true }))).toBe(true);
    expect(needsReconcile(session({ pending: true }))).toBe(true);
    expect(needsReconcile(session({ live: true }))).toBe(false);
    expect(needsReconcile(null)).toBe(false);
  });

  it("never reports Live for a session that is not acknowledged", () => {
    expect(sessionStatusLine(session({ active: true }))).toContain("not confirmed");
    expect(sessionStatusLine(session({ live: true }))).toBe("Live");
    expect(sessionStatusLine(session({ paused: true, live: true }))).toContain("Paused");
    expect(sessionStatusLine(session({ newSessionRequired: true }))).toContain("new session");
  });
});

describe("sizes", () => {
  it("formats definition bytes", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(2048)).toBe("2.0 KiB");
  });
});
