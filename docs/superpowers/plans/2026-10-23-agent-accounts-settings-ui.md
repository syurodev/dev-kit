# Agent Accounts Settings UI Implementation Plan (Plan 3 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the person a Settings page, a transcript line, a summary-card row and a toast for the several-accounts-per-vendor failover that Plans 1 and 2 built in Go (`docs/superpowers/plans/2026-10-22-agent-multi-account-failover.md`, `...-agent-accounts-window-backend.md`; spec `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`; AGENTS.md "A vendor can have several accounts...").

**Architecture:** Frontend only; no Go file changes. A thin `bridge/agentAccounts.ts` over the generated bindings; a pure model (`accountModel.ts`, `accountPolicy.ts`) that every surface reads; presentational editors fed by props (so the gallery can draw every state) under two containers that own the queries, the compare-and-swap save and the events; the transcript and the summary card read the same accounts list through one shared query key.

**Tech Stack:** React 19 + TypeScript, TanStack Query v5, shadcn components already in `components/ui` (`Switch`, `NativeSelect`, `Input`, `Button`, `Badge`, `ActionTooltip`, `ConfirmDialog`, `AlertDialog`, `Checkbox`, `Skeleton`), sonner, Tailwind with the repo's type scale and `--status-*` tokens, `bun test --isolate` with `renderToStaticMarkup`. No new library, colour, layout primitive or animation.

---

## The constraint this plan is built on

"Follow the current UI, do not do anything different." Every piece below copies an existing screen: the page is a `TabColumn` + `SectionHeader` + `VendorSwitch` page like Runtimes; the policy form is `SubagentLimitsEditor` / `AgentSubagentLimitsSetting` (container reads, `{revision, ...}` CAS save, error line + refetch on a stale revision); a row is a Model profiles row; the usage bars are the summary card's `role="meter"`; status colour (`text-status-*`) says how safe something is and `--hue-*` is never used; icon-only buttons are `ActionTooltip`; Claude's sign-in is `useClaudeSignIn` (terminal tab, `closeSettings()` first, toast) and Codex's is `useCodexSignIn`; the toast follows `UpdateNotifier`'s render-null component. UI copy is English. Where the current UI did not settle something, the most conservative existing pattern was taken and is listed next.

## Decisions (review these)

1. **Where the code lives.** All the new UI is under `frontend/src/modules/agenthub/accounts/`, and Settings only mounts it from `AgentsPages.tsx` (the way it already mounts `AgentEnvironmentPanel`). New files under `modules/settings/` would each add an edge to the `settings -> agenthub` ratchet (baseline 3); this adds none, so `moduleBoundaryRatchet.test.ts` is not touched.
2. **Vendors.** The page's Claude | Codex switch offers those two only (`VendorSwitch` gains an optional `vendors` prop; its default and the other pages are unchanged). opencode has no accounts. The remembered vendor is the same shared localStorage choice; a remembered "opencode" falls back to Claude on this page.
3. **Reordering.** Move up / Move down icon buttons (tooltips, disabled at the ends) that save the whole policy at once with the same compare-and-swap as every other save. No drag and drop, no separate "Save order". The built-in account is an ordinary row in the order.
4. **Turning switching on.** One form per vendor (switch, thresholds, re-check age, refusal choice) with a Save button, like Subagent limits. The terms dialog opens when the switch is first flipped on while the terms were never accepted; Cancel leaves it off; "Turn on" needs the tick and only marks the form, so nothing is stored until Save. `acknowledgeRisk` is sent as true only on a Save that enables switching right after the dialog was accepted, never otherwise.
5. **Refusal choice control.** `NativeSelect` (as Model profiles uses) instead of the Radix `Select`, so the options render in the SSR tests; the chosen option's sentence is shown under the field.
6. **Add and sign in.** "Add account" asks for a name (40 characters, the backend's limit) and creates the account; the new row, signed out, shows its sign-in inline (Claude: terminal tab; Codex: inside DevKit) with the same buttons Runtimes has. Nothing signs in automatically. A signed-out row also has "Check again" (refresh status).
7. **After a Claude sign-in.** Starting it closes Settings (the terminal tab is behind the modal, exactly as under Runtimes) and the outcome is a toast; Settings is not reopened, because no existing sign-in does that.
8. **Refresh usage.** An icon button on signed-in rows. The backend answers with the stored account, no flag, when asked too soon, so an unchanged `observedAt` is shown as a quiet inline "Nothing new…" status. There is no "Refresh all".
9. **Shared, rename, remove.** Every account, built-in included, has the "Other people also use this login" switch and Refresh usage; only added accounts have Rename and Remove (the built-in name is fixed). Remove asks first (`ConfirmDialog`) and the backend's refusal ("A session is using that account. Close it first.") appears inside the dialog.
10. **Time-dependent text.** A past `exhaustedUntil` / `cooldownUntil` says nothing; a usage window past its `resetsAt` is drawn empty with "reset since the last reading"; "Observed/Checked N ago" and "resets in …" move on once a minute on an open page. `cooldownUntil` (migration 46) is shown as "Left out for now: it could not be opened · retry in …".
11. **Switch toast.** `agentaccounts:switched` always updates the window's session row (so the summary card's Account row is right) and the transcript line is always drawn; the toast "Moved to X" (reason as its description) is shown only when that session is the one on screen. It is a component that renders null, mounted in the Workbench beside `AgentSessionsSubscriber` (after the vault is open), not in `App.tsx`.
12. **Transcript line.** A quiet one-liner at the end of the turn it follows: "Switched from A to B · reason", or in the warning tone "Could not switch from A to B: reason". Names come from the accounts list; while it loads, the built-in account has its own fallback name and any other account reads "another account"; one that was removed reads "a removed account".
13. **Summary card.** An "Account" row right after "Agent", only when the session's `accountId` is non-empty (a session that never moved shows none) and its name is known. After a move back to the built-in account it shows "System default" / "DevKit default".
14. **Copy.** Short and factual: one line on when switching happens and that DevKit holds no token (above the form and in the dialog), and a terms warning that makes no claim that anything is permitted (the spec's "Risk and terms"). Navigation keywords for the page: accounts several multiple subscription usage quota threshold switch failover order priority terms add claude codex.
15. **Left out on purpose.** No "every account is limited" toast (the turn's own `agent_usage_limit` error already says it), no usage polling, no "Refresh all", no drag and drop, no opencode accounts, and no special server-mode UI (there the backend answers its "desktop app only" sentence and the page shows it as the error line).

## Facts the plan relies on (read from the code, not guessed)

- Bound methods (`frontend/bindings/devkit/internal/bridge/appservice.ts`): `AgentAccountsList|Add|Rename|Remove|SetShared|RefreshStatus|RefreshUsage|StartSignIn|StartBrowserSignIn|StartDeviceSignIn`, `AgentAccountPolicyGet|Set(state, acknowledgeRisk)`. `AgentAccountInfo` is `{id, vendor, label, builtin, signedIn, shared, exhaustedUntil, cooldownUntil, usage: LimitWindow[] | null, usageSource, observedAt}` with times in Unix seconds and `0` for none; `exhaustedUntil` / `cooldownUntil` may be past values. The policy is `{revision, riskAcknowledgedAt, vendors: {[vendor]?: {enabled, thresholdPercent, weeklyThresholdPercent, usageMaxAgeSeconds, order: string[] | null, onLimitRefusal}} | null}`.
- Go limits (`internal/preferences/accountpolicy.go`): threshold and weekly threshold 50-100 (default 90; weekly 0 = the same), `usageMaxAgeSeconds` 60-3600 (default 300), at most 32 order ids, `onLimitRefusal` is `none` | `resendIfNoProgress` | `continue`; enabling needs `riskAcknowledgedAt`, which DevKit stamps itself when `acknowledgeRisk` is true. An account name is 1-40 characters (`maxAccountLabel`).
- Not in the generated bindings, hand-typed here: the `accountId` on `agenthub:signInCompleted`, `SessionUpdate.accountNotice` (typed in Go, `update.go`), and the `agentaccounts:switched` payload `{sessionId, from, to, reason}`. `AgentSession.AccountID` is PascalCase in the bound JSON.
- `AgentAccountsRefreshUsage` answers with the stored account, no flag, when asked inside the backend's floor; the bridge errors are already published sentences (`A session is using that account. Close it first.`, `You already have an account with that name.`, `The account switching rules changed. Refresh and apply your change again.`, `Account switching rules: ...`).
- In server mode only `AgentAccountsList` and `AgentAccountPolicyGet` work.

## File structure

| Path | Create / Modify | Responsibility |
| --- | --- | --- |
| `frontend/src/bridge/agentAccounts.ts` | Create | Wrappers over the generated `AgentAccounts*` / `AgentAccountPolicy*` bindings; null to `[]` normalisation; the policy and account types; `agentaccounts:changed` / `agentaccounts:switched` helpers |
| `frontend/src/modules/agenthub/accounts/accountModel.ts` | Create | Pure: account names and fallbacks, ordering, usage rows, relative time, notes, notice and toast text |
| `frontend/src/modules/agenthub/accounts/accountPolicy.ts` | Create | Pure: the switching rules' defaults, validation (50-100, 60-3600, 32 ids), refusal choices, the copy |
| `frontend/src/modules/agenthub/accounts/accountActions.ts` | Create | Pure-ish: `attempt` (refusal sentence + refetch), cached-list helpers |
| `frontend/src/modules/agenthub/accounts/useAgentAccounts.ts` | Create | Query keys, `useAgentAccounts`, `useAgentAccountPolicy`, `watchAgentAccounts`, `useNow` |
| `frontend/src/modules/agenthub/accounts/UsageMeter.tsx` | Create | One usage window: the summary card's `role="meter"` bar and tone text |
| `frontend/src/modules/agenthub/accounts/AccountRow.tsx` | Create | One account: state, usage, reading age, shared switch, icon actions, rename, remove confirmation |
| `frontend/src/modules/agenthub/accounts/AddAccountForm.tsx` | Create | "Add account" name form |
| `frontend/src/modules/agenthub/accounts/AccountsEditor.tsx` | Create | Presentational list in order, empty / loading / error states |
| `frontend/src/modules/agenthub/accounts/AccountRiskDialog.tsx` | Create | Terms warning body and the `AlertDialog` + `Checkbox` |
| `frontend/src/modules/agenthub/accounts/AccountPolicyEditor.tsx` | Create | Presentational switching form (CAS-saved by its container) |
| `frontend/src/modules/agenthub/accounts/AccountSignIn.tsx` | Create | Claude (terminal) and Codex (in-app) sign-in for one account |
| `frontend/src/modules/agenthub/accounts/AgentAccountsSetting.tsx` | Create | Container: reads, changes, refreshes, saves the policy |
| `frontend/src/modules/agenthub/transcript/AccountNotice.tsx` | Create | The transcript line and its container |
| `frontend/src/workbench/AgentAccountsSubscriber.tsx` | Create | Refetch on event/focus, store patch and toast when a session moves |
| `frontend/src/dev/AgentAccountsSection.tsx` | Create | `?gallery#agent-accounts` |
| `frontend/src/bridge/agentSignIn.ts`, `modules/agenthub/CodexSignIn.tsx`, `ClaudeSignIn.tsx`, `AgentHubView.tsx` | Modify | `accountId` on the sign-in event; account-aware trackers and hooks; `note` prop |
| `frontend/src/modules/agenthub/types.ts`, `transcript.ts`, `surfaces/AgentTurn.tsx` | Modify | `AccountNotice` type, the `devkit_account_notice` row, drawing `turn.notes` |
| `frontend/src/bridge/agenthub.ts`, `summary/summaryModel.ts`, `surfaces/AgentHubContent.tsx` | Modify | `AgentSession.accountId`, the Account row |
| `frontend/src/modules/settings/{settingsNavigation,SettingsLayout,SettingsView,AgentsPages}` | Modify | The `agents-accounts` page |
| `frontend/src/workbench/Workbench.tsx`, `src/dev/{ComponentGallery,AgentHubTranscriptSection,AgentSummarySection}.tsx` | Modify | Mount the subscriber; gallery fixtures |
| `AGENTS.md` | Modify | The "Accounts UI" bullet |

Every `*.test.ts(x)` beside a created file is created with it (the tasks list them). No Go file changes.

---

## Tasks

Run frontend commands from `frontend/`; `bun` is on `PATH` through `~/.bun/bin`. Each task is one commit. Tests are written first and are the whole component-test style the repo uses: `renderToStaticMarkup` / `renderToString`, no DOM library, `QueryClientProvider` + `setQueryData` where a hook runs, `mock.module` only when unavoidable (not needed here).

### Task 0: Preconditions (no code, no commit)

- [ ] **Step 1: Check the branch and the tree**

Run: `git rev-parse --abbrev-ref HEAD && git status --short`

Expected: `feat/agent-accounts-window-backend` and nothing else. Do not switch branches and do not push.

- [ ] **Step 2: Check the generated bindings carry the account methods**

Run: `grep -c "^export function AgentAccount" frontend/bindings/devkit/internal/bridge/appservice.ts`

Expected: `12` (`AgentAccountPolicyGet|Set`, and `AgentAccountsList|Add|Rename|Remove|SetShared|RefreshStatus|RefreshUsage|StartSignIn|StartBrowserSignIn|StartDeviceSignIn`). If it is lower, the Go bridge is not on this branch: stop. Nothing in this plan regenerates bindings.

- [ ] **Step 3: Baseline**

Run: `cd frontend && ls node_modules >/dev/null 2>&1 || bun install; bun test --isolate src/modules/settings src/modules/agenthub src/bridge src/workbench`

Expected: `0 fail`. Every later "run" command in this plan is scoped the same way; Task 14 runs everything.


### Task 1: The bridge wrapper over the real bindings

Everything the page needs from Go, in one file, shaped once. The bindings type `usage` and `order` as `T[] | null` and the account's times as Unix seconds; the policy is a compare-and-swap on `revision`. `agentaccounts:changed` has no payload; `agentaccounts:switched` is `{sessionId, from, to, reason}`. Nothing here is a Go change.

**Files:**
- Create: `frontend/src/bridge/agentAccounts.ts`
- Create: `frontend/src/bridge/agentAccounts.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/bridge/agentAccounts.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AgentAccountInfo, AgentAccountPolicyState } from "../../bindings/devkit/internal/bridge/models";
import { accountOf, parseAccountSwitched, policyOf, rawPolicyOf } from "./agentAccounts";

const raw: AgentAccountInfo = {
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: true,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [{ label: "5-hour", usedPercent: 41.5, resetsAt: 1_800_000_000, status: "ok" }],
  usageSource: "checked",
  observedAt: 1_799_999_000,
};

describe("accountOf", () => {
  test("keeps what Go sent", () => {
    expect(accountOf(raw)).toEqual({
      id: "claude-work",
      vendor: "claude",
      label: "Work",
      builtin: false,
      signedIn: true,
      shared: true,
      exhaustedUntil: 0,
      cooldownUntil: 0,
      usage: [{ label: "5-hour", usedPercent: 41.5, resetsAt: 1_800_000_000, status: "ok" }],
      usageSource: "checked",
      observedAt: 1_799_999_000,
    });
  });

  test("a null usage list is an empty one, and an absent reset or status is 0 and empty", () => {
    expect(accountOf({ ...raw, usage: null }).usage).toEqual([]);
    const bare = accountOf({ ...raw, usage: [{ label: "Weekly", usedPercent: 10 }] });
    expect(bare.usage).toEqual([{ label: "Weekly", usedPercent: 10, resetsAt: 0, status: "" }]);
  });
});

describe("policy", () => {
  const state: AgentAccountPolicyState = {
    revision: 3,
    riskAcknowledgedAt: "2026-10-23T08:00:00Z",
    vendors: {
      claude: {
        enabled: true,
        thresholdPercent: 90,
        weeklyThresholdPercent: 0,
        usageMaxAgeSeconds: 300,
        order: ["claude-work", "claude-default"],
        onLimitRefusal: "none",
      },
      codex: undefined,
    },
  };

  test("reads vendors, drops an absent one, and never hands out Go's array", () => {
    const policy = policyOf(state);
    expect(Object.keys(policy.vendors)).toEqual(["claude"]);
    expect(policy.vendors.claude.order).toEqual(["claude-work", "claude-default"]);
    expect(policy.vendors.claude.order).not.toBe(state.vendors?.claude?.order);
    expect(policy.revision).toBe(3);
    expect(policy.riskAcknowledgedAt).toBe("2026-10-23T08:00:00Z");
  });

  test("null vendors and a null order are empty", () => {
    expect(policyOf({ revision: 0, riskAcknowledgedAt: "", vendors: null })).toEqual({
      revision: 0,
      riskAcknowledgedAt: "",
      vendors: {},
    });
    const noOrder = policyOf({
      revision: 1,
      riskAcknowledgedAt: "",
      vendors: { claude: { ...state.vendors!.claude!, order: null } },
    });
    expect(noOrder.vendors.claude.order).toEqual([]);
  });

  test("goes back to Go as it was read", () => {
    const policy = policyOf(state);
    const again = rawPolicyOf(policy);
    expect(again.revision).toBe(3);
    expect(again.vendors?.claude?.order).toEqual(["claude-work", "claude-default"]);
    expect(policyOf(again)).toEqual(policy);
  });
});

describe("parseAccountSwitched", () => {
  test("reads the event main.go emits, array-wrapped or not", () => {
    const event = { sessionId: "s1", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };
    expect(parseAccountSwitched(event)).toEqual(event);
    expect(parseAccountSwitched([event])).toEqual(event);
  });

  test("a missing reason is empty, anything malformed is not a switch", () => {
    expect(parseAccountSwitched({ sessionId: "s1", from: "a", to: "b" })?.reason).toBe("");
    expect(parseAccountSwitched(null)).toBeNull();
    expect(parseAccountSwitched({ sessionId: "s1", from: "a" })).toBeNull();
    expect(parseAccountSwitched({ sessionId: "", from: "a", to: "b" })).toBeNull();
    expect(parseAccountSwitched({ sessionId: "s1", from: "a", to: "" })).toBeNull();
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/bridge/agentAccounts.test.ts`

Expected: `error: Cannot find module './agentAccounts'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/bridge/agentAccounts.ts`:

```ts
import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  AgentAccountInfo as RawAccount,
  AgentAccountPolicyState as RawPolicy,
  AgentAccountVendorPolicyState as RawVendorPolicy,
} from "../../bindings/devkit/internal/bridge/models";
import type { SignInFlow } from "../../bindings/devkit/internal/agenthub/models";
import { callBridge } from "./client";

export type { SignInFlow };

/**
 * Several accounts per agent (Settings → Agents → Accounts & limits).
 *
 * Every method here is the window's own: there is no MCP tool for any of them,
 * the backend refuses an agent caller, and only the list and the policy read
 * work in server mode. An account is a row — the agent's own login lives in the
 * account's home folder, and neither a credential nor that folder's path ever
 * reaches the window.
 */

/** One usage ceiling an account has: a window that resets. `resetsAt` is Unix seconds, 0 when unknown. */
export type AccountWindow = {
  label: string;
  usedPercent: number;
  resetsAt: number;
  /** "ok", "warning" or "reached"; "" when the vendor gave none. */
  status: string;
};

/**
 * One vendor account as Settings draws it. Times are Unix seconds, 0 for none.
 * `exhaustedUntil` and `cooldownUntil` can be past values — nothing clears them
 * when time passes — so a time at or before now means "not limited".
 */
export type AgentAccount = {
  id: string;
  vendor: string;
  label: string;
  /** The machine's own login (Claude) or DevKit's own home (Codex): fixed name, never removed. */
  builtin: boolean;
  signedIn: boolean;
  /** Other people also use this login, so what DevKit saw of its usage is a lower bound. */
  shared: boolean;
  exhaustedUntil: number;
  cooldownUntil: number;
  usage: AccountWindow[];
  /** "observed" (seen in a session's own traffic), "checked" (asked of the vendor) or "". */
  usageSource: string;
  observedAt: number;
};

/** One vendor's switching rules. A zero threshold or max age in storage means "the default". */
export type VendorPolicy = {
  enabled: boolean;
  thresholdPercent: number;
  /** 0 = the same as `thresholdPercent`. */
  weeklyThresholdPercent: number;
  usageMaxAgeSeconds: number;
  /** Account ids, first used first; may name an account that was removed. */
  order: string[];
  /** "none", "resendIfNoProgress" or "continue". */
  onLimitRefusal: string;
};

/** The whole rules preference: `revision` is the compare-and-swap token. */
export type AccountPolicy = {
  revision: number;
  /** RFC 3339, written by DevKit when the person accepted the terms warning; "" until then. */
  riskAcknowledgedAt: string;
  vendors: Record<string, VendorPolicy>;
};

/** The bindings type `usage` and `order` as nullable though Go sends `[]`: normalised here, once. */
export function accountOf(raw: RawAccount): AgentAccount {
  return {
    id: raw.id,
    vendor: raw.vendor,
    label: raw.label,
    builtin: raw.builtin,
    signedIn: raw.signedIn,
    shared: raw.shared,
    exhaustedUntil: raw.exhaustedUntil ?? 0,
    cooldownUntil: raw.cooldownUntil ?? 0,
    usage: (raw.usage ?? []).map((window) => ({
      label: window.label,
      usedPercent: window.usedPercent,
      resetsAt: window.resetsAt ?? 0,
      status: window.status ?? "",
    })),
    usageSource: raw.usageSource ?? "",
    observedAt: raw.observedAt ?? 0,
  };
}

function vendorPolicyOf(raw: RawVendorPolicy): VendorPolicy {
  return {
    enabled: raw.enabled,
    thresholdPercent: raw.thresholdPercent,
    weeklyThresholdPercent: raw.weeklyThresholdPercent,
    usageMaxAgeSeconds: raw.usageMaxAgeSeconds,
    order: [...(raw.order ?? [])],
    onLimitRefusal: raw.onLimitRefusal,
  };
}

export function policyOf(raw: RawPolicy): AccountPolicy {
  const vendors: Record<string, VendorPolicy> = {};
  for (const [name, value] of Object.entries(raw.vendors ?? {})) {
    if (value) vendors[name] = vendorPolicyOf(value);
  }
  return { revision: raw.revision, riskAcknowledgedAt: raw.riskAcknowledgedAt ?? "", vendors };
}

/**
 * What crosses to Go. `riskAcknowledgedAt` is sent back as read and ignored
 * there: DevKit stamps it itself when `acknowledgeRisk` is true.
 */
export function rawPolicyOf(policy: AccountPolicy): RawPolicy {
  const vendors: Record<string, RawVendorPolicy> = {};
  for (const [name, value] of Object.entries(policy.vendors)) {
    vendors[name] = { ...value, order: [...value.order] };
  }
  return { revision: policy.revision, riskAcknowledgedAt: policy.riskAcknowledgedAt, vendors };
}

export function agentAccountsList(vendor: string): Promise<AgentAccount[]> {
  return callBridge(async () => ((await AppService.AgentAccountsList(vendor)) ?? []).map(accountOf));
}

/** Adds an account: its folder only. Nothing is signed in yet. */
export function agentAccountsAdd(vendor: string, label: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsAdd(vendor, label)));
}

export function agentAccountsRename(id: string, label: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRename(id, label)));
}

/** Refused while a live session runs as the account; the sentence says so. */
export function agentAccountsRemove(id: string): Promise<void> {
  return callBridge(() => AppService.AgentAccountsRemove(id) as Promise<void>);
}

export function agentAccountsSetShared(id: string, shared: boolean): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsSetShared(id, shared)));
}

/** Re-reads whether the account is signed in. */
export function agentAccountsRefreshStatus(id: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRefreshStatus(id)));
}

/**
 * Asks the vendor what the account has left. The backend limits how often that
 * runs per account and answers with what it already has when asked too soon —
 * no error, no flag — so compare `observedAt` before and after to tell.
 */
export function agentAccountsRefreshUsage(id: string): Promise<AgentAccount> {
  return callBridge(async () => accountOf(await AppService.AgentAccountsRefreshUsage(id)));
}

/** Claude's terminal sign-in at an added account's folder: the id of the terminal session to attach a tab to. */
export function agentAccountsStartSignIn(id: string, methodId: string, rows: number, cols: number): Promise<string> {
  return callBridge(() => AppService.AgentAccountsStartSignIn(id, methodId, rows, cols));
}

/** Codex's in-app sign-in at an added account's folder; it ends with agenthub:signInCompleted carrying `accountId`. */
export function agentAccountsStartBrowserSignIn(id: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentAccountsStartBrowserSignIn(id));
}

export function agentAccountsStartDeviceSignIn(id: string): Promise<SignInFlow> {
  return callBridge(() => AppService.AgentAccountsStartDeviceSignIn(id));
}

export function agentAccountPolicyGet(): Promise<AccountPolicy> {
  return callBridge(async () => policyOf(await AppService.AgentAccountPolicyGet()));
}

/**
 * Replaces the rules when `policy.revision` is the stored one. `acknowledgeRisk`
 * is for an explicit click on the terms warning and nothing else; a stored
 * acknowledgement is kept on later saves.
 */
export function agentAccountPolicySet(policy: AccountPolicy, acknowledgeRisk: boolean): Promise<AccountPolicy> {
  return callBridge(async () => policyOf(await AppService.AgentAccountPolicySet(rawPolicyOf(policy), acknowledgeRisk)));
}

export const AGENT_ACCOUNTS_CHANGED_EVENT = "agentaccounts:changed";
export const AGENT_ACCOUNTS_SWITCHED_EVENT = "agentaccounts:switched";

/**
 * An account was added, renamed, removed, signed in, marked limited, or its usage
 * was read. No payload. Readings seen in a session's traffic are announced at
 * most once every two seconds with no trailing event, so the newest one can go
 * unannounced: refetch on window focus as well.
 */
export function onAgentAccountsChanged(cb: () => void): () => void {
  return Events.On(AGENT_ACCOUNTS_CHANGED_EVENT, () => cb());
}

/** A session moved to another account at the start of a turn. `from` and `to` are account ids. */
export type AccountSwitchedEvent = { sessionId: string; from: string; to: string; reason: string };

/** Reads the event's payload; anything malformed is not a switch. */
export function parseAccountSwitched(data: unknown): AccountSwitchedEvent | null {
  const record = (Array.isArray(data) ? data[0] : data) as Record<string, unknown> | null | undefined;
  if (!record || typeof record !== "object") return null;
  if (typeof record.sessionId !== "string" || typeof record.from !== "string" || typeof record.to !== "string") return null;
  if (!record.sessionId || !record.to) return null;
  return {
    sessionId: record.sessionId,
    from: record.from,
    to: record.to,
    reason: typeof record.reason === "string" ? record.reason : "",
  };
}

/** Fires once per move that stood, after the new process opened. Returns unsubscribe. */
export function onAgentAccountSwitched(handler: (event: AccountSwitchedEvent) => void): () => void {
  return Events.On(AGENT_ACCOUNTS_SWITCHED_EVENT, (ev) => {
    const parsed = parseAccountSwitched(ev?.data);
    if (parsed) handler(parsed);
  });
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/bridge/agentAccounts.test.ts`

Expected: `7 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/bridge/agentAccounts.ts \
  frontend/src/bridge/agentAccounts.test.ts
git commit -m "bridge: agent accounts over the real bindings" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 2: The pure account model, and the notice types

Names, order, usage rows, relative time and the words for the transcript line and the toast, with no fetching, so Settings, the transcript and the summary card read an account the same way. Also adds the hand-typed `AccountNotice` type (`SessionUpdate.accountNotice` is typed in Go but is not in the generated bindings) to `types.ts`, because the model's `noticeText` takes it.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/accountModel.ts`
- Create: `frontend/src/modules/agenthub/accounts/accountModel.test.ts`
- Modify: `frontend/src/modules/agenthub/types.ts`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/accountModel.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AgentAccount } from "@/bridge/agentAccounts";
import {
  builtinAccountHelp,
  accountLabelFor,
  accountLabelOf,
  accountNotes,
  agoText,
  inWords,
  isBuiltinAccountId,
  movedOrder,
  moveId,
  noticeText,
  orderedAccounts,
  readingText,
  refreshNote,
  switchedToast,
  usageRows,
} from "./accountModel";

const NOW = Date.UTC(2026, 9, 23, 12, 0, 0);
const nowSeconds = NOW / 1000;

const account = (over: Partial<AgentAccount> = {}): AgentAccount => ({
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});
const builtin = account({ id: "claude-default", label: "System default", builtin: true });

describe("account names", () => {
  test("the built-in account is known by its id and by an empty one", () => {
    expect(isBuiltinAccountId("claude", "")).toBe(true);
    expect(isBuiltinAccountId("claude", "claude-default")).toBe(true);
    expect(isBuiltinAccountId("claude", "codex-default")).toBe(false);
  });

  test("a label comes from the list; the built-in one has fallbacks, a removed one has words", () => {
    expect(accountLabelOf([builtin, account()], "claude", "claude-work")).toBe("Work");
    expect(accountLabelOf(undefined, "claude", "claude-default")).toBe("System default");
    expect(accountLabelOf(undefined, "codex", "codex-default")).toBe("DevKit default");
    expect(accountLabelOf([builtin], "claude", "claude-default")).toBe("System default");
    expect(accountLabelOf(undefined, "claude", "claude-work")).toBe("another account");
    expect(accountLabelOf([builtin], "claude", "claude-gone")).toBe("a removed account");
  });

  test("the summary row is absent for an empty id and while the list that would name it is missing", () => {
    expect(accountLabelFor(undefined, "claude", "")).toBeUndefined();
    expect(accountLabelFor(undefined, "claude", "claude-work")).toBeUndefined();
    expect(accountLabelFor([builtin, account()], "claude", "claude-work")).toBe("Work");
    expect(accountLabelFor(undefined, "claude", "claude-default")).toBe("System default");
    expect(accountLabelFor([builtin], "claude", "claude-gone")).toBe("a removed account");
  });
});

describe("the built-in account", () => {
  test("is explained per vendor", () => {
    expect(builtinAccountHelp("claude")).toContain("already uses on this computer");
    expect(builtinAccountHelp("codex")).toContain("DevKit keeps for its own sessions");
  });
});

describe("order", () => {
  test("moveId swaps neighbours and stops at the ends", () => {
    expect(moveId(["a", "b", "c"], "b", -1)).toEqual(["b", "a", "c"]);
    expect(moveId(["a", "b", "c"], "b", 1)).toEqual(["a", "c", "b"]);
    expect(moveId(["a", "b", "c"], "a", -1)).toEqual(["a", "b", "c"]);
    expect(moveId(["a", "b", "c"], "c", 1)).toEqual(["a", "b", "c"]);
    expect(moveId(["a", "b"], "zzz", 1)).toEqual(["a", "b"]);
  });

  test("the ordered ids come first, a removed id is skipped, the rest keep their place", () => {
    const list = [builtin, account(), account({ id: "claude-spare", label: "Spare" })];
    expect(orderedAccounts(list, ["claude-spare", "claude-gone", "claude-spare"]).map((a) => a.id)).toEqual([
      "claude-spare",
      "claude-default",
      "claude-work",
    ]);
    expect(orderedAccounts(list, []).map((a) => a.id)).toEqual(["claude-default", "claude-work", "claude-spare"]);
  });

  test("a move is written out in full from what is on screen", () => {
    const list = [builtin, account(), account({ id: "claude-spare", label: "Spare" })];
    expect(movedOrder(list, [], "claude-spare", -1)).toEqual(["claude-default", "claude-spare", "claude-work"]);
    // On screen: Work, System default, Spare. System default moves above Work.
    expect(movedOrder(list, ["claude-work", "claude-gone"], "claude-default", -1)).toEqual([
      "claude-default",
      "claude-work",
      "claude-spare",
    ]);
  });
});

describe("times", () => {
  test("inWords and agoText round to the unit a person reads", () => {
    expect(inWords(nowSeconds + 12 * 60, NOW)).toBe("in 12m");
    expect(inWords(nowSeconds + 3 * 3600, NOW)).toBe("in 3h");
    expect(inWords(nowSeconds + 4 * 86_400, NOW)).toBe("in 4d");
    expect(inWords(nowSeconds - 100, NOW)).toBe("in 0m");
    expect(agoText(nowSeconds - 20, NOW)).toBe("just now");
    expect(agoText(nowSeconds - 3 * 60, NOW)).toBe("3m ago");
    expect(agoText(nowSeconds - 5 * 3600, NOW)).toBe("5h ago");
    expect(agoText(nowSeconds - 4 * 86_400, NOW)).toBe("4d ago");
    expect(agoText(nowSeconds + 50, NOW)).toBe("just now");
  });
});

describe("usage", () => {
  test("a window is a percentage, its reset and a tone from the vendor's status", () => {
    const rows = usageRows(
      account({
        usage: [
          { label: "5-hour", usedPercent: 41.6, resetsAt: nowSeconds + 2 * 3600, status: "ok" },
          { label: "Weekly", usedPercent: 83, resetsAt: nowSeconds + 3 * 86_400, status: "warning" },
          { label: "Opus", usedPercent: 100, resetsAt: nowSeconds + 600, status: "reached" },
          { label: "Other", usedPercent: 12, resetsAt: 0, status: "" },
        ],
      }),
      NOW,
    );
    expect(rows.map((r) => [r.label, r.percent, r.text, r.tone])).toEqual([
      ["5-hour", 42, "42% · resets in 2h", undefined],
      ["Weekly", 83, "83% · resets in 3d", "warning"],
      ["Opus", 100, "100% · resets in 10m", "danger"],
      ["Other", 12, "12%", undefined],
    ]);
  });

  test("a window that reset since the reading is drawn empty and says so; junk is clamped", () => {
    const rows = usageRows(
      account({
        usage: [
          { label: "5-hour", usedPercent: 97, resetsAt: nowSeconds - 60, status: "reached" },
          { label: "Weekly", usedPercent: 140, resetsAt: nowSeconds + 3600, status: "" },
        ],
      }),
      NOW,
    );
    expect(rows[0]).toMatchObject({ percent: 0, text: "reset since the last reading", tone: undefined });
    expect(rows[1].percent).toBe(100);
  });

  test("the reading says who saw it and how long ago", () => {
    expect(readingText(account(), NOW)).toBe("No usage reading yet");
    expect(readingText(account({ observedAt: nowSeconds - 180, usageSource: "checked" }), NOW)).toBe("Checked 3m ago");
    expect(readingText(account({ observedAt: nowSeconds - 7200, usageSource: "observed" }), NOW)).toBe("Observed 2h ago");
  });

  test("an unchanged reading time is the sign that nothing was asked", () => {
    expect(refreshNote({ observedAt: 5 }, { observedAt: 9 })).toBe("");
    expect(refreshNote({ observedAt: 5 }, { observedAt: 5 })).toContain("Nothing new");
  });
});

describe("what keeps an account out", () => {
  test("limited and cooling down each say so; past times say nothing", () => {
    expect(accountNotes(account({ exhaustedUntil: nowSeconds + 3 * 3600 }), NOW)).toEqual([
      { key: "limit", text: "Limit reached · available in 3h", tone: "danger" },
    ]);
    expect(accountNotes(account({ cooldownUntil: nowSeconds + 600 }), NOW)[0].text).toContain("retry in 10m");
    expect(accountNotes(account({ exhaustedUntil: nowSeconds - 5, cooldownUntil: nowSeconds - 5 }), NOW)).toEqual([]);
  });
});

describe("words for the notice and the toast", () => {
  test("a switch reads as a move, a failure leads with what did not happen", () => {
    expect(noticeText({ kind: "switch", from: "a", to: "b", reason: "5-hour 95% ≥ 90%" }, "System default", "Work")).toBe(
      "Switched from System default to Work · 5-hour 95% ≥ 90%",
    );
    expect(noticeText({ kind: "switch", from: "a", to: "b" }, "A", "B")).toBe("Switched from A to B");
    expect(noticeText({ kind: "failed", from: "a", to: "b", reason: "It could not be opened." }, "A", "B")).toBe(
      "Could not switch from A to B: It could not be opened.",
    );
  });

  test("the toast names where the session went and why", () => {
    expect(switchedToast("Work", "Weekly 95% ≥ 90%")).toEqual({ title: "Moved to Work", description: "Weekly 95% ≥ 90%" });
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountModel.test.ts`

Expected: `error: Cannot find module './accountModel'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/accountModel.ts`:

```ts
import type { AgentAccount } from "@/bridge/agentAccounts";
import { CLAUDE_VENDOR, CODEX_VENDOR } from "@/bridge/agenthub";
import type { AccountNotice } from "../types";
import { resetsIn } from "../summary/summaryModel";

/**
 * The pure half of "Accounts & limits": names, order, usage rows and the words
 * for them. Nothing here fetches, so the settings page, the transcript notice
 * and the summary card read accounts the same way.
 */

/** The agents that can have several accounts. opencode keeps one login. */
export const ACCOUNT_VENDORS: readonly string[] = [CLAUDE_VENDOR, CODEX_VENDOR];

/** The longest an account's name can be; the backend refuses more. */
export const ACCOUNT_LABEL_MAX = 40;

/** The id of a vendor's built-in account. A session that runs as it may carry "" instead. */
export function builtinAccountId(vendor: string): string {
  return `${vendor}-default`;
}

export function isBuiltinAccountId(vendor: string, id: string): boolean {
  return id === "" || id === builtinAccountId(vendor);
}

/** What the built-in account is, in a sentence, for the page that lists it. */
export function builtinAccountHelp(vendor: string): string {
  return vendor === CLAUDE_VENDOR
    ? "System default is the login Claude Code already uses on this computer."
    : "DevKit default is the Codex sign-in DevKit keeps for its own sessions.";
}

/** What the built-in account is called when the list that names it is not at hand. */
export function builtinAccountLabel(vendor: string): string {
  return vendor === CLAUDE_VENDOR ? "System default" : "DevKit default";
}

/**
 * The words for an account id in a sentence ("Switched from … to …"). Built-in
 * ids always resolve; another id waits for the list, and one the list no longer
 * holds was removed.
 */
export function accountLabelOf(accounts: readonly AgentAccount[] | undefined, vendor: string, id: string): string {
  if (isBuiltinAccountId(vendor, id)) return accounts?.find((a) => a.builtin)?.label || builtinAccountLabel(vendor);
  if (!accounts) return "another account";
  return accounts.find((a) => a.id === id)?.label || "a removed account";
}

/**
 * The summary card's Account row: absent for a session on the built-in account
 * that never moved (its id is empty) and while the list that would name the
 * account has not arrived, so the row never shows a guess.
 */
export function accountLabelFor(
  accounts: readonly AgentAccount[] | undefined,
  vendor: string,
  id: string,
): string | undefined {
  if (!id) return undefined;
  if (!isBuiltinAccountId(vendor, id) && !accounts) return undefined;
  return accountLabelOf(accounts, vendor, id);
}

/** Moves `id` one place; an id that is not there, or already at the end, leaves the order as it was. */
export function moveId(order: readonly string[], id: string, delta: -1 | 1): string[] {
  const from = order.indexOf(id);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= order.length) return [...order];
  const next = [...order];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/**
 * The accounts as the rules will try them: the ids the person ordered first (an
 * id of a removed account is skipped), then every other account in the order the
 * backend listed them, built-in first.
 */
export function orderedAccounts<T extends { id: string }>(accounts: readonly T[], order: readonly string[]): T[] {
  const byId = new Map(accounts.map((account) => [account.id, account] as const));
  const seen = new Set<string>();
  const out: T[] = [];
  for (const id of order) {
    const account = byId.get(id);
    if (account && !seen.has(id)) {
      seen.add(id);
      out.push(account);
    }
  }
  for (const account of accounts) if (!seen.has(account.id)) out.push(account);
  return out;
}

/** The order to save after moving one account, written out in full from what is on screen. */
export function movedOrder(accounts: readonly AgentAccount[], order: readonly string[], id: string, delta: -1 | 1): string[] {
  return moveId(orderedAccounts(accounts, order).map((account) => account.id), id, delta);
}

export type Tone = "ok" | "warning" | "danger";

/** "in 42m" / "in 3h" / "in 2d". */
export function inWords(atSeconds: number, nowMs: number): string {
  const minutes = Math.max(0, Math.round((atSeconds * 1000 - nowMs) / 60_000));
  if (minutes < 90) return `in ${minutes}m`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `in ${hours}h` : `in ${Math.round(hours / 24)}d`;
}

/** "just now" / "3m ago" / "5h ago" / "4d ago", for a Unix-seconds time. */
export function agoText(atSeconds: number, nowMs: number): string {
  const seconds = Math.max(0, nowMs / 1000 - atSeconds);
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 90) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

export type UsageRow = {
  key: string;
  label: string;
  /** 0–100, what the meter fills to. */
  percent: number;
  text: string;
  tone?: Tone;
};

/**
 * One row per window the vendor reported. A window whose reset time has passed
 * since the reading starts over, so it is drawn empty and says so; a reading with
 * no reset time stays as it was seen.
 */
export function usageRows(account: Pick<AgentAccount, "usage">, nowMs: number): UsageRow[] {
  return account.usage.map((window, index) => {
    const reset = window.resetsAt > 0 && window.resetsAt * 1000 <= nowMs;
    const percent = reset ? 0 : Math.min(100, Math.max(0, Math.round(window.usedPercent)));
    const parts = [reset ? "reset since the last reading" : `${percent}%`];
    if (!reset && window.resetsAt > 0) parts.push(resetsIn(window.resetsAt, nowMs));
    return {
      key: `${index}:${window.label}`,
      label: window.label,
      percent,
      text: parts.join(" · "),
      tone: reset ? undefined : window.status === "reached" ? "danger" : window.status === "warning" ? "warning" : undefined,
    };
  });
}

/** "Observed 3m ago", "Checked 3m ago", or that there is no reading yet. */
export function readingText(account: Pick<AgentAccount, "observedAt" | "usageSource">, nowMs: number): string {
  if (!account.observedAt) return "No usage reading yet";
  const verb = account.usageSource === "checked" ? "Checked" : "Observed";
  return `${verb} ${agoText(account.observedAt, nowMs)}`;
}

export type AccountNote = { key: string; text: string; tone?: Tone };

/**
 * What keeps a signed-in account from being used right now. A time at or before
 * now is a past value nothing cleared, so it says nothing.
 */
export function accountNotes(
  account: Pick<AgentAccount, "exhaustedUntil" | "cooldownUntil">,
  nowMs: number,
): AccountNote[] {
  const notes: AccountNote[] = [];
  if (account.exhaustedUntil * 1000 > nowMs) {
    notes.push({ key: "limit", text: `Limit reached · available ${inWords(account.exhaustedUntil, nowMs)}`, tone: "danger" });
  }
  if (account.cooldownUntil * 1000 > nowMs) {
    notes.push({
      key: "cooldown",
      text: `Left out for now: it could not be opened · retry ${inWords(account.cooldownUntil, nowMs)}`,
      tone: "warning",
    });
  }
  return notes;
}

/**
 * How a Refresh usage ended. The backend answers with what it already has when
 * asked too soon, so the only sign that nothing was read is an unchanged time.
 */
export function refreshNote(before: Pick<AgentAccount, "observedAt">, after: Pick<AgentAccount, "observedAt">): string {
  return after.observedAt !== before.observedAt
    ? ""
    : "Nothing new: DevKit asks the vendor at most every few seconds, and this reading is already the latest.";
}

/** The one line a transcript notice says. */
export function noticeText(notice: AccountNotice, fromLabel: string, toLabel: string): string {
  if (notice.kind === "failed") {
    return `Could not switch from ${fromLabel} to ${toLabel}${notice.reason ? `: ${notice.reason}` : ""}`;
  }
  return `Switched from ${fromLabel} to ${toLabel}${notice.reason ? ` · ${notice.reason}` : ""}`;
}

/** The toast when a session moves. */
export function switchedToast(toLabel: string, reason: string): { title: string; description: string } {
  return { title: `Moved to ${toLabel}`, description: reason };
}
```

In `frontend/src/modules/agenthub/types.ts`, replace:

```ts
/**
 * What a session is working towards, from the goal extension.
 * Mirrors internal/agenthub/update.go: SessionGoal
 */
```

with:

```ts
/**
 * A move between vendor accounts, on a session_info_update's _meta.devkit.
 * `kind` is "switch" (a move that stands) or "failed" (one that was tried and
 * undone). `from` and `to` are account ids; the built-in account's is
 * "<vendor>-default". Mirrors internal/agenthub/update.go: AccountNotice, which
 * the generated bindings do not carry.
 */
export type AccountNotice = {
  kind: "switch" | "failed";
  from: string;
  to: string;
  reason?: string;
};

/**
 * What a session is working towards, from the goal extension.
 * Mirrors internal/agenthub/update.go: SessionGoal
 */
```

In `frontend/src/modules/agenthub/types.ts`, replace:

```ts
  /** The goal was removed — which `goal: null` alone cannot say, since most
   * session_info_updates carry no goal at all. */
  goalCleared?: boolean;
```

with:

```ts
  /** The goal was removed — which `goal: null` alone cannot say, since most
   * session_info_updates carry no goal at all. */
  goalCleared?: boolean;
  /** The session moved to another account, or tried to: a line in the transcript, not session state. */
  accountNotice?: AccountNotice | null;
```

In `frontend/src/modules/agenthub/types.ts`, replace:

```ts
  /** One row per background task, merged from the three async_task_* kinds. */
  asyncTask?: AsyncTask;
  raw?: unknown;
  createdAt?: string;
```

with:

```ts
  /** One row per background task, merged from the three async_task_* kinds. */
  asyncTask?: AsyncTask;
  /** Set on a devkit_account_notice row: the session moved to another account. */
  accountNotice?: AccountNotice;
  raw?: unknown;
  createdAt?: string;
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountModel.test.ts`

Expected: `15 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/accountModel.ts \
  frontend/src/modules/agenthub/accounts/accountModel.test.ts \
  frontend/src/modules/agenthub/types.ts
git commit -m "agenthub: the pure account model and the AccountNotice type" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 3: The pure policy model

The switching rules as the form edits them: defaults where the store holds a zero, validation that mirrors `internal/preferences/accountpolicy.go` (50-100 percent, 60-3600 seconds, at most 32 order ids, three refusal choices), the refusal sentences, and the two pieces of copy. The sentences follow `subagentLimits.ts`.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/accountPolicy.ts`
- Create: `frontend/src/modules/agenthub/accounts/accountPolicy.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/accountPolicy.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AccountPolicy } from "@/bridge/agentAccounts";
import {
  fieldsDiffer,
  fieldsOf,
  needsRiskAcknowledgement,
  parseFields,
  refusalOf,
  thresholdMeaning,
  vendorPolicyOf,
  withVendorPolicy,
} from "./accountPolicy";

const policy: AccountPolicy = {
  revision: 4,
  riskAcknowledgedAt: "2026-10-23T08:00:00Z",
  vendors: {
    claude: {
      enabled: true,
      thresholdPercent: 80,
      weeklyThresholdPercent: 95,
      usageMaxAgeSeconds: 600,
      order: ["claude-work"],
      onLimitRefusal: "continue",
    },
  },
};

describe("vendorPolicyOf", () => {
  test("nothing stored is off with the defaults", () => {
    expect(vendorPolicyOf(undefined, "codex")).toEqual({
      enabled: false,
      thresholdPercent: 90,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: [],
      onLimitRefusal: "none",
    });
    expect(vendorPolicyOf(policy, "codex").enabled).toBe(false);
  });

  test("a stored zero is the default, and a stored vendor is read as it is", () => {
    const zeros: AccountPolicy = {
      ...policy,
      vendors: { claude: { ...policy.vendors.claude, thresholdPercent: 0, usageMaxAgeSeconds: 0, onLimitRefusal: "" } },
    };
    expect(vendorPolicyOf(zeros, "claude")).toMatchObject({ thresholdPercent: 90, usageMaxAgeSeconds: 300, onLimitRefusal: "none" });
    expect(vendorPolicyOf(policy, "claude")).toMatchObject({ enabled: true, thresholdPercent: 80, order: ["claude-work"] });
  });

  test("an unknown refusal choice is the default one", () => {
    expect(refusalOf("resendIfNoProgress")).toBe("resendIfNoProgress");
    expect(refusalOf("surprise")).toBe("none");
  });
});

describe("parseFields", () => {
  const fields = fieldsOf(vendorPolicyOf(policy, "claude"));

  test("what is stored round-trips, an empty weekly is 0", () => {
    expect(fields).toEqual({ enabled: true, threshold: "80", weekly: "95", maxAge: "600", refusal: "continue" });
    expect(parseFields(fields)).toEqual({
      ok: true,
      value: { enabled: true, thresholdPercent: 80, weeklyThresholdPercent: 95, usageMaxAgeSeconds: 600, onLimitRefusal: "continue" },
    });
    const parsed = parseFields({ ...fields, weekly: " " });
    expect(parsed.ok && parsed.value.weeklyThresholdPercent).toBe(0);
  });

  test("the bounds are the backend's: 50 to 100 percent, 60 to 3600 seconds", () => {
    expect(parseFields({ ...fields, threshold: "50" }).ok).toBe(true);
    expect(parseFields({ ...fields, threshold: "100" }).ok).toBe(true);
    for (const bad of ["49", "101", "", "ninety", "90.5", "-90", "9e1"]) {
      const parsed = parseFields({ ...fields, threshold: bad });
      expect(parsed.ok).toBe(false);
      if (!parsed.ok) {
        expect(parsed.field).toBe("threshold");
        expect(parsed.problem).toContain("50 to 100");
      }
    }
    expect(parseFields({ ...fields, maxAge: "60" }).ok).toBe(true);
    expect(parseFields({ ...fields, maxAge: "3600" }).ok).toBe(true);
    for (const bad of ["59", "3601", "", "5 min"]) {
      const parsed = parseFields({ ...fields, maxAge: bad });
      expect(parsed.ok).toBe(false);
      if (!parsed.ok) expect(parsed.problem).toContain("60 to 3600");
    }
    const weekly = parseFields({ ...fields, weekly: "20" });
    expect(weekly.ok).toBe(false);
    if (!weekly.ok) expect(weekly.field).toBe("weekly");
  });
});

describe("thresholdMeaning", () => {
  const fields = fieldsOf(vendorPolicyOf(policy, "claude"));
  test("says what the thresholds do, with the weekly one only when it differs", () => {
    expect(thresholdMeaning(parseFields({ ...fields, weekly: "" }))).toBe(
      "A session moves to the next signed-in account at its next turn once its account passes 80% of a window.",
    );
    expect(thresholdMeaning(parseFields(fields))).toContain("or 95% of a weekly one");
    expect(thresholdMeaning(parseFields({ ...fields, threshold: "x" }))).toBe("");
  });
});

describe("saving", () => {
  test("replaces one vendor's rules and nothing else", () => {
    const next = withVendorPolicy(policy, "codex", {
      enabled: false,
      thresholdPercent: 90,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: ["codex-default"],
      onLimitRefusal: "none",
    });
    expect(next.revision).toBe(4);
    expect(next.riskAcknowledgedAt).toBe("2026-10-23T08:00:00Z");
    expect(next.vendors.claude).toEqual(policy.vendors.claude);
    expect(next.vendors.codex.order).toEqual(["codex-default"]);
  });

  test("the order is capped where the backend caps it", () => {
    const many = Array.from({ length: 40 }, (_, i) => `a${i}`);
    expect(withVendorPolicy(policy, "claude", { ...vendorPolicyOf(policy, "claude"), order: many }).vendors.claude.order).toHaveLength(32);
  });

  test("the terms warning is owed until one was accepted", () => {
    expect(needsRiskAcknowledgement(undefined)).toBe(true);
    expect(needsRiskAcknowledgement({ riskAcknowledgedAt: "" })).toBe(true);
    expect(needsRiskAcknowledgement({ riskAcknowledgedAt: " " })).toBe(true);
    expect(needsRiskAcknowledgement(policy)).toBe(false);
  });

  test("fields differ when a value was typed, not when whitespace was", () => {
    const base = fieldsOf(vendorPolicyOf(policy, "claude"));
    expect(fieldsDiffer(base, { ...base, threshold: " 80 " })).toBe(false);
    expect(fieldsDiffer(base, { ...base, threshold: "81" })).toBe(true);
    expect(fieldsDiffer(base, { ...base, enabled: false })).toBe(true);
    expect(fieldsDiffer(base, { ...base, refusal: "none" })).toBe(true);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountPolicy.test.ts`

Expected: `error: Cannot find module './accountPolicy'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/accountPolicy.ts`:

```ts
import type { AccountPolicy, VendorPolicy } from "@/bridge/agentAccounts";

/**
 * The switching rules, as the form edits them. The limits repeat what the
 * backend enforces (internal/preferences/accountpolicy.go) so a wrong value is
 * told in words before a save is refused, never instead of it.
 */

export const THRESHOLD_MIN = 50;
export const THRESHOLD_MAX = 100;
export const THRESHOLD_DEFAULT = 90;
export const MAX_AGE_MIN = 60;
export const MAX_AGE_MAX = 3600;
export const MAX_AGE_DEFAULT = 300;
/** The most account ids the order can hold. */
export const ORDER_MAX = 32;

export type Refusal = "none" | "resendIfNoProgress" | "continue";

/** What to do with a turn the vendor refused at a real limit, and what each choice does. */
export const REFUSAL_CHOICES: readonly { value: Refusal; label: string; detail: string }[] = [
  {
    value: "none",
    label: "Do not resend (default)",
    detail: "A turn the vendor refused stays refused. Your next message starts on the next account.",
  },
  {
    value: "resendIfNoProgress",
    label: "Resend if it had not started",
    detail:
      "If the refused turn had produced no text and run no tool, DevKit sends the same message once on the next account.",
  },
  {
    value: "continue",
    label: "Send a continue message",
    detail:
      "DevKit sends one fixed message asking the agent to continue on the next account, even if the refused turn had made progress.",
  },
];

export function refusalOf(value: string): Refusal {
  return value === "resendIfNoProgress" || value === "continue" ? value : "none";
}

/** A vendor's rules, with the defaults filled in where nothing is stored (zero means "the default"). */
export function vendorPolicyOf(policy: AccountPolicy | undefined, vendor: string): VendorPolicy {
  const stored = policy?.vendors[vendor];
  return {
    enabled: stored?.enabled ?? false,
    thresholdPercent: stored?.thresholdPercent || THRESHOLD_DEFAULT,
    weeklyThresholdPercent: stored?.weeklyThresholdPercent ?? 0,
    usageMaxAgeSeconds: stored?.usageMaxAgeSeconds || MAX_AGE_DEFAULT,
    order: [...(stored?.order ?? [])],
    onLimitRefusal: refusalOf(stored?.onLimitRefusal ?? ""),
  };
}

/** The form's fields: text for the numbers, so a half-typed value is not a number yet. */
export type PolicyFields = {
  enabled: boolean;
  threshold: string;
  /** Empty means "the same as the threshold". */
  weekly: string;
  maxAge: string;
  refusal: Refusal;
};

export function fieldsOf(policy: VendorPolicy): PolicyFields {
  return {
    enabled: policy.enabled,
    threshold: String(policy.thresholdPercent),
    weekly: policy.weeklyThresholdPercent ? String(policy.weeklyThresholdPercent) : "",
    maxAge: String(policy.usageMaxAgeSeconds),
    refusal: refusalOf(policy.onLimitRefusal),
  };
}

export type ParsedFields =
  | { ok: true; value: Omit<VendorPolicy, "order"> }
  | { ok: false; field: "threshold" | "weekly" | "maxAge"; problem: string };

const WHOLE = /^\d+$/;
const PERCENT_PROBLEM = `Use a whole number from ${THRESHOLD_MIN} to ${THRESHOLD_MAX}.`;

function inRange(text: string, min: number, max: number): number | null {
  const trimmed = text.trim();
  if (!WHOLE.test(trimmed)) return null;
  const value = Number(trimmed);
  return value >= min && value <= max ? value : null;
}

export function parseFields(fields: PolicyFields): ParsedFields {
  const threshold = inRange(fields.threshold, THRESHOLD_MIN, THRESHOLD_MAX);
  if (threshold === null) return { ok: false, field: "threshold", problem: PERCENT_PROBLEM };
  let weekly = 0;
  if (fields.weekly.trim() !== "") {
    const parsed = inRange(fields.weekly, THRESHOLD_MIN, THRESHOLD_MAX);
    if (parsed === null) return { ok: false, field: "weekly", problem: `${PERCENT_PROBLEM} Leave it empty to use the same as above.` };
    weekly = parsed;
  }
  const maxAge = inRange(fields.maxAge, MAX_AGE_MIN, MAX_AGE_MAX);
  if (maxAge === null) {
    return { ok: false, field: "maxAge", problem: `Use a whole number of seconds from ${MAX_AGE_MIN} to ${MAX_AGE_MAX}.` };
  }
  return {
    ok: true,
    value: {
      enabled: fields.enabled,
      thresholdPercent: threshold,
      weeklyThresholdPercent: weekly,
      usageMaxAgeSeconds: maxAge,
      onLimitRefusal: fields.refusal,
    },
  };
}

/** What the two thresholds mean for a session, in a sentence; nothing while a field is not a number yet. */
export function thresholdMeaning(parsed: ParsedFields): string {
  if (!parsed.ok) return "";
  const { thresholdPercent, weeklyThresholdPercent } = parsed.value;
  const base = `A session moves to the next signed-in account at its next turn once its account passes ${thresholdPercent}% of a window`;
  return weeklyThresholdPercent ? `${base}, or ${weeklyThresholdPercent}% of a weekly one.` : `${base}.`;
}

/** The whole preference with one vendor's rules replaced; the other vendors and the revision are untouched. */
export function withVendorPolicy(policy: AccountPolicy, vendor: string, next: VendorPolicy): AccountPolicy {
  return { ...policy, vendors: { ...policy.vendors, [vendor]: { ...next, order: next.order.slice(0, ORDER_MAX) } } };
}

/** Turning switching on is refused until the terms warning was accepted once. */
export function needsRiskAcknowledgement(policy: Pick<AccountPolicy, "riskAcknowledgedAt"> | undefined): boolean {
  return !policy?.riskAcknowledgedAt?.trim();
}

/** Whether the form differs from what is stored, field by field as the person wrote them. */
export function fieldsDiffer(a: PolicyFields, b: PolicyFields): boolean {
  return (
    a.enabled !== b.enabled ||
    a.threshold.trim() !== b.threshold.trim() ||
    a.weekly.trim() !== b.weekly.trim() ||
    a.maxAge.trim() !== b.maxAge.trim() ||
    a.refusal !== b.refusal
  );
}

/** The one-line truth shown beside the switch and in the terms dialog. */
export const SWITCHING_TRUTH =
  "Switching happens only at the start of a new turn: a turn that is already running is never moved. Each account signs in through the agent's own login, in its own folder, so DevKit never holds a token.";

/** The terms warning. Short and factual: DevKit makes no claim that any of this is permitted. */
export const TERMS_WARNING =
  "Some providers' terms restrict how subscription logins may be used, including holding several accounts or sharing one login. DevKit cannot tell what yours allow: use only accounts you are entitled to use, and check each provider's terms and your plan's.";
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountPolicy.test.ts`

Expected: `10 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/accountPolicy.ts \
  frontend/src/modules/agenthub/accounts/accountPolicy.test.ts
git commit -m "agenthub: the switching rules' defaults, validation and copy" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 4: Query keys, hooks and the event/focus watcher

One place for the keys (`["agenthub","accounts",vendor]`, `["agenthub","accountPolicy"]`), the two read hooks, the watcher that refetches on `agentaccounts:changed` and on window focus (observed-usage events are throttled leading-edge, so the newest reading can go unannounced), and a `useNow` that moves relative times on once a minute. The watcher is mounted once, in Task 12; nothing polls.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/useAgentAccounts.ts`
- Create: `frontend/src/modules/agenthub/accounts/useAgentAccounts.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/useAgentAccounts.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { AGENT_ACCOUNTS_KEY, agentAccountsKey, watchAgentAccounts } from "./useAgentAccounts";

function fakes() {
  const invalidated: unknown[] = [];
  const client = { invalidateQueries: (filter: unknown) => (invalidated.push(filter), Promise.resolve()) };
  let changed: () => void = () => {};
  let unsubscribed = 0;
  const subscribe = (cb: () => void) => {
    changed = cb;
    return () => void (unsubscribed += 1);
  };
  const listeners = new Map<string, () => void>();
  const focus = {
    addEventListener: (type: string, listener: () => void) => void listeners.set(type, listener),
    removeEventListener: (type: string) => void listeners.delete(type),
  } as unknown as Pick<Window, "addEventListener" | "removeEventListener">;
  return { client, invalidated, subscribe, focus, listeners, fire: () => changed(), unsubscribed: () => unsubscribed };
}

describe("account query keys", () => {
  test("a vendor's list sits under the key every list is invalidated by", () => {
    expect(agentAccountsKey("claude")).toEqual([...AGENT_ACCOUNTS_KEY, "claude"]);
  });
});

describe("watchAgentAccounts", () => {
  test("a change event and a window focus both refetch every list", () => {
    const f = fakes();
    watchAgentAccounts(f.client, f.subscribe, f.focus);
    f.fire();
    f.listeners.get("focus")?.();
    expect(f.invalidated).toEqual([{ queryKey: AGENT_ACCOUNTS_KEY }, { queryKey: AGENT_ACCOUNTS_KEY }]);
  });

  test("stopping removes both listeners", () => {
    const f = fakes();
    const stop = watchAgentAccounts(f.client, f.subscribe, f.focus);
    expect(f.listeners.has("focus")).toBe(true);
    stop();
    expect(f.listeners.has("focus")).toBe(false);
    expect(f.unsubscribed()).toBe(1);
  });

  test("with no window to listen on it still follows the event", () => {
    const f = fakes();
    watchAgentAccounts(f.client, f.subscribe, null);
    f.fire();
    expect(f.invalidated).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/useAgentAccounts.test.ts`

Expected: `error: Cannot find module './useAgentAccounts'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/useAgentAccounts.ts`:

```ts
import { useEffect, useState } from "react";
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { agentAccountPolicyGet, agentAccountsList, onAgentAccountsChanged } from "@/bridge/agentAccounts";

/** Every vendor's account list; one entry per vendor under it. */
export const AGENT_ACCOUNTS_KEY = ["agenthub", "accounts"] as const;
export const agentAccountsKey = (vendor: string) => [...AGENT_ACCOUNTS_KEY, vendor] as const;
export const AGENT_ACCOUNT_POLICY_KEY = ["agenthub", "accountPolicy"] as const;

/**
 * A vendor's accounts, its built-in one first. Kept fresh by events, never by
 * polling: `watchAgentAccounts` (mounted once, by AgentAccountsSubscriber)
 * invalidates this key when Go says something changed and when the window
 * regains focus.
 */
export function useAgentAccounts(vendor: string, enabled = true) {
  return useQuery({
    queryKey: agentAccountsKey(vendor),
    queryFn: () => agentAccountsList(vendor),
    enabled: enabled && vendor !== "",
    staleTime: 30_000,
  });
}

/** The switching rules and their revision (what a save has to quote). */
export function useAgentAccountPolicy() {
  return useQuery({ queryKey: AGENT_ACCOUNT_POLICY_KEY, queryFn: agentAccountPolicyGet });
}

type FocusTarget = Pick<Window, "addEventListener" | "removeEventListener">;

/**
 * Refetch the lists when `agentaccounts:changed` fires and when the window gets
 * focus. Both are needed: readings seen in a session's traffic are announced at
 * most once every two seconds with no trailing event, so the newest reading can
 * go unannounced. `subscribe` and `focus` are the test seams. Returns unsubscribe.
 */
export function watchAgentAccounts(
  client: Pick<QueryClient, "invalidateQueries">,
  subscribe: (cb: () => void) => () => void = onAgentAccountsChanged,
  focus: FocusTarget | null = typeof window === "undefined" ? null : window,
): () => void {
  const refetch = () => {
    void client.invalidateQueries({ queryKey: AGENT_ACCOUNTS_KEY });
  };
  const off = subscribe(refetch);
  focus?.addEventListener("focus", refetch);
  return () => {
    off();
    focus?.removeEventListener("focus", refetch);
  };
}

/** The current time, moved on once a minute so "3m ago" and "resets in 2h" do not stand still on an open page. */
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/useAgentAccounts.test.ts`

Expected: `4 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/useAgentAccounts.ts \
  frontend/src/modules/agenthub/accounts/useAgentAccounts.test.ts
git commit -m "agenthub: account query keys, hooks and the change watcher" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 5: agenthub:signInCompleted carries accountId; account-aware sign-in

Plan 2's Go now emits `accountId` on `agenthub:signInCompleted` for a flow that signs in an added account (`""` for the built-in sign-in). Every handler that treated the event as "the built-in Codex signed in" must ignore a non-empty `accountId`: `createSignInTracker` (used by `useCodexSignIn`, which `CodexAccountSection` on the Runtimes page uses), and `AgentHubView`'s config-catalog refresh. The same two hooks gain an optional `accountId` to start the account's own flow (`AgentAccounts*` instead of `AgentHub*`), and the two sign-in panels gain a `note` prop for the line under the buttons. Existing behaviour with no `accountId` is unchanged.

**Files:**
- Create: `frontend/src/modules/agenthub/ClaudeSignIn.test.tsx`
- Modify: `frontend/src/bridge/agentSignIn.ts`
- Modify: `frontend/src/modules/agenthub/CodexSignIn.tsx`
- Modify: `frontend/src/modules/agenthub/ClaudeSignIn.tsx`
- Modify: `frontend/src/modules/agenthub/AgentHubView.tsx`
- Modify: `frontend/src/bridge/agentSignIn.test.ts`
- Modify: `frontend/src/modules/agenthub/CodexSignIn.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/ClaudeSignIn.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ClaudeSignIn, type ClaudeSignInController } from "./ClaudeSignIn";

const controller = (over: Partial<Record<string, unknown>> = {}) =>
  ({
    methods: { isPending: false, isError: false, data: [{ id: "claude-ai-login", label: "", description: "" }] },
    running: "",
    outcome: null,
    startError: "",
    start: () => {},
    ...over,
  }) as unknown as ClaudeSignInController;

describe("ClaudeSignIn", () => {
  it("offers the agent's own login and says it is the machine's own", () => {
    const html = renderToString(<ClaudeSignIn signIn={controller()} />);
    expect(html).toContain("Sign in with Claude subscription");
    expect(html).toContain("ANTHROPIC_API_KEY");
  });

  it("an added account says where its login goes instead", () => {
    const html = renderToString(
      <ClaudeSignIn signIn={controller()} note="Signs in to this account's own folder; your ~/.claude login is left alone." />,
    );
    expect(html).toContain("Sign in with Claude subscription");
    expect(html).toContain("this account&#x27;s own folder");
    expect(html).not.toContain("ANTHROPIC_API_KEY");
  });

  it("says how the login ended", () => {
    expect(renderToString(<ClaudeSignIn signIn={controller({ outcome: { signedIn: true, message: "Signed in." } })} />)).toContain(
      "Signed in.",
    );
    expect(renderToString(<ClaudeSignIn signIn={controller({ startError: "Claude is not installed." })} />)).toContain(
      "Claude is not installed.",
    );
  });
});
```

In `frontend/src/bridge/agentSignIn.test.ts`, replace:

```ts
    expect(parseSignInResult({ agent: "codex", loginId: "l1", success: true, error: "" })).toEqual({
      agent: "codex",
      loginId: "l1",
      success: true,
      error: "",
    });
  });
```

with:

```ts
    expect(parseSignInResult({ agent: "codex", loginId: "l1", success: true, error: "" })).toEqual({
      agent: "codex",
      loginId: "l1",
      success: true,
      error: "",
      accountId: "",
    });
  });

  test("says which added account finished, and nothing for the built-in sign-in", () => {
    expect(parseSignInResult({ agent: "codex", loginId: "l9", success: true, accountId: "codex-work" })?.accountId).toBe("codex-work");
    expect(parseSignInResult({ agent: "codex", loginId: "l9", success: true, accountId: 7 })?.accountId).toBe("");
  });
```

In `frontend/src/bridge/agentSignIn.test.ts`, replace:

```ts
      loginId: "l2",
      success: false,
      error: "Sign-in timed out.",
    });
```

with:

```ts
      loginId: "l2",
      success: false,
      error: "Sign-in timed out.",
      accountId: "",
    });
```

In `frontend/src/modules/agenthub/CodexSignIn.test.tsx`, replace:

```tsx
  it("other vendors are ignored, and cancel hands back the flow to withdraw", () => {
```

with:

```tsx
  it("an added account's sign-in is not the built-in one's news", () => {
    const hub = createSignInTracker();
    hub.started(flow("a"));
    const work = { ...result("w1", true), accountId: "codex-work" };
    expect(hub.event(work)).toBeNull();
    expect(hub.event({ ...result("w2", false, "Sign-in cancelled."), accountId: "codex-work" })).toBeNull();
    expect(hub.current?.loginId).toBe("a");
    expect(hub.event(result("a", true))?.success).toBe(true);
  });

  it("an account's tracker acts on its own account's events only", () => {
    const work = createSignInTracker("codex-work");
    expect(work.started(flow("w1"))).toBeNull();
    // The built-in flow and another account's flow are not this panel's.
    expect(work.event(result("a", true))).toBeNull();
    expect(work.event({ ...result("s1", true), accountId: "codex-spare" })).toBeNull();
    expect(work.current?.loginId).toBe("w1");
    expect(work.event({ ...result("w1", true), accountId: "codex-work" })?.success).toBe(true);
    expect(work.current).toBeNull();
  });

  it("an account's sign-in finished elsewhere lets the waiting panel go ahead", () => {
    const work = createSignInTracker("codex-work");
    work.started(flow("w1"));
    expect(work.event({ ...result("w1", false, "Replaced by a new sign-in."), accountId: "codex-work" })?.success).toBe(false);
    expect(work.event({ ...result("w2", true), accountId: "codex-work" })?.success).toBe(true);
  });

  it("other vendors are ignored, and cancel hands back the flow to withdraw", () => {
```

In `frontend/src/modules/agenthub/CodexSignIn.test.tsx`, replace:

```tsx
  it("says how it ended", () => {
```

with:

```tsx
  it("says its own line under the buttons when it is not the built-in sign-in", () => {
    expect(panel()).toContain("Codex&#x27;s own ChatGPT login");
    const html = panel({ note: "Signs in to this account's own folder." });
    expect(html).toContain("Signs in to this account&#x27;s own folder.");
    expect(html).not.toContain("your ~/.codex is");
  });

  it("says how it ended", () => {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/bridge/agentSignIn.test.ts src/modules/agenthub/CodexSignIn.test.tsx src/modules/agenthub/ClaudeSignIn.test.tsx`

Expected: failures in `agentSignIn.test.ts` (`accountId` is not on the parsed result), `CodexSignIn.test.tsx` (the built-in tracker acts on an added account's event; the `note` is not shown) and `ClaudeSignIn.test.tsx` (the note does not replace the default line).

- [ ] **Step 3: Implement**

In `frontend/src/bridge/agentSignIn.ts`, replace:

```ts
/** How a sign-in ended, from the agenthub:signInCompleted event. */
export type SignInResult = {
  agent: string;
  loginId: string;
  success: boolean;
  error: string;
};
```

with:

```ts
/**
 * How a sign-in ended, from the agenthub:signInCompleted event. `accountId` is
 * the added account the flow signed in to ("" for the built-in sign-in), and it
 * is not in the generated bindings: anything that acts on this event has to
 * check it, or an added account's sign-in is taken for the built-in one's.
 */
export type SignInResult = {
  agent: string;
  loginId: string;
  success: boolean;
  error: string;
  accountId?: string;
};
```

In `frontend/src/bridge/agentSignIn.ts`, replace:

```ts
    success: record.success === true,
    error: typeof record.error === "string" ? record.error : "",
  };
}
```

with:

```ts
    success: record.success === true,
    error: typeof record.error === "string" ? record.error : "",
    accountId: typeof record.accountId === "string" ? record.accountId : "",
  };
}
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
```

with:

```tsx
import { agentAccountsStartBrowserSignIn, agentAccountsStartDeviceSignIn } from "@/bridge/agentAccounts";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { AGENT_ACCOUNTS_KEY } from "./accounts/useAgentAccounts";
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
 * - An end that arrives before the start that made it has answered (Codex
 *   exiting at once): remembered, and taken when the start resolves, so the
 *   panel does not wait for an event that already came.
 */
export function createSignInTracker() {
```

with:

```tsx
 * - An end that arrives before the start that made it has answered (Codex
 *   exiting at once): remembered, and taken when the start resolves, so the
 *   panel does not wait for an event that already came.
 *
 * `accountId` is the added account this tracker signs in ("" for the built-in
 * sign-in). Every rule above holds only among events about that account: the
 * built-in panel ignores an added account's flow and the other way round.
 */
export function createSignInTracker(accountId = "") {
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
      if (result.agent !== CODEX_VENDOR) return null;
      if (current && result.loginId === current.loginId) {
```

with:

```tsx
      if (result.agent !== CODEX_VENDOR || (result.accountId ?? "") !== accountId) return null;
      if (current && result.loginId === current.loginId) {
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
 * The flow runs in the backend (a short-lived Codex in the jail, holding the
 * login page's callback port) and ends with an agenthub:signInCompleted event;
 * createSignInTracker decides which events count.
 */
export function useCodexSignIn(onDone?: (outcome: SignInOutcome) => void) {
```

with:

```tsx
 * The flow runs in the backend (a short-lived Codex in the jail, holding the
 * login page's callback port) and ends with an agenthub:signInCompleted event;
 * createSignInTracker decides which events count.
 *
 * With an `accountId` it signs in that added account instead (its own folder),
 * and finishing refreshes the accounts lists rather than the built-in status.
 * The id is fixed for the hook's life: give a panel for another account its own
 * mount (a `key`).
 */
export function useCodexSignIn(onDone?: (outcome: SignInOutcome) => void, accountId = "") {
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
  const [tracker] = useState(createSignInTracker);
```

with:

```tsx
  const [tracker] = useState(() => createSignInTracker(accountId));
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
    void client.invalidateQueries({ queryKey: agentSignInStatusQueryKey(CODEX_VENDOR) });
    onDoneRef.current?.(next);
```

with:

```tsx
    void client.invalidateQueries({
      queryKey: accountId ? AGENT_ACCOUNTS_KEY : agentSignInStatusQueryKey(CODEX_VENDOR),
    });
    onDoneRef.current?.(next);
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
      const next =
        method === "browser"
          ? await agentHubStartBrowserSignIn(CODEX_VENDOR)
          : await agentHubStartDeviceSignIn(CODEX_VENDOR);
```

with:

```tsx
      const browser = method === "browser";
      const next = accountId
        ? await (browser ? agentAccountsStartBrowserSignIn(accountId) : agentAccountsStartDeviceSignIn(accountId))
        : await (browser ? agentHubStartBrowserSignIn(CODEX_VENDOR) : agentHubStartDeviceSignIn(CODEX_VENDOR));
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
export function CodexSignIn({
  signIn,
  onStart,
}: {
  signIn: CodexSignInController;
  /** Called as a sign-in starts, e.g. to get a dialog out of its way. */
  onStart?: () => void;
}) {
  return (
    <CodexSignInPanel
```

with:

```tsx
export function CodexSignIn({
  signIn,
  onStart,
  note,
}: {
  signIn: CodexSignInController;
  /** Called as a sign-in starts, e.g. to get a dialog out of its way. */
  onStart?: () => void;
  /** Replaces the line under the buttons, for a sign-in that is not into DevKit's own Codex home. */
  note?: React.ReactNode;
}) {
  return (
    <CodexSignInPanel
      note={note}
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
export function CodexSignInPanel({
  flow,
  starting,
  outcome,
  startError,
  onStart,
  onCancel,
}: {
  flow: SignInFlow | null;
```

with:

```tsx
export function CodexSignInPanel({
  flow,
  starting,
  outcome,
  startError,
  onStart,
  onCancel,
  note,
}: {
  note?: React.ReactNode;
  flow: SignInFlow | null;
```

In `frontend/src/modules/agenthub/CodexSignIn.tsx`, replace:

```tsx
          <p className="text-caption-11 text-text-tertiary">
            Codex&apos;s own ChatGPT login, into a Codex home only DevKit uses — your ~/.codex is
            left alone. DevKit never sees your password.
          </p>
```

with:

```tsx
          <p className="text-caption-11 text-text-tertiary">
            {note ?? (
              <>
                Codex&apos;s own ChatGPT login, into a Codex home only DevKit uses — your ~/.codex is
                left alone. DevKit never sees your password.
              </>
            )}
          </p>
```

In `frontend/src/modules/agenthub/ClaudeSignIn.tsx`, replace:

```tsx
import {
  CLAUDE_VENDOR,
  agentHubAuthMethods,
```

with:

```tsx
import { agentAccountsStartSignIn } from "@/bridge/agentAccounts";
import {
  CLAUDE_VENDOR,
  agentHubAuthMethods,
```

In `frontend/src/modules/agenthub/ClaudeSignIn.tsx`, replace:

```tsx
 * `onDone` is read through a ref so it sees the render the exit arrives in,
 * not the one the button was pressed in.
 */
export function useClaudeSignIn(
  onDone?: (outcome: SignInOutcome) => void,
  vendor: string = CLAUDE_VENDOR,
  /**
   * Listing the methods can start the agent in its jail (the backend caches the
   * answer per install), so a vendor nobody is using yet leaves it off.
   */
  enabled = true,
) {
```

with:

```tsx
 * `onDone` is read through a ref so it sees the render the exit arrives in,
 * not the one the button was pressed in.
 *
 * With an `accountId` the login runs at that added account's folder instead
 * (AgentAccountsStartSignIn); the terminal tab and the exit are the same.
 */
export function useClaudeSignIn(
  onDone?: (outcome: SignInOutcome) => void,
  vendor: string = CLAUDE_VENDOR,
  /**
   * Listing the methods can start the agent in its jail (the backend caches the
   * answer per install), so a vendor nobody is using yet leaves it off.
   */
  enabled = true,
  accountId = "",
) {
```

In `frontend/src/modules/agenthub/ClaudeSignIn.tsx`, replace:

```tsx
      start: (rows, cols) =>
        agentHubStartSignIn(vendor, method.id, rows, cols).catch((e: unknown) => {
```

with:

```tsx
      start: (rows, cols) =>
        (accountId
          ? agentAccountsStartSignIn(accountId, method.id, rows, cols)
          : agentHubStartSignIn(vendor, method.id, rows, cols)
        ).catch((e: unknown) => {
```

In `frontend/src/modules/agenthub/ClaudeSignIn.tsx`, replace:

```tsx
export function ClaudeSignIn({
  signIn,
  onStart,
  vendor = CLAUDE_VENDOR,
}: {
  signIn: ClaudeSignInController;
  /** Called as the terminal opens, e.g. to get a dialog out of its way. */
  onStart?: () => void;
  vendor?: string;
}) {
```

with:

```tsx
export function ClaudeSignIn({
  signIn,
  onStart,
  vendor = CLAUDE_VENDOR,
  note,
}: {
  signIn: ClaudeSignInController;
  /** Called as the terminal opens, e.g. to get a dialog out of its way. */
  onStart?: () => void;
  vendor?: string;
  /** Replaces the line under the buttons, for a login that is not the machine's own. */
  note?: React.ReactNode;
}) {
```

In `frontend/src/modules/agenthub/ClaudeSignIn.tsx`, replace:

```tsx
        {running ? "Finish signing in in the terminal. " : ""}
        {vendor === CLAUDE_VENDOR ? (
```

with:

```tsx
        {running ? "Finish signing in in the terminal. " : ""}
        {note ? (
          note
        ) : vendor === CLAUDE_VENDOR ? (
```

In `frontend/src/modules/agenthub/AgentHubView.tsx`, replace:

```tsx
  useEffect(() => onAgentSignInCompleted(() => {
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "configCatalog"] });
```

with:

```tsx
  useEffect(() => onAgentSignInCompleted((result) => {
    // An added account's sign-in says nothing about the login a draft is about to use.
    if (result.accountId) return;
    void queryClient.invalidateQueries({ queryKey: ["agenthub", "configCatalog"] });
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/bridge/agentSignIn.test.ts src/modules/agenthub/CodexSignIn.test.tsx src/modules/agenthub/ClaudeSignIn.test.tsx`

Expected: `25 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/ClaudeSignIn.test.tsx \
  frontend/src/bridge/agentSignIn.ts \
  frontend/src/modules/agenthub/CodexSignIn.tsx \
  frontend/src/modules/agenthub/ClaudeSignIn.tsx \
  frontend/src/modules/agenthub/AgentHubView.tsx \
  frontend/src/bridge/agentSignIn.test.ts \
  frontend/src/modules/agenthub/CodexSignIn.test.tsx
git commit -m "agenthub: sign-in events carry accountId and the sign-in hooks take one" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 6: The accounts list (presentational)

Rows built from the Agents pages' own vocabulary (`rounded-control border border-border-subtle bg-surface-raised`, `text-body-13`, `text-caption-11`, `text-status-*` for how safe it is, `ActionTooltip` icon buttons, the summary card's meter). The editor takes everything as props (accounts, order, busy id, error, now, a `renderSignIn` slot and callbacks), so Task 8's container and the gallery both feed it. Status tones use `--status-*` only; no `--hue-*`.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/UsageMeter.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AddAccountForm.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AccountRow.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AccountsEditor.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AccountsEditor.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/AccountsEditor.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { AccountsEditor, type AccountsEditorProps } from "./AccountsEditor";

const NOW = Date.UTC(2026, 9, 23, 12, 0, 0);
const sec = NOW / 1000;

const account = (over: Partial<AgentAccount> = {}): AgentAccount => ({
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});
const builtin = account({ id: "claude-default", label: "System default", builtin: true });

const noop = async () => true;
const props = (over: Partial<AccountsEditorProps> = {}): AccountsEditorProps => ({
  vendor: "claude",
  vendorName: "Claude",
  accounts: [builtin],
  order: [],
  loadError: "",
  busyId: "",
  error: "",
  refreshNotes: {},
  now: NOW,
  renderSignIn: () => <div data-testid="sign-in-panel">Sign in</div>,
  onAdd: noop,
  onRename: noop,
  onRemove: noop,
  onSetShared: () => {},
  onRefreshUsage: () => {},
  onRefreshStatus: () => {},
  onMove: () => {},
  ...over,
});
const render = (over: Partial<AccountsEditorProps> = {}) =>
  renderToStaticMarkup(
    <TooltipProvider>
      <AccountsEditor {...props(over)} />
    </TooltipProvider>,
  );

const rows = (html: string) => html.match(/data-testid="agent-account-row"/g)?.length ?? 0;

describe("AccountsEditor", () => {
  test("a vendor with only its built-in account shows that and no way to reorder or remove it", () => {
    const html = render();
    expect(rows(html)).toBe(1);
    expect(html).toContain("System default");
    expect(html).toContain("Built in");
    expect(html).toContain("already uses on this computer");
    expect(html).not.toContain('aria-label="Move up"');
    expect(html).not.toContain('aria-label="Remove"');
    expect(html).not.toContain('aria-label="Rename"');
    expect(html).toContain("Add account");
  });

  test("accounts are listed in the order the rules try them, and the ends cannot move further", () => {
    const html = render({
      accounts: [builtin, account(), account({ id: "claude-spare", label: "Spare" })],
      order: ["claude-spare", "claude-work"],
    });
    expect(rows(html)).toBe(3);
    const at = (id: string) => html.indexOf(`data-account="${id}"`);
    expect(at("claude-spare")).toBeLessThan(at("claude-work"));
    expect(at("claude-work")).toBeLessThan(at("claude-default"));
    expect(html).toContain("tries them from the top");
    expect(html.match(/aria-label="Move up"/g)?.length).toBe(3);
    // The first row cannot go up and the last cannot go down.
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Move up"|aria-label="Move up"[^>]*disabled=""/g)?.length).toBe(1);
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Move down"|aria-label="Move down"[^>]*disabled=""/g)?.length).toBe(1);
  });

  test("a signed-in account shows its windows as meters, how old the reading is, and the lower bound of a shared login", () => {
    const html = render({
      accounts: [
        builtin,
        account({
          shared: true,
          observedAt: sec - 180,
          usageSource: "checked",
          usage: [
            { label: "5-hour", usedPercent: 83, resetsAt: sec + 2 * 3600, status: "warning" },
            { label: "Weekly", usedPercent: 40, resetsAt: sec + 3 * 86_400, status: "ok" },
          ],
        }),
      ],
    });
    expect(html.match(/role="meter"/g)?.length).toBe(2);
    expect(html).toContain('aria-valuenow="83"');
    expect(html).toContain("83% · resets in 2h");
    expect(html).toContain('data-tone="warning"');
    expect(html).toContain("Checked 3m ago");
    expect(html).toContain("a lower bound: other people use this login too");
    expect(html).toContain("Shared");
  });

  test("an account out of usage says until when; a past time says nothing", () => {
    const limited = render({ accounts: [builtin, account({ exhaustedUntil: sec + 3 * 3600 })] });
    expect(limited).toContain("Limit reached · available in 3h");
    const past = render({ accounts: [builtin, account({ exhaustedUntil: sec - 60, cooldownUntil: sec - 60 })] });
    expect(past).not.toContain("Limit reached");
    expect(past).not.toContain("Left out for now");
  });

  test("a signed-out account offers the sign-in and a way to check again, and draws no usage", () => {
    const html = render({
      accounts: [account({ signedIn: false, usage: [{ label: "5-hour", usedPercent: 10, resetsAt: 0, status: "" }] })],
    });
    expect(html).toContain("Not signed in");
    expect(html).toContain('data-testid="sign-in-panel"');
    expect(html).toContain("Check again");
    expect(html).not.toContain('role="meter"');
    expect(html).not.toContain('aria-label="Refresh usage"');
  });

  test("a refresh that read nothing says why, quietly", () => {
    const html = render({ accounts: [builtin, account()], refreshNotes: { "claude-work": "Nothing new: DevKit asks…" } });
    expect(html).toContain('role="status"');
    expect(html).toContain("Nothing new: DevKit asks…");
  });

  test("loading, a failed read and an empty list each say so", () => {
    expect(render({ accounts: undefined })).not.toContain('data-testid="agent-account-row"');
    expect(render({ accounts: undefined })).toContain("animate-pulse");
    const failed = render({ accounts: undefined, loadError: "Accounts are not available." });
    expect(failed).toContain('role="alert"');
    expect(failed).toContain("Accounts are not available.");
    expect(render({ accounts: [] })).toContain("No accounts yet.");
  });

  test("Add account opens a form with one field, and says the sign-in comes next", () => {
    expect(render()).not.toContain("<form");
    const html = render({ initialAddOpen: true });
    expect(html).toContain('aria-label="Add a Claude account"');
    expect(html).toContain("Account name");
    expect(html).toContain('maxLength="40"');
    expect(html).toContain("You sign in to the Claude account on the next step.");
    expect(html.match(/<input/g)?.length).toBeGreaterThanOrEqual(1);
    expect(render({ initialAddOpen: true, busyId: "add" })).toContain("Adding…");
  });

  test("the sentence Go refused with is shown as an alert", () => {
    const html = render({ accounts: [builtin, account()], error: "A session is using that account. Close it first." });
    expect(html).toContain('role="alert"');
    expect(html).toContain("A session is using that account. Close it first.");
  });

  test("an account being changed has its controls off", () => {
    const html = render({ accounts: [builtin, account()], busyId: "claude-work" });
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Remove"|aria-label="Remove"[^>]*disabled=""/g)?.length).toBe(1);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/AccountsEditor.test.tsx`

Expected: `error: Cannot find module './AccountsEditor'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/UsageMeter.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { Tone, UsageRow } from "./accountModel";

/** How safe a value is, in the chrome's status colours — the summary card's own mapping. */
export const TONE_TEXT: Record<Tone, string> = {
  ok: "text-status-success",
  warning: "text-status-warning",
  danger: "text-status-danger",
};

/**
 * One usage window: its name and what it says on one line, a meter under it. The
 * meter is the summary card's (role="meter", same bar, same transition); only the
 * fill is inline, because it is the one dynamic value.
 */
export function UsageMeter({ row }: { row: UsageRow }) {
  return (
    <div data-testid="account-usage-row" data-tone={row.tone} className="flex flex-col gap-1.5">
      <div className="flex items-baseline gap-2 text-body-12">
        <span className="shrink-0 text-text-secondary">{row.label}</span>
        <span className={cn("ml-auto min-w-0 truncate text-right tabular-nums", row.tone ? TONE_TEXT[row.tone] : "text-text-primary")}>
          {row.text}
        </span>
      </div>
      <div
        role="meter"
        aria-label={`${row.label} used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={row.percent}
        className="h-1 w-full overflow-hidden rounded-full bg-state-soft"
      >
        <div
          className="h-full w-full origin-left rounded-full bg-text-primary transition-transform duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none"
          style={{ transform: `scaleX(${row.percent / 100})` }}
        />
      </div>
    </div>
  );
}
```

Create `frontend/src/modules/agenthub/accounts/AddAccountForm.tsx`:

```tsx
import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ACCOUNT_LABEL_MAX } from "./accountModel";

/**
 * "Add account": a name, then the account exists with nothing signed in, and its
 * row offers the sign-in. Closed until asked for, like the model profiles form.
 */
export function AddAccountForm({
  vendorName,
  busy,
  onAdd,
  initiallyOpen = false,
  initialLabel = "",
}: {
  vendorName: string;
  busy: boolean;
  /** Resolves true when the account was added, so the form can close. */
  onAdd: (label: string) => Promise<boolean>;
  /** Opens with the form already showing (the gallery draws it so). */
  initiallyOpen?: boolean;
  initialLabel?: string;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  const [label, setLabel] = useState(initialLabel);
  const [problem, setProblem] = useState("");

  async function submit() {
    const name = label.trim();
    if (!name) {
      setProblem("Give the account a name.");
      return;
    }
    setProblem("");
    if (await onAdd(name)) {
      setLabel("");
      setOpen(false);
    }
  }

  if (!open) {
    return (
      <div className="flex items-center gap-3">
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setOpen(true)}>
          <Plus />
          Add account
        </Button>
      </div>
    );
  }

  return (
    <form
      aria-label={`Add a ${vendorName} account`}
      className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="w-72">
        <Input
          label="Account name"
          value={label}
          maxLength={ACCOUNT_LABEL_MAX}
          placeholder="Work"
          error={problem || undefined}
          helperText={`Only for you. You sign in to the ${vendorName} account on the next step.`}
          onChange={(event) => {
            setLabel(event.target.value);
            setProblem("");
          }}
        />
      </div>
      <div className="flex items-center gap-2">
        <Button type="submit" size="lg" disabled={busy}>
          {busy ? "Adding…" : "Add account"}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="lg"
          disabled={busy}
          onClick={() => {
            setOpen(false);
            setLabel("");
            setProblem("");
          }}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}
```

Create `frontend/src/modules/agenthub/accounts/AccountRow.tsx`:

```tsx
import { useState, type ReactNode } from "react";
import { Check, ChevronDown, ChevronUp, CircleDashed, Pencil, RefreshCw, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { ACCOUNT_LABEL_MAX, accountNotes, readingText, usageRows } from "./accountModel";
import { TONE_TEXT, UsageMeter } from "./UsageMeter";

export type AccountRowProps = {
  account: AgentAccount;
  /** Epoch milliseconds, passed in so the row reads the same in a test and in the gallery. */
  now: number;
  /** Where the account is in the order the rules try them, and how many there are. */
  index: number;
  count: number;
  /** A change to this account is in flight. */
  busy: boolean;
  /** Why the last Refresh usage changed nothing, when it did not. */
  refreshNote?: string;
  /** The sign-in controls, drawn while the account is signed out. */
  signIn: ReactNode;
  /** The Remove confirmation is open for this account. */
  removing: boolean;
  /** Why the last change failed; shown inside the Remove confirmation while it is open. */
  error: string;
  onMove: (delta: -1 | 1) => void;
  onRefreshUsage: () => void;
  onRefreshStatus: () => void;
  onSetShared: (shared: boolean) => void;
  /** Resolves true when the name was stored, so the form can close. */
  onRename: (label: string) => Promise<boolean>;
  onAskRemove: () => void;
  onCancelRemove: () => void;
  /** Resolves true when the account was removed. */
  onConfirmRemove: () => Promise<boolean>;
};

function RenameForm({
  initial,
  busy,
  onSave,
  onCancel,
}: {
  initial: string;
  busy: boolean;
  onSave: (label: string) => Promise<boolean>;
  onCancel: () => void;
}) {
  const [text, setText] = useState(initial);
  const [problem, setProblem] = useState("");
  async function submit() {
    const name = text.trim();
    if (!name) {
      setProblem("Give the account a name.");
      return;
    }
    if (await onSave(name)) onCancel();
  }
  return (
    <form
      aria-label={`Rename ${initial}`}
      className="flex items-end gap-2"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="w-60">
        <Input
          label="Name"
          value={text}
          maxLength={ACCOUNT_LABEL_MAX}
          error={problem || undefined}
          onChange={(event) => {
            setText(event.target.value);
            setProblem("");
          }}
        />
      </div>
      <Button type="submit" size="lg" disabled={busy || text.trim() === initial}>
        Save
      </Button>
      <Button type="button" variant="ghost" size="lg" disabled={busy} onClick={onCancel}>
        Cancel
      </Button>
    </form>
  );
}

/**
 * One account: who it is, whether it is signed in, how much of each window it
 * has used and how old that reading is, and what can be done to it. Everything
 * the account can say about itself is here; what the rules do with it is the
 * policy editor's.
 */
export function AccountRow({
  account,
  now,
  index,
  count,
  busy,
  refreshNote,
  signIn,
  removing,
  error,
  onMove,
  onRefreshUsage,
  onRefreshStatus,
  onSetShared,
  onRename,
  onAskRemove,
  onCancelRemove,
  onConfirmRemove,
}: AccountRowProps) {
  const [renaming, setRenaming] = useState(false);
  const notes = account.signedIn ? accountNotes(account, now) : [];
  const usage = account.signedIn ? usageRows(account, now) : [];
  return (
    <li
      data-testid="agent-account-row"
      data-account={account.id}
      data-signed-in={account.signedIn}
      className="flex flex-col gap-2.5 rounded-control border border-border-subtle bg-surface-raised px-3.5 py-2.5"
    >
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-body-13 font-medium text-text-primary">{account.label}</span>
        {account.builtin ? <Badge variant="outline">Built in</Badge> : null}
        {account.shared ? <Badge variant="secondary">Shared</Badge> : null}
        {count > 1 ? (
          <>
            <ActionTooltip label="Move up" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy || index === 0} onClick={() => onMove(-1)}>
                <ChevronUp />
              </Button>
            </ActionTooltip>
            <ActionTooltip label="Move down" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy || index === count - 1} onClick={() => onMove(1)}>
                <ChevronDown />
              </Button>
            </ActionTooltip>
          </>
        ) : null}
        {account.signedIn ? (
          <ActionTooltip label="Refresh usage" wrap>
            <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={onRefreshUsage}>
              <RefreshCw />
            </Button>
          </ActionTooltip>
        ) : null}
        {!account.builtin ? (
          <>
            <ActionTooltip label="Rename" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={() => setRenaming(true)}>
                <Pencil />
              </Button>
            </ActionTooltip>
            <ActionTooltip label="Remove" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={onAskRemove}>
                <Trash2 />
              </Button>
            </ActionTooltip>
          </>
        ) : null}
      </div>

      {renaming ? <RenameForm initial={account.label} busy={busy} onSave={onRename} onCancel={() => setRenaming(false)} /> : null}

      <div className="flex flex-wrap items-center gap-2">
        <span data-testid="agent-account-status" className="flex items-center gap-1.5 text-body-13 text-text-primary">
          {account.signedIn ? (
            <Check className="size-4 shrink-0 text-status-success" />
          ) : (
            <CircleDashed className="size-4 shrink-0 text-text-tertiary" />
          )}
          {account.signedIn ? "Signed in" : "Not signed in"}
        </span>
        {!account.signedIn ? (
          <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onRefreshStatus}>
            Check again
          </Button>
        ) : null}
      </div>

      {notes.map((note) => (
        <p key={note.key} data-testid="agent-account-note" className={cn("text-body-12", note.tone ? TONE_TEXT[note.tone] : "text-text-secondary")}>
          {note.text}
        </p>
      ))}

      {usage.length > 0 ? (
        <div className="flex flex-col gap-2.5">
          {usage.map((row) => (
            <UsageMeter key={row.key} row={row} />
          ))}
        </div>
      ) : null}

      {account.signedIn ? (
        <p data-testid="agent-account-reading" className="text-caption-11 text-text-tertiary">
          {readingText(account, now)}
          {account.shared ? " · a lower bound: other people use this login too" : ""}
        </p>
      ) : null}
      {refreshNote ? (
        <p className="text-caption-11 text-text-tertiary" role="status">
          {refreshNote}
        </p>
      ) : null}

      <label className="flex items-center gap-2.5 text-body-12 text-text-secondary">
        <span className="min-w-0 flex-1">Other people also use this login</span>
        <Switch
          aria-label={`Other people also use the ${account.label} login`}
          checked={account.shared}
          disabled={busy}
          onCheckedChange={onSetShared}
        />
      </label>

      {!account.signedIn ? signIn : null}

      <ConfirmDialog
        open={removing}
        onClose={onCancelRemove}
        title={`Remove ${account.label}?`}
        confirmLabel="Remove account"
        pendingLabel="Removing…"
        pending={busy}
        error={error || undefined}
        onConfirm={() => void onConfirmRemove()}
      >
        DevKit removes this account and its folder, including the login stored there. A session that is running as it must
        be closed first.
      </ConfirmDialog>
    </li>
  );
}
```

Create `frontend/src/modules/agenthub/accounts/AccountsEditor.tsx`:

```tsx
import { useState, type ReactNode } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { AccountRow } from "./AccountRow";
import { AddAccountForm } from "./AddAccountForm";
import { builtinAccountHelp, orderedAccounts } from "./accountModel";

export type AccountsEditorProps = {
  vendor: string;
  vendorName: string;
  /** Undefined until the first read answers. */
  accounts: readonly AgentAccount[] | undefined;
  /** The order the person saved; ids of removed accounts may be in it. */
  order: readonly string[];
  /** Why the list could not be read. */
  loadError: string;
  /** The id of the account a change is in flight for, or "add" while one is being added; "" when idle. */
  busyId: string;
  /** Why the last change failed. */
  error: string;
  /** Why the last Refresh usage of an account changed nothing, by account id. */
  refreshNotes: Readonly<Record<string, string>>;
  now: number;
  /** The sign-in controls for a signed-out account. */
  renderSignIn: (account: AgentAccount) => ReactNode;
  onAdd: (label: string) => Promise<boolean>;
  onRename: (id: string, label: string) => Promise<boolean>;
  onRemove: (id: string) => Promise<boolean>;
  onSetShared: (id: string, shared: boolean) => void;
  onRefreshUsage: (id: string) => void;
  onRefreshStatus: (id: string) => void;
  onMove: (id: string, delta: -1 | 1) => void;
  /** Opens with the Add account form already showing (the gallery draws it so). */
  initialAddOpen?: boolean;
};

/**
 * A vendor's accounts, in the order the switching rules try them. Presentational:
 * the container reads, changes and refreshes, so the gallery can draw every state
 * (`?gallery#agent-accounts`).
 */
export function AccountsEditor({
  vendor,
  vendorName,
  accounts,
  order,
  loadError,
  busyId,
  error,
  refreshNotes,
  now,
  renderSignIn,
  onAdd,
  onRename,
  onRemove,
  onSetShared,
  onRefreshUsage,
  onRefreshStatus,
  onMove,
  initialAddOpen,
}: AccountsEditorProps) {
  const [removingAsked, setRemovingId] = useState("");
  const list = accounts ? orderedAccounts(accounts, order) : [];
  // A removal can fail after the account is gone (its folder was left behind): the
  // list then no longer has the row the confirmation lives in, and the page shows the sentence.
  const removingId = list.some((account) => account.id === removingAsked) ? removingAsked : "";
  return (
    <section className="flex flex-col gap-3" aria-label={`${vendorName} accounts`}>
      <h3 className="text-body-14-medium text-text-primary">Accounts</h3>
      <p className="text-body-12 text-text-tertiary">
        A {vendorName} session runs as one of these accounts. {builtinAccountHelp(vendor)}
        {list.length > 1 ? " When a session has to move, DevKit tries them from the top." : ""}
      </p>

      {accounts === undefined && !loadError ? <Skeleton className="h-24 w-full rounded-control" /> : null}
      {loadError ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {loadError}
        </p>
      ) : null}
      {accounts !== undefined && list.length === 0 && !loadError ? (
        <p className="rounded-control border border-dashed border-border-subtle px-3.5 py-3 text-body-13 text-text-secondary">
          No accounts yet.
        </p>
      ) : null}

      {list.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label={`${vendorName} accounts`}>
          {list.map((account, index) => (
            <AccountRow
              key={account.id}
              account={account}
              now={now}
              index={index}
              count={list.length}
              busy={busyId === account.id}
              refreshNote={refreshNotes[account.id]}
              signIn={renderSignIn(account)}
              removing={removingId === account.id}
              error={error}
              onMove={(delta) => onMove(account.id, delta)}
              onRefreshUsage={() => onRefreshUsage(account.id)}
              onRefreshStatus={() => onRefreshStatus(account.id)}
              onSetShared={(shared) => onSetShared(account.id, shared)}
              onRename={(label) => onRename(account.id, label)}
              onAskRemove={() => setRemovingId(account.id)}
              onCancelRemove={() => setRemovingId("")}
              onConfirmRemove={async () => {
                const removed = await onRemove(account.id);
                if (removed) setRemovingId("");
                return removed;
              }}
            />
          ))}
        </ul>
      ) : null}

      {error && !removingId ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {error}
        </p>
      ) : null}

      <AddAccountForm vendorName={vendorName} busy={busyId === "add"} onAdd={onAdd} initiallyOpen={initialAddOpen} />

      <p className="text-body-12 text-text-tertiary">
        Mark an account as shared when other people also use that login. What DevKit sees of its usage is then a lower
        bound, so DevKit asks the vendor itself before a turn once its last check is older than the re-check time below.
      </p>
    </section>
  );
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/AccountsEditor.test.tsx`

Expected: `10 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/UsageMeter.tsx \
  frontend/src/modules/agenthub/accounts/AddAccountForm.tsx \
  frontend/src/modules/agenthub/accounts/AccountRow.tsx \
  frontend/src/modules/agenthub/accounts/AccountsEditor.tsx \
  frontend/src/modules/agenthub/accounts/AccountsEditor.test.tsx
git commit -m "agenthub: the accounts list for Settings" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 7: The switching form and the terms dialog (presentational)

A copy of `SubagentLimitsEditor`'s shape: state for the text fields, a `useEffect` keyed on a primitive (`JSON.stringify(stored)`, because the container builds a new policy object every render), `onSave` resolving true when stored, error and saved lines. The terms dialog is `AlertDialog` + `Checkbox` like `AgentRuntimeSetting`'s confirmation; its body is its own component and the dialog takes `portal` so a test can render it in place.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/AccountRiskDialog.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AccountPolicyEditor.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AccountPolicyEditor.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/AccountPolicyEditor.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { VendorPolicy } from "@/bridge/agentAccounts";
import { AccountPolicyEditor } from "./AccountPolicyEditor";
import { AccountRiskBody, AccountRiskDialog } from "./AccountRiskDialog";

const policy: VendorPolicy = {
  enabled: false,
  thresholdPercent: 90,
  weeklyThresholdPercent: 0,
  usageMaxAgeSeconds: 300,
  order: ["claude-work"],
  onLimitRefusal: "none",
};

type Props = Parameters<typeof AccountPolicyEditor>[0];
const render = (over: Partial<Props> = {}) =>
  renderToStaticMarkup(
    <AccountPolicyEditor
      vendorName="Claude"
      policy={policy}
      signedInCount={2}
      riskAcknowledged
      saving={false}
      error=""
      onSave={async () => true}
      {...over}
    />,
  );

describe("AccountPolicyEditor", () => {
  test("starts off, with the defaults in the fields and the truth about when switching happens", () => {
    const html = render();
    expect(html).toContain("Switching");
    expect(html).toContain("only at the start of a new turn");
    expect(html).toContain("DevKit never holds a token");
    expect(html).toContain('aria-checked="false"');
    expect(html).toContain('value="90"');
    expect(html).toContain('value="300"');
    expect(html.match(/<input[^>]*type="number"/g)?.length).toBe(3);
    expect(html).toContain('min="50"');
    expect(html).toContain('max="100"');
    expect(html).toContain('min="60"');
    expect(html).toContain('max="3600"');
    expect(html).toContain("at its next turn once its account passes 90% of a window.");
  });

  test("the refusal choices are plain, the default is first and the chosen one is explained", () => {
    const html = render();
    expect(html).toContain("Do not resend (default)");
    expect(html.indexOf("Do not resend (default)")).toBeLessThan(html.indexOf("Resend if it had not started"));
    expect(html).toContain("Send a continue message");
    expect(html).toContain("Your next message starts on the next account.");
    const resend = render({ policy: { ...policy, onLimitRefusal: "resendIfNoProgress" } });
    expect(resend).toMatch(/<option[^>]*value="resendIfNoProgress" selected=""/);
    expect(resend).toContain("sends the same message once on the next account");
  });

  test("Save is off until something differs", () => {
    expect(render()).toMatch(/<button[^>]*disabled=""[^>]*>Save</);
    expect(render({ saving: true })).toContain("Saving…");
  });

  test("on with fewer than two signed-in accounts says nothing can move yet", () => {
    const on = { ...policy, enabled: true };
    expect(render({ policy: on, signedInCount: 1 })).toContain("Nothing can move until at least two Claude accounts are signed in.");
    expect(render({ policy: on, signedInCount: 2 })).not.toContain("Nothing can move");
    expect(render({ policy, signedInCount: 0 })).not.toContain("Nothing can move");
  });

  test("a weekly threshold is shown when stored and explained", () => {
    const html = render({ policy: { ...policy, weeklyThresholdPercent: 95 } });
    expect(html).toContain('value="95"');
    expect(html).toContain("or 95% of a weekly one");
  });

  test("a refused save is an alert", () => {
    const html = render({ error: "The account switching rules changed. Refresh and apply your change again." });
    expect(html).toContain('role="alert"');
    expect(html).toContain("rules changed");
  });
});

describe("the terms warning", () => {
  test("says what switching does, that terms may restrict it, and asks for a tick", () => {
    const html = renderToStaticMarkup(<AccountRiskBody checked={false} onCheckedChange={() => {}} />);
    expect(html).toContain("only at the start of a new turn");
    expect(html).toContain("restrict how subscription logins may be used");
    expect(html).toContain("check each provider&#x27;s terms");
    expect(html).toContain('role="checkbox"');
    expect(html).toContain('aria-checked="false"');
    expect(renderToStaticMarkup(<AccountRiskBody checked onCheckedChange={() => {}} />)).toContain('aria-checked="true"');
  });

  test("the dialog names the vendor and cannot be confirmed before the tick", () => {
    const html = renderToStaticMarkup(
      <AccountRiskDialog open vendorName="Codex" onCancel={() => {}} onConfirm={() => {}} portal={false} />,
    );
    expect(html).toContain("Turn on account switching for Codex?");
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>Turn on</);
    expect(html).toContain("Cancel");
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/AccountPolicyEditor.test.tsx`

Expected: `error: Cannot find module './AccountPolicyEditor'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/AccountRiskDialog.tsx`:

```tsx
import { useEffect, useState } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { SWITCHING_TRUTH, TERMS_WARNING } from "./accountPolicy";

/** The words of the terms warning and the box that accepts it. */
export function AccountRiskBody({
  checked,
  onCheckedChange,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex flex-col gap-3" data-testid="account-risk-body">
      <p className="text-body-13 text-text-secondary">{SWITCHING_TRUTH}</p>
      <p className="text-body-13 text-text-secondary">{TERMS_WARNING}</p>
      <Checkbox
        label="I understand, and I will check the terms of every account I add."
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
    </div>
  );
}

/**
 * Shown the first time switching is turned on, and never again once it was
 * accepted. Turning it on is the explicit action the backend wants before it
 * stamps the time; Cancel leaves the switch where it was.
 */
export function AccountRiskDialog({
  open,
  vendorName,
  onCancel,
  onConfirm,
  portal = true,
}: {
  open: boolean;
  vendorName: string;
  onCancel: () => void;
  onConfirm: () => void;
  /** False draws the panel in place, for a test that has no document to portal into. */
  portal?: boolean;
}) {
  const [checked, setChecked] = useState(false);
  useEffect(() => {
    if (!open) setChecked(false);
  }, [open]);
  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <AlertDialogContent portal={portal}>
        <AlertDialogHeader>
          <AlertDialogTitle>{`Turn on account switching for ${vendorName}?`}</AlertDialogTitle>
          <AlertDialogDescription>
            {`DevKit can move a ${vendorName} session to another of your accounts when one reaches its limit.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AccountRiskBody checked={checked} onCheckedChange={setChecked} />
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={!checked} onClick={onConfirm}>
            Turn on
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
```

Create `frontend/src/modules/agenthub/accounts/AccountPolicyEditor.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import type { VendorPolicy } from "@/bridge/agentAccounts";
import { AccountRiskDialog } from "./AccountRiskDialog";
import {
  MAX_AGE_MAX,
  MAX_AGE_MIN,
  REFUSAL_CHOICES,
  SWITCHING_TRUTH,
  THRESHOLD_MAX,
  THRESHOLD_MIN,
  fieldsDiffer,
  fieldsOf,
  parseFields,
  refusalOf,
  thresholdMeaning,
  type PolicyFields,
} from "./accountPolicy";

type Problem = { field: "threshold" | "weekly" | "maxAge"; text: string };

/**
 * When a session moves to another account, and what to do with a turn the vendor
 * refused. Presentational like SubagentLimitsEditor: the container reads and
 * saves (compare-and-swap on the whole preference), so the gallery can draw every
 * state. The order is the list above it; this form never changes it.
 */
export function AccountPolicyEditor({
  vendorName,
  policy,
  signedInCount,
  riskAcknowledged,
  saving,
  error,
  onSave,
}: {
  vendorName: string;
  policy: VendorPolicy;
  /** How many of the vendor's accounts are signed in: with fewer than two nothing can move. */
  signedInCount: number;
  /** The terms warning was accepted before; turning switching on then asks nothing. */
  riskAcknowledged: boolean;
  saving: boolean;
  error: string;
  /** `acknowledgeRisk` is true only when the person just accepted the warning. Resolves true when stored. */
  onSave: (next: VendorPolicy, acknowledgeRisk: boolean) => Promise<boolean>;
}) {
  const stored = fieldsOf(policy);
  const storedKey = JSON.stringify(stored);
  const [fields, setFields] = useState<PolicyFields>(stored);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [saved, setSaved] = useState(false);
  const [askRisk, setAskRisk] = useState(false);
  const [accepted, setAccepted] = useState(false);

  // A save elsewhere (or the first read) moves what is stored under the form.
  useEffect(() => {
    setFields(JSON.parse(storedKey) as PolicyFields);
    setProblem(null);
  }, [storedKey]);

  const parsed = parseFields(fields);
  const dirty = fieldsDiffer(fields, stored);
  const refusal = REFUSAL_CHOICES.find((choice) => choice.value === fields.refusal) ?? REFUSAL_CHOICES[0];
  const edit = (change: Partial<PolicyFields>) => {
    setFields((current) => ({ ...current, ...change }));
    setProblem(null);
    setSaved(false);
  };

  async function save() {
    const next = parseFields(fields);
    if (!next.ok) {
      setProblem({ field: next.field, text: next.problem });
      return;
    }
    setProblem(null);
    setSaved(false);
    if (await onSave({ ...next.value, order: policy.order }, accepted && next.value.enabled)) {
      setSaved(true);
      setAccepted(false);
    }
  }

  const fieldError = (field: Problem["field"]) => (problem?.field === field ? problem.text : undefined);

  return (
    <section className="flex flex-col gap-3" aria-label={`${vendorName} switching`}>
      <h3 className="text-body-14-medium text-text-primary">Switching</h3>
      <p className="text-body-12 text-text-tertiary">{SWITCHING_TRUTH}</p>
      <form
        className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        <label className="flex items-center gap-2.5">
          <span className="min-w-0 flex-1 text-body-13 text-text-primary">{`Move ${vendorName} sessions to the next account at a limit`}</span>
          <Switch
            aria-label={`Switch ${vendorName} accounts at a limit`}
            checked={fields.enabled}
            disabled={saving}
            onCheckedChange={(on) => {
              if (on && !riskAcknowledged && !accepted) setAskRisk(true);
              else edit({ enabled: on });
            }}
          />
        </label>
        {fields.enabled && signedInCount < 2 ? (
          <p className="text-body-12 text-status-warning" data-testid="account-policy-needs-two">
            Nothing can move until at least two {vendorName} accounts are signed in.
          </p>
        ) : null}

        <div className="flex flex-wrap items-start gap-3">
          <div className="w-44">
            <Input
              label="Move at (% used)"
              type="number"
              inputMode="numeric"
              min={THRESHOLD_MIN}
              max={THRESHOLD_MAX}
              step={1}
              value={fields.threshold}
              error={fieldError("threshold")}
              onChange={(event) => edit({ threshold: event.target.value })}
            />
          </div>
          <div className="w-44">
            <Input
              label="Weekly window (% used)"
              type="number"
              inputMode="numeric"
              min={THRESHOLD_MIN}
              max={THRESHOLD_MAX}
              step={1}
              value={fields.weekly}
              placeholder="Same as the left"
              error={fieldError("weekly")}
              onChange={(event) => edit({ weekly: event.target.value })}
            />
          </div>
          <div className="w-44">
            <Input
              label="Re-check after (seconds)"
              type="number"
              inputMode="numeric"
              min={MAX_AGE_MIN}
              max={MAX_AGE_MAX}
              step={1}
              value={fields.maxAge}
              error={fieldError("maxAge")}
              onChange={(event) => edit({ maxAge: event.target.value })}
            />
          </div>
        </div>
        <p className="text-body-12 text-text-secondary">{thresholdMeaning(parsed)}</p>
        <p className="text-body-12 text-text-tertiary">
          Before a turn, DevKit asks the vendor again about a shared login whose last check is older than the re-check time.
          Usage is never read on a timer.
        </p>

        <label className="flex flex-col gap-1">
          <span className="text-caption-11 text-text-tertiary">When the vendor refuses a turn at its limit</span>
          <NativeSelect
            aria-label="When the vendor refuses a turn at its limit"
            value={fields.refusal}
            disabled={saving}
            onChange={(event) => edit({ refusal: refusalOf(event.target.value) })}
          >
            {REFUSAL_CHOICES.map((choice) => (
              <NativeSelectOption key={choice.value} value={choice.value}>
                {choice.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <span className="text-caption-11 text-text-tertiary">{refusal.detail}</span>
        </label>

        <div className="flex items-center gap-3">
          <Button type="submit" size="lg" disabled={saving || !dirty}>
            {saving ? "Saving…" : "Save"}
          </Button>
          {saved ? (
            <p className="text-body-12 text-text-secondary" role="status">
              Saved. Applies to turns started from now on.
            </p>
          ) : null}
        </div>
        {error ? (
          <p className="text-body-12 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}
      </form>

      <AccountRiskDialog
        open={askRisk}
        vendorName={vendorName}
        onCancel={() => setAskRisk(false)}
        onConfirm={() => {
          setAccepted(true);
          setAskRisk(false);
          edit({ enabled: true });
        }}
      />
    </section>
  );
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/AccountPolicyEditor.test.tsx`

Expected: `8 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/AccountRiskDialog.tsx \
  frontend/src/modules/agenthub/accounts/AccountPolicyEditor.tsx \
  frontend/src/modules/agenthub/accounts/AccountPolicyEditor.test.tsx
git commit -m "agenthub: the switching rules form and the terms warning" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 8: The container and the sign-in for one account

`AgentAccountsSetting` is `AgentSubagentLimitsSetting`'s shape for a list: queries for the accounts and the policy, one change at a time per account (`attempt` turns a refusal into its sentence and refetches), a CAS save of the whole policy (`setQueryData` on success, error + refetch on a stale revision). `AccountSignIn` reuses `ClaudeSignIn` (terminal tab, `closeSettings()` first, outcome toast) and `CodexSignIn` (in-app) with the account's id.

**Files:**
- Create: `frontend/src/modules/agenthub/accounts/accountActions.ts`
- Create: `frontend/src/modules/agenthub/accounts/accountActions.test.ts`
- Create: `frontend/src/modules/agenthub/accounts/AccountSignIn.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AgentAccountsSetting.tsx`
- Create: `frontend/src/modules/agenthub/accounts/AgentAccountsSetting.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/accounts/accountActions.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { attempt, dropAccount, replaceAccount } from "./accountActions";

const account = (id: string, label = id): AgentAccount => ({
  id,
  vendor: "claude",
  label,
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
});

function fakeClient(initial: AgentAccount[] | undefined) {
  let cache = initial;
  const invalidated: unknown[] = [];
  return {
    client: {
      setQueryData: (_key: unknown, update: (current: AgentAccount[] | undefined) => AgentAccount[] | undefined) => {
        cache = update(cache);
        return cache;
      },
      invalidateQueries: (filter: unknown) => (invalidated.push(filter), Promise.resolve()),
    } as never,
    cache: () => cache,
    invalidated,
  };
}

describe("attempt", () => {
  test("hands back what the call answered and leaves the list alone", async () => {
    const f = fakeClient([]);
    expect(await attempt(f.client, ["k"], async () => 7)).toEqual({ ok: true, value: 7 });
    expect(f.invalidated).toEqual([]);
  });

  test("a refusal comes back as its sentence and the list is read again", async () => {
    const f = fakeClient([]);
    const out = await attempt(f.client, ["agenthub", "accounts", "claude"], async () => {
      throw new Error("A session is using that account. Close it first.");
    });
    expect(out).toEqual({ ok: false, message: "A session is using that account. Close it first." });
    expect(f.invalidated).toEqual([{ queryKey: ["agenthub", "accounts", "claude"] }]);
  });
});

describe("the cached list", () => {
  test("a renamed account takes its own place, and nothing else moves", () => {
    const f = fakeClient([account("a"), account("b")]);
    replaceAccount(f.client, ["k"], account("b", "Renamed"));
    expect(f.cache()?.map((a) => a.label)).toEqual(["a", "Renamed"]);
  });

  test("a list that was never read stays unread", () => {
    const f = fakeClient(undefined);
    replaceAccount(f.client, ["k"], account("a"));
    dropAccount(f.client, ["k"], "a");
    expect(f.cache()).toBeUndefined();
  });

  test("a removed account leaves the list", () => {
    const f = fakeClient([account("a"), account("b")]);
    dropAccount(f.client, ["k"], "a");
    expect(f.cache()?.map((a) => a.id)).toEqual(["b"]);
  });
});
```

Create `frontend/src/modules/agenthub/accounts/AgentAccountsSetting.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AccountPolicy, AgentAccount } from "@/bridge/agentAccounts";
import { AgentAccountsSetting } from "./AgentAccountsSetting";
import { AGENT_ACCOUNT_POLICY_KEY, agentAccountsKey } from "./useAgentAccounts";

const account = (over: Partial<AgentAccount>): AgentAccount => ({
  id: "claude-default",
  vendor: "claude",
  label: "System default",
  builtin: true,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});

const policy: AccountPolicy = {
  revision: 2,
  riskAcknowledgedAt: "2026-10-23T08:00:00Z",
  vendors: {
    claude: {
      enabled: true,
      thresholdPercent: 85,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: ["claude-work", "claude-default"],
      onLimitRefusal: "none",
    },
  },
};

function render(client: QueryClient) {
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AgentAccountsSetting vendor="claude" />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("AgentAccountsSetting", () => {
  test("draws the accounts in the saved order with the rules under them", () => {
    const client = new QueryClient();
    client.setQueryData(agentAccountsKey("claude"), [
      account({}),
      account({ id: "claude-work", label: "Work", builtin: false }),
      account({ id: "claude-spare", label: "Spare", builtin: false, signedIn: false }),
    ]);
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, policy);
    const html = render(client);
    const at = (id: string) => html.indexOf(`data-account="${id}"`);
    expect(at("claude-work")).toBeGreaterThan(-1);
    expect(at("claude-work")).toBeLessThan(at("claude-default"));
    expect(at("claude-default")).toBeLessThan(at("claude-spare"));
    expect(html).toContain("Switching");
    expect(html).toContain('value="85"');
    // The signed-out account carries Claude's own sign-in, with where it goes.
    expect(html).toContain('data-testid="claude-sign-in"');
    expect(html).toContain("this account&#x27;s own folder");
  });

  test("before the first answer both halves wait instead of showing an empty list", () => {
    const html = render(new QueryClient());
    expect(html).not.toContain('data-testid="agent-account-row"');
    expect(html).not.toContain("No accounts yet.");
    expect(html.match(/animate-pulse/g)?.length).toBe(2);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountActions.test.ts src/modules/agenthub/accounts/AgentAccountsSetting.test.tsx`

Expected: `Cannot find module './accountActions'` and `Cannot find module './AgentAccountsSetting'`, `2 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/accounts/accountActions.ts`:

```ts
import type { QueryClient } from "@tanstack/react-query";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";

/**
 * What the accounts page does around a bridge call, apart from React so the
 * rules can be tested with a fake client.
 */

type ClientLike = Pick<QueryClient, "setQueryData" | "invalidateQueries">;
type Key = readonly unknown[];

export type Outcome<T> = { ok: true; value: T } | { ok: false; message: string };

/**
 * Runs a change. A refusal comes back as its sentence (Go's, as the bridge
 * published it) and the list is read again, because a refusal is often a stale
 * view: the account was removed elsewhere, or a session now runs as it.
 */
export async function attempt<T>(client: ClientLike, key: Key, run: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, value: await run() };
  } catch (cause) {
    void client.invalidateQueries({ queryKey: key });
    return { ok: false, message: errorMessage(cause) };
  }
}

/** Puts an account the backend answered with into the cached list, in its place. */
export function replaceAccount(client: ClientLike, key: Key, next: AgentAccount): void {
  client.setQueryData<AgentAccount[]>(key, (current) => current?.map((account) => (account.id === next.id ? next : account)));
}

/** Takes a removed account out of the cached list. */
export function dropAccount(client: ClientLike, key: Key, id: string): void {
  client.setQueryData<AgentAccount[]>(key, (current) => current?.filter((account) => account.id !== id));
}
```

Create `frontend/src/modules/agenthub/accounts/AccountSignIn.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { closeSettings } from "@/workbench/settingsRequest";
import { ClaudeSignIn, useClaudeSignIn } from "../ClaudeSignIn";
import { CodexSignIn, useCodexSignIn } from "../CodexSignIn";
import { vendorInfo } from "../vendors";
import { AGENT_ACCOUNTS_KEY } from "./useAgentAccounts";

/** The line under a sign-in button: where this login goes, and that the machine's own is left alone. */
export function accountSignInNote(vendor: string): string {
  const name = vendorInfo(vendor).name;
  return vendor === "claude"
    ? "Signs in to this account's own folder. The Claude login you use yourself in ~/.claude is left alone, and DevKit never sees your password."
    : `Signs in to this account's own ${name} folder. Your ~/.codex is left alone, and DevKit never sees your password.`;
}

/**
 * Claude's login runs in a terminal tab, as it does under Runtimes: the tab is
 * in the bottom slot behind Settings, so starting it closes Settings, and how it
 * ended is told in a toast. Nothing reopens Settings afterwards — no sign-in does.
 */
function ClaudeAccountSignIn({ account }: { account: AgentAccount }) {
  const client = useQueryClient();
  const signIn = useClaudeSignIn(
    (outcome) => {
      void client.invalidateQueries({ queryKey: AGENT_ACCOUNTS_KEY });
      if (outcome.signedIn) toast.success(`${account.label}: ${outcome.message}`);
      else toast.error(`${account.label}: ${outcome.message}`);
    },
    account.vendor,
    true,
    account.id,
  );
  return <ClaudeSignIn signIn={signIn} vendor={account.vendor} onStart={closeSettings} note={accountSignInNote(account.vendor)} />;
}

/** Codex signs in inside DevKit, so the page stays open and the row turns "Signed in" when the event lands. */
function CodexAccountSignIn({ account }: { account: AgentAccount }) {
  const signIn = useCodexSignIn((outcome) => {
    if (outcome.signedIn) toast.success(`${account.label}: ${outcome.message}`);
  }, account.id);
  return <CodexSignIn signIn={signIn} note={accountSignInNote(account.vendor)} />;
}

/** The sign-in for a signed-out account, by how its vendor signs in. */
export function AccountSignIn({ account }: { account: AgentAccount }) {
  return vendorInfo(account.vendor).signIn === "inApp" ? (
    <CodexAccountSignIn key={account.id} account={account} />
  ) : (
    <ClaudeAccountSignIn key={account.id} account={account} />
  );
}
```

Create `frontend/src/modules/agenthub/accounts/AgentAccountsSetting.tsx`:

```tsx
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import {
  agentAccountPolicySet,
  agentAccountsAdd,
  agentAccountsRefreshStatus,
  agentAccountsRefreshUsage,
  agentAccountsRemove,
  agentAccountsRename,
  agentAccountsSetShared,
  type VendorPolicy,
} from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";
import { AccountPolicyEditor } from "./AccountPolicyEditor";
import { AccountSignIn } from "./AccountSignIn";
import { AccountsEditor } from "./AccountsEditor";
import { attempt, dropAccount, replaceAccount } from "./accountActions";
import { movedOrder, refreshNote } from "./accountModel";
import { vendorPolicyOf, withVendorPolicy } from "./accountPolicy";
import {
  AGENT_ACCOUNT_POLICY_KEY,
  agentAccountsKey,
  useAgentAccountPolicy,
  useAgentAccounts,
  useNow,
} from "./useAgentAccounts";
import { vendorInfo } from "../vendors";

/**
 * Settings → Agents → Accounts & limits, for one vendor: its accounts and the
 * rules that move a session between them. Reads and saves through the UI-only
 * bridge methods; the two editors below only draw.
 */
export function AgentAccountsSetting({ vendor }: { vendor: string }) {
  const client = useQueryClient();
  const vendorName = vendorInfo(vendor).name;
  const key = agentAccountsKey(vendor);
  const accounts = useAgentAccounts(vendor);
  const policy = useAgentAccountPolicy();
  const now = useNow();
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");
  const [refreshNotes, setRefreshNotes] = useState<Record<string, string>>({});
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [policyError, setPolicyError] = useState("");
  const list = accounts.data;

  /** One change at a time per account: the buttons are off while it runs. */
  async function change<T>(id: string, run: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
    setBusyId(id);
    setError("");
    const outcome = await attempt(client, key, run);
    setBusyId("");
    if (outcome.ok) return outcome;
    setError(outcome.message);
    return { ok: false };
  }

  /** Stores one vendor's rules; a stale revision is fixed by reading again. */
  async function savePolicy(next: VendorPolicy, acknowledgeRisk: boolean, report: (message: string) => void): Promise<boolean> {
    if (!policy.data) return false;
    setSavingPolicy(true);
    report("");
    const outcome = await attempt(client, AGENT_ACCOUNT_POLICY_KEY, () =>
      agentAccountPolicySet(withVendorPolicy(policy.data!, vendor, next), acknowledgeRisk),
    );
    setSavingPolicy(false);
    if (!outcome.ok) {
      report(outcome.message);
      return false;
    }
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, outcome.value);
    return true;
  }

  const current = vendorPolicyOf(policy.data, vendor);

  return (
    <div className="flex flex-col gap-6">
      <AccountsEditor
        vendor={vendor}
        vendorName={vendorName}
        accounts={list}
        order={current.order}
        loadError={accounts.error ? errorMessage(accounts.error) : ""}
        busyId={busyId}
        error={error}
        refreshNotes={refreshNotes}
        now={now}
        renderSignIn={(account) => <AccountSignIn account={account} />}
        onAdd={async (label) => {
          const done = await change("add", () => agentAccountsAdd(vendor, label));
          if (done.ok) void client.invalidateQueries({ queryKey: key });
          return done.ok;
        }}
        onRename={async (id, label) => {
          const done = await change(id, () => agentAccountsRename(id, label));
          if (done.ok) replaceAccount(client, key, done.value);
          return done.ok;
        }}
        onRemove={async (id) => {
          const done = await change(id, () => agentAccountsRemove(id));
          if (done.ok) dropAccount(client, key, id);
          return done.ok;
        }}
        onSetShared={(id, shared) =>
          void change(id, () => agentAccountsSetShared(id, shared)).then((done) => done.ok && replaceAccount(client, key, done.value))
        }
        onRefreshUsage={(id) => {
          const before = list?.find((account) => account.id === id);
          setRefreshNotes((notes) => ({ ...notes, [id]: "" }));
          void change(id, () => agentAccountsRefreshUsage(id)).then((done) => {
            if (!done.ok) return;
            replaceAccount(client, key, done.value);
            setRefreshNotes((notes) => ({ ...notes, [id]: before ? refreshNote(before, done.value) : "" }));
          });
        }}
        onRefreshStatus={(id) =>
          void change(id, () => agentAccountsRefreshStatus(id)).then((done) => done.ok && replaceAccount(client, key, done.value))
        }
        onMove={(id, delta) => {
          if (!list || !policy.data) return;
          setBusyId(id);
          setError("");
          void savePolicy({ ...current, order: movedOrder(list, current.order, id, delta) }, false, setError).then(() => setBusyId(""));
        }}
      />

      {policy.isPending ? <Skeleton className="h-48 w-full rounded-control" /> : null}
      {policy.error ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {errorMessage(policy.error)}
        </p>
      ) : null}
      {policy.data ? (
        <AccountPolicyEditor
          vendorName={vendorName}
          policy={current}
          signedInCount={list?.filter((account) => account.signedIn).length ?? 0}
          riskAcknowledged={Boolean(policy.data.riskAcknowledgedAt.trim())}
          saving={savingPolicy}
          error={policyError}
          onSave={(next, acknowledgeRisk) => savePolicy(next, acknowledgeRisk, setPolicyError)}
        />
      ) : null}
    </div>
  );
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/accounts/accountActions.test.ts src/modules/agenthub/accounts/AgentAccountsSetting.test.tsx`

Expected: `7 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/accounts/accountActions.ts \
  frontend/src/modules/agenthub/accounts/accountActions.test.ts \
  frontend/src/modules/agenthub/accounts/AccountSignIn.tsx \
  frontend/src/modules/agenthub/accounts/AgentAccountsSetting.tsx \
  frontend/src/modules/agenthub/accounts/AgentAccountsSetting.test.tsx
git commit -m "agenthub: the accounts container, changes and per-account sign-in" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 9: The page in Settings

A seventh page in the Agents group, placed after `agents-models` and before `agents-skills`, so the nav reads Runtimes, Models & subagents, Accounts & limits, Skills & MCP, Safety, Notifications & remote, DevKit as MCP. `SettingsLayout`'s `ICONS` is `satisfies Record<SettingsPage, unknown>`, so the build fails without an icon. The guard tests (`moduleBoundaryRatchet`, `designSystemConformance`, `smoothnessRatchet`, `storeSubscriptionRatchet`, `focusRing`) run here because this is the first task that mounts the new files from a settings page; none of their baselines change. The nav keywords must not contain any word the existing unique-search tests use (`subagent`, `api key`, `sandbox`, `checkpoints`, `skills`, `notifications`, `telegram`, `phone pairing`, `proxy install`, `credentials`, `third-party`, `open source`, `terminal font`).

**Files:**
- Modify: `frontend/src/modules/settings/settingsNavigation.ts`
- Modify: `frontend/src/modules/settings/SettingsLayout.tsx`
- Modify: `frontend/src/modules/settings/SettingsView.tsx`
- Modify: `frontend/src/modules/settings/AgentsPages.tsx`
- Modify: `frontend/src/modules/settings/settingsNavigation.test.ts`

- [ ] **Step 1: Write the failing tests**

In `frontend/src/modules/settings/settingsNavigation.test.ts`, replace:

```ts
  test("Agents is its own group of six pages, and Services no longer holds agent settings", () => {
    const agents = SETTINGS_GROUPS.find((group) => group.label === "Agents")!;
    expect(agents.items.map((item) => item.id)).toEqual([
      "agents-runtimes", "agents-models", "agents-skills", "agents-safety", "agents-notifications", "agents-mcp",
    ]);
```

with:

```ts
  test("Agents is its own group of seven pages, and Services no longer holds agent settings", () => {
    const agents = SETTINGS_GROUPS.find((group) => group.label === "Agents")!;
    expect(agents.items.map((item) => item.id)).toEqual([
      "agents-runtimes", "agents-models", "agents-accounts", "agents-skills", "agents-safety", "agents-notifications", "agents-mcp",
    ]);
```

In `frontend/src/modules/settings/settingsNavigation.test.ts`, replace:

```ts
    expect(idsOf("agents")).toHaveLength(6);
  });
```

with:

```ts
    expect(idsOf("agents")).toHaveLength(7);
  });
  test("accounts and their limits are found by what people call them", () => {
    expect(idsOf("failover")).toEqual(["agents-accounts"]);
    expect(idsOf("threshold")).toEqual(["agents-accounts"]);
    expect(idsOf("subscription")).toEqual(["agents-accounts"]);
    expect(idsOf("quota")).toEqual(["agents-accounts"]);
    expect(idsOf("accounts limits")).toEqual(["agents-accounts"]);
    expect(resolveSettingsTab("agents-accounts")).toBe("agents-accounts");
  });
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/settings src/moduleBoundaryRatchet.test.ts src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts src/focusRing.test.ts`

Expected: in `settingsNavigation.test.ts`, `Agents is its own group of seven pages` fails (the list has six), `idsOf("failover")` is `[]`, and `idsOf("agents")` has length 6.

- [ ] **Step 3: Implement**

In `frontend/src/modules/settings/settingsNavigation.ts`, replace:

```ts
  | "agents-runtimes" | "agents-models" | "agents-skills" | "agents-safety" | "agents-notifications" | "agents-mcp";
```

with:

```ts
  | "agents-runtimes" | "agents-models" | "agents-accounts" | "agents-skills" | "agents-safety" | "agents-notifications" | "agents-mcp";
```

In `frontend/src/modules/settings/settingsNavigation.ts`, replace:

```ts
    { id: "agents-skills", label: "Skills & MCP",
```

with:

```ts
    { id: "agents-accounts", label: "Accounts & limits", keywords: "accounts several multiple subscription usage quota threshold switch failover order priority terms add claude codex" },
    { id: "agents-skills", label: "Skills & MCP",
```

In `frontend/src/modules/settings/SettingsLayout.tsx`, replace:

```tsx
Bell, Scale } from "lucide-react";
```

with:

```tsx
Bell, Scale, Users } from "lucide-react";
```

In `frontend/src/modules/settings/SettingsLayout.tsx`, replace:

```tsx
"agents-models": Cpu, "agents-skills": Puzzle,
```

with:

```tsx
"agents-models": Cpu, "agents-accounts": Users, "agents-skills": Puzzle,
```

In `frontend/src/modules/settings/SettingsView.tsx`, replace:

```tsx
import {
  AgentMcpPage,
  AgentModelsPage,
```

with:

```tsx
import {
  AgentAccountsPage,
  AgentMcpPage,
  AgentModelsPage,
```

In `frontend/src/modules/settings/SettingsView.tsx`, replace:

```tsx
              {activeTab === "agents-models" && <AgentModelsPage />}
```

with:

```tsx
              {activeTab === "agents-models" && <AgentModelsPage />}
              {activeTab === "agents-accounts" && <AgentAccountsPage />}
```

In `frontend/src/modules/settings/AgentsPages.tsx`, replace:

```tsx
import { AgentEnvironmentPanel } from "@/modules/agenthub/environment/AgentEnvironment";
```

with:

```tsx
import { ACCOUNT_VENDORS } from "@/modules/agenthub/accounts/accountModel";
import { AgentAccountsSetting } from "@/modules/agenthub/accounts/AgentAccountsSetting";
import { AgentEnvironmentPanel } from "@/modules/agenthub/environment/AgentEnvironment";
```

In `frontend/src/modules/settings/AgentsPages.tsx`, replace:

```tsx
 * Claude | Codex, a mode switch inside one page. The choice is remembered
 * across pages and restarts; only the selected vendor's panel mounts, so the
 * other one fetches nothing.
 */
function VendorSwitch({ label, render }: { label: string; render: (vendor: string) => React.ReactNode }) {
  const [vendor, setVendor] = useState(() => readAgentVendor(SHIPPED_VENDORS));
```

with:

```tsx
 * Claude | Codex, a mode switch inside one page. The choice is remembered
 * across pages and restarts; only the selected vendor's panel mounts, so the
 * other one fetches nothing. A page whose topic only some vendors have (accounts)
 * says which in `vendors`; a remembered choice outside them falls back to the first.
 */
function VendorSwitch({
  label,
  render,
  vendors = SHIPPED_VENDORS,
}: {
  label: string;
  render: (vendor: string) => React.ReactNode;
  vendors?: readonly string[];
}) {
  const [vendor, setVendor] = useState(() => readAgentVendor(vendors));
```

In `frontend/src/modules/settings/AgentsPages.tsx`, replace:

```tsx
        {SHIPPED_VENDORS.map((id) => (
          <TabsTrigger key={id} value={id}>{vendorInfo(id).name}</TabsTrigger>
        ))}
      </TabsList>
      {SHIPPED_VENDORS.map((id) => (
        <TabsContent key={id} value={id}>{render(id)}</TabsContent>
      ))}
```

with:

```tsx
        {vendors.map((id) => (
          <TabsTrigger key={id} value={id}>{vendorInfo(id).name}</TabsTrigger>
        ))}
      </TabsList>
      {vendors.map((id) => (
        <TabsContent key={id} value={id}>{render(id)}</TabsContent>
      ))}
```

In `frontend/src/modules/settings/AgentsPages.tsx`, replace:

```tsx
export function AgentSkillsPage() {
```

with:

```tsx
export function AgentAccountsPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Accounts & limits"
        description="Several accounts per agent, in the order DevKit should use them, and when a session should move to the next one."
      />
      <VendorSwitch label="Agent accounts" vendors={ACCOUNT_VENDORS} render={(vendor) => <AgentAccountsSetting vendor={vendor} />} />
    </TabColumn>
  );
}

export function AgentSkillsPage() {
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/settings src/moduleBoundaryRatchet.test.ts src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts src/focusRing.test.ts`

Expected: every test passes, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/settings/settingsNavigation.ts \
  frontend/src/modules/settings/SettingsLayout.tsx \
  frontend/src/modules/settings/SettingsView.tsx \
  frontend/src/modules/settings/AgentsPages.tsx \
  frontend/src/modules/settings/settingsNavigation.test.ts
git commit -m "settings: Accounts & limits page" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 10: The transcript line

Today `session_info_update` rows that carry only `_meta.devkit.accountSwitch` / `accountSwitchFailed` are consumed with `continue` in both `applySessionUpdates` and `initTranscript` and never drawn (read with `grep -a` if you inspect `transcript.ts`: it holds bytes plain grep reports nothing on). `SessionUpdate.accountNotice` is already typed in Go (`update.go` `AccountNotice`), so the notice rides on the update and on the stored payload; nothing is read from `raw`. A notice becomes an item of kind `devkit_account_notice`, which `groupTurns` already files under `turn.notes` (it is not in `WORK_KINDS`); `Turn.notes` is collected today and never drawn, so `AgentTurn` draws the account ones. A move happens before the next prompt, so the line lands in the previous turn's notes and closes it. `golden` and `transcriptPerformance` tests run here: a `session_info_update` with no notice must stay byte-identical.

**Files:**
- Create: `frontend/src/modules/agenthub/transcript/AccountNotice.tsx`
- Create: `frontend/src/modules/agenthub/transcript/AccountNotice.test.tsx`
- Create: `frontend/src/modules/agenthub/transcriptAccountNotice.test.ts`
- Modify: `frontend/src/modules/agenthub/transcript.ts`
- Modify: `frontend/src/modules/agenthub/surfaces/AgentTurn.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/modules/agenthub/transcript/AccountNotice.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { agentAccountsKey } from "../accounts/useAgentAccounts";
import { AgentTurn } from "../surfaces/AgentTurn";
import { groupTurns } from "../turns";
import type { AccountNotice as AccountNoticeData, TranscriptItem } from "../types";
import { AccountNotice, AccountNoticeLine } from "./AccountNotice";

const account = (id: string, label: string, builtin = false): AgentAccount => ({
  id,
  vendor: "claude",
  label,
  builtin,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
});

const move: AccountNoticeData = { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };

function withAccounts(node: React.ReactNode, accounts?: AgentAccount[]) {
  const client = new QueryClient();
  if (accounts) client.setQueryData(agentAccountsKey("claude"), accounts);
  return renderToString(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

describe("AccountNotice", () => {
  it("a move reads as a quiet line with the reason after a dot", () => {
    const html = renderToString(<AccountNotice notice={move} fromLabel="System default" toLabel="Work" />);
    expect(html).toContain('data-testid="account-notice"');
    expect(html).toContain("Switched from System default to Work · 5-hour 95% ≥ 90%");
    expect(html).toContain("text-text-tertiary");
    expect(html).not.toContain("text-status-warning");
  });

  it("a move that did not happen leads with that, in the warning tone", () => {
    const html = renderToString(
      <AccountNotice notice={{ kind: "failed", from: "a", to: "b", reason: "It could not be opened." }} fromLabel="Work" toLabel="Spare" />,
    );
    expect(html).toContain("Could not switch from Work to Spare: It could not be opened.");
    expect(html).toContain("text-status-warning");
    expect(html).toContain('data-kind="failed"');
  });
});

describe("AccountNoticeLine", () => {
  it("names the accounts from the list", () => {
    const html = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />, [
      account("claude-default", "System default", true),
      account("claude-work", "Work"),
    ]);
    expect(html).toContain("Switched from System default to Work");
  });

  it("falls back for the built-in account, a removed one and a list not read yet", () => {
    const removed = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />, [account("claude-default", "System default", true)]);
    expect(removed).toContain("Switched from System default to a removed account");
    const waiting = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />);
    expect(waiting).toContain("Switched from System default to another account");
  });
});

describe("in the transcript", () => {
  const items: TranscriptItem[] = [
    { id: "u1", kind: "user_message_chunk", text: "first", messageId: "m1", createdAt: "2026-10-23T08:00:00Z" },
    { id: "a1", kind: "agent_message_chunk", text: "Done.", messageId: "a1", createdAt: "2026-10-23T08:00:01Z" },
    { id: "n1", kind: "devkit_account_notice", accountNotice: move, createdAt: "2026-10-23T08:00:02Z" },
    { id: "u2", kind: "user_message_chunk", text: "second", messageId: "m2", createdAt: "2026-10-23T08:00:03Z" },
  ];

  it("is drawn at the end of the turn it follows and nowhere else", () => {
    const turns = groupTurns(items);
    const first = withAccounts(<AgentTurn turn={turns[0]} vendor="claude" />, [account("claude-default", "System default", true), account("claude-work", "Work")]);
    expect(first).toContain("Switched from System default to Work");
    expect(first.indexOf("Done.")).toBeLessThan(first.indexOf("Switched from"));
    const second = withAccounts(<AgentTurn turn={turns[1]} vendor="claude" />);
    expect(second).not.toContain("Switched from");
  });

  it("a turn that is only a notice (the first turn moved before it began) still draws it", () => {
    const [turn] = groupTurns([{ id: "n0", kind: "devkit_account_notice", accountNotice: move }]);
    expect(withAccounts(<AgentTurn turn={turn} vendor="claude" />)).toContain("Switched from");
  });

  it("other notes stay undrawn", () => {
    const [turn] = groupTurns([{ id: "x", kind: "something_unmodelled" }]);
    expect(withAccounts(<AgentTurn turn={turn} vendor="claude" />)).not.toContain("account-notice");
  });
});
```

Create `frontend/src/modules/agenthub/transcriptAccountNotice.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import {
  ACCOUNT_NOTICE_KIND,
  applySessionUpdate,
  createInitialTranscriptModel,
  dropRecordedChunks,
  initTranscript,
  modelFromNewestPage,
  readAccountNotice,
  sameAccountNotice,
} from "./transcript";
import { groupTurns } from "./turns";
import type { AccountNotice, SessionUpdate } from "./types";

const move: AccountNotice = { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };
const noticeUpdate = (notice: AccountNotice = move): SessionUpdate => ({
  kind: "session_info_update",
  accountNotice: notice,
  raw: { sessionUpdate: "session_info_update" },
});

describe("readAccountNotice", () => {
  test("takes a switch or a failed switch between two ids, and nothing else", () => {
    expect(readAccountNotice(move)).toEqual(move);
    expect(readAccountNotice({ kind: "failed", from: "a", to: "b" })).toEqual({ kind: "failed", from: "a", to: "b" });
    expect(readAccountNotice({ kind: "failed", from: "a", to: "b", reason: "" })).toEqual({ kind: "failed", from: "a", to: "b" });
    for (const bad of [null, undefined, "x", {}, { kind: "other", from: "a", to: "b" }, { kind: "switch", from: "a", to: "" }, { kind: "switch", from: 1, to: "b" }]) {
      expect(readAccountNotice(bad)).toBeNull();
    }
  });

  test("two notices are the same when kind, ends and reason are", () => {
    expect(sameAccountNotice(move, { ...move })).toBe(true);
    expect(sameAccountNotice(move, { ...move, reason: "other" })).toBe(false);
    expect(sameAccountNotice(move, { ...move, kind: "failed" })).toBe(false);
    expect(sameAccountNotice(move, null)).toBe(false);
  });
});

describe("a live notice", () => {
  test("becomes a row of its own and leaves the session state alone", () => {
    let model = applySessionUpdate(createInitialTranscriptModel(), { kind: "session_info_update", goal: { objective: "ship" } });
    model = applySessionUpdate(model, noticeUpdate());
    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({ kind: ACCOUNT_NOTICE_KIND, accountNotice: move });
    expect(model.goal?.objective).toBe("ship");
    expect(model.failure).toBeNull();
  });

  test("a title or a goal update draws nothing", () => {
    const model = applySessionUpdate(createInitialTranscriptModel(), { kind: "session_info_update", title: "Refund worker" });
    expect(model.items).toEqual([]);
  });

  test("a malformed notice draws nothing", () => {
    const model = applySessionUpdate(createInitialTranscriptModel(), {
      kind: "session_info_update",
      accountNotice: { kind: "switch", from: "a", to: "" },
    });
    expect(model.items).toEqual([]);
  });

  test("sits between the turns: after the answer it follows, before the next prompt", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "first" }, messageId: "m1" });
    model = applySessionUpdate(model, { kind: "agent_message_chunk", content: { type: "text", text: "done" }, messageId: "a1" });
    model = applySessionUpdate(model, noticeUpdate());
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "second" }, messageId: "m2" });
    const turns = groupTurns(model.items);
    expect(turns).toHaveLength(2);
    expect(turns[0].notes.map((n) => n.accountNotice)).toEqual([move]);
    expect(turns[1].notes).toEqual([]);
  });
});

describe("a stored notice", () => {
  const entry = (id: string, payload: unknown) => ({
    ID: id,
    Kind: "session_info_update",
    Payload: JSON.stringify(payload),
    Seq: 7,
    CreatedAt: "2026-10-23T08:00:00Z",
    UpdatedAt: "2026-10-23T08:00:00Z",
  });

  test("comes back as the row the live update made", () => {
    const model = initTranscript([entry("r1", { kind: "session_info_update", accountNotice: move, raw: {} })], "s1");
    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({ id: "r1", kind: ACCOUNT_NOTICE_KIND, seq: 7, sessionId: "s1", accountNotice: move });
  });

  test("a stored title row still draws nothing", () => {
    expect(initTranscript([entry("r2", { kind: "session_info_update", title: "Named" })], "s1").items).toEqual([]);
  });
});

describe("a notice that arrived while the page loaded", () => {
  const stored = (notice: AccountNotice) => ({
    ID: "r1",
    Kind: "session_info_update",
    Payload: JSON.stringify({ kind: "session_info_update", accountNotice: notice }),
    Seq: 3,
  });

  test("is not drawn twice when the page already holds it", () => {
    const model = modelFromNewestPage({ entries: [stored(move)], state: [] }, [noticeUpdate()], "s1");
    expect(model.items.filter((item) => item.accountNotice)).toHaveLength(1);
  });

  test("is drawn when the page does not hold it, or holds a different one", () => {
    const empty = modelFromNewestPage({ entries: [], state: [] }, [noticeUpdate()], "s1");
    expect(empty.items.filter((item) => item.accountNotice)).toHaveLength(1);
    const back: AccountNotice = { kind: "switch", from: "claude-work", to: "claude-default", reason: "Weekly 95% ≥ 90%" };
    const other = modelFromNewestPage({ entries: [stored(move)], state: [] }, [noticeUpdate(back)], "s1");
    expect(other.items.filter((item) => item.accountNotice).map((item) => item.accountNotice)).toEqual([move, back]);
  });

  test("only one buffered copy is dropped", () => {
    const model = initTranscript([stored(move)], "s1");
    expect(dropRecordedChunks(model, [noticeUpdate(), noticeUpdate()])).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/transcriptAccountNotice.test.ts src/modules/agenthub/transcript src/modules/agenthub/surfaces`

Expected: `Cannot find module './AccountNotice'`, and in `transcriptAccountNotice.test.ts` the named exports (`ACCOUNT_NOTICE_KIND`, `readAccountNotice`, `sameAccountNotice`) are not found.

- [ ] **Step 3: Implement**

Create `frontend/src/modules/agenthub/transcript/AccountNotice.tsx`:

```tsx
import { cn } from "@/lib/utils";
import { accountLabelOf, noticeText } from "../accounts/accountModel";
import { useAgentAccounts } from "../accounts/useAgentAccounts";
import type { AccountNotice as AccountNoticeData } from "../types";

/**
 * A session moved to another account, or tried to and stayed. One quiet line in
 * the transcript, the weight of the docked plan's caption: it is news about where
 * the work runs, not something anyone has to answer. A move that did not happen
 * leads with what did not, in the warning tone, so the colour is never the only
 * signal.
 */
export function AccountNotice({
  notice,
  fromLabel,
  toLabel,
}: {
  notice: AccountNoticeData;
  fromLabel: string;
  toLabel: string;
}) {
  const failed = notice.kind === "failed";
  return (
    <p
      data-testid="account-notice"
      data-kind={notice.kind}
      className={cn("w-full text-caption-11", failed ? "text-status-warning" : "text-text-tertiary")}
    >
      {noticeText(notice, fromLabel, toLabel)}
    </p>
  );
}

/**
 * The notice with the accounts' names: asked of the same list Settings reads, so
 * a rename shows here too. An id the list no longer holds reads "a removed
 * account", and the built-in account has its own name before the list arrives.
 */
export function AccountNoticeLine({ notice, vendor }: { notice: AccountNoticeData; vendor: string }) {
  const accounts = useAgentAccounts(vendor).data;
  return (
    <AccountNotice
      notice={notice}
      fromLabel={accountLabelOf(accounts, vendor, notice.from)}
      toLabel={accountLabelOf(accounts, vendor, notice.to)}
    />
  );
}
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
let fallbackIdCounter = 0;
```

with:

```ts
/** The item kind an account move is kept under: a line in the transcript, not session state. */
export const ACCOUNT_NOTICE_KIND = "devkit_account_notice";

/**
 * A move between vendor accounts, read from a live update or a stored row. Anything
 * that is not a switch or a failed switch between two ids is not a notice.
 */
export function readAccountNotice(value: unknown): AccountNotice | null {
  const record = value as Record<string, unknown> | null | undefined;
  if (!record || typeof record !== "object") return null;
  if (record.kind !== "switch" && record.kind !== "failed") return null;
  if (typeof record.from !== "string" || typeof record.to !== "string" || !record.to) return null;
  return {
    kind: record.kind,
    from: record.from,
    to: record.to,
    ...(typeof record.reason === "string" && record.reason ? { reason: record.reason } : {}),
  };
}

export function sameAccountNotice(a: AccountNotice | null | undefined, b: AccountNotice | null | undefined): boolean {
  return Boolean(a && b && a.kind === b.kind && a.from === b.from && a.to === b.to && (a.reason ?? "") === (b.reason ?? ""));
}

let fallbackIdCounter = 0;
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
      if (update.goal) {
        goal = update.goal;
        changed = true;
      } else if (update.goalCleared) {
        goal = null;
        changed = true;
      }
      continue;
    }
```

with:

```ts
      if (update.goal) {
        goal = update.goal;
        changed = true;
      } else if (update.goalCleared) {
        goal = null;
        changed = true;
      }
      // A move between accounts is not session state: it is a line where it happened.
      const notice = readAccountNotice(update.accountNotice);
      if (notice) {
        copyItems();
        items.push({
          id: generateId(),
          kind: ACCOUNT_NOTICE_KIND,
          accountNotice: notice,
          raw: update.raw,
          createdAt: now(),
          updatedAt: now(),
        });
        changed = true;
      }
      continue;
    }
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
      if (payload.failure) model.failure = payload.failure;
      if (payload.goal) model.goal = payload.goal;
      else if (payload.goalCleared) model.goal = null;
      continue;
    }
```

with:

```ts
      if (payload.failure) model.failure = payload.failure;
      if (payload.goal) model.goal = payload.goal;
      else if (payload.goalCleared) model.goal = null;
      const notice = readAccountNotice(payload.accountNotice);
      if (notice) {
        model.items.push({
          id,
          kind: ACCOUNT_NOTICE_KIND,
          seq,
          sessionId,
          accountNotice: notice,
          raw: payload.raw,
          createdAt,
          updatedAt,
        });
      }
      continue;
    }
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
 * ponytail: a text-suffix match, not a cursor.
```

with:

```ts
 * An account notice the page already holds is dropped the same way: the newest one
 * stored is the one a buffered copy would repeat (a move that stood twice in a row
 * needs a move back in between, so an identical newer one is never lost to this).
 *
 * ponytail: a text-suffix match, not a cursor.
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
  return dropped.size === 0 ? updates : updates.filter((_, index) => !dropped.has(index));
}
```

with:

```ts
  const heldNotice = [...model.items].reverse().find((item) => item.accountNotice)?.accountNotice;
  if (heldNotice) {
    const index = updates.findIndex((update) => sameAccountNotice(heldNotice, readAccountNotice(update.accountNotice)));
    if (index >= 0) dropped.add(index);
  }
  return dropped.size === 0 ? updates : updates.filter((_, index) => !dropped.has(index));
}
```

In `frontend/src/modules/agenthub/transcript.ts`, replace:

```ts
import type {
  SessionUpdate,
  ToolCall,
  TranscriptAction,
  TranscriptModel,
  TranscriptItem,
  Subagent,
  AsyncTask,
} from "./types";
```

with:

```ts
import type {
  AccountNotice,
  SessionUpdate,
  ToolCall,
  TranscriptAction,
  TranscriptModel,
  TranscriptItem,
  Subagent,
  AsyncTask,
} from "./types";
```

In `frontend/src/modules/agenthub/surfaces/AgentTurn.tsx`, replace:

```tsx
import { PeerMessage } from "../transcript/PeerMessage";
```

with:

```tsx
import { PeerMessage } from "../transcript/PeerMessage";
import { AccountNoticeLine } from "../transcript/AccountNotice";
```

In `frontend/src/modules/agenthub/surfaces/AgentTurn.tsx`, replace:

```tsx
      {checkpoint ? (
        <AgentTurnChanges checkpoint={checkpoint} loadDiff={loadFileDiff} />
      ) : null}
```

with:

```tsx
      {checkpoint ? (
        <AgentTurnChanges checkpoint={checkpoint} loadDiff={loadFileDiff} />
      ) : null}

      {/* A session that moved to another account does it before the next prompt,
          so the line closes the turn it follows and reads as sitting between the two. */}
      {turn.notes.map((note) =>
        note.accountNotice ? <AccountNoticeLine key={note.id} notice={note.accountNotice} vendor={vendor ?? ""} /> : null,
      )}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/transcriptAccountNotice.test.ts src/modules/agenthub/transcript src/modules/agenthub/surfaces`

Expected: every test passes, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/agenthub/transcript/AccountNotice.tsx \
  frontend/src/modules/agenthub/transcript/AccountNotice.test.tsx \
  frontend/src/modules/agenthub/transcriptAccountNotice.test.ts \
  frontend/src/modules/agenthub/transcript.ts \
  frontend/src/modules/agenthub/surfaces/AgentTurn.tsx
git commit -m "agenthub: draw an account move in the transcript" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 11: The summary card's Account row

`AgentSession.accountId` is `AccountID` in the bound JSON (PascalCase; the account DTOs are camelCase), `""` for the built-in account. The row is built where the other environment rows are (`AgentHubContent`), asking the shared accounts list only for a session that carries an id.

**Files:**
- Create: `frontend/src/bridge/agenthubSession.test.ts`
- Modify: `frontend/src/bridge/agenthub.ts`
- Modify: `frontend/src/modules/agenthub/summary/summaryModel.ts`
- Modify: `frontend/src/modules/agenthub/summary/summaryModel.test.ts`
- Modify: `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/bridge/agenthubSession.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AgentSession as RawSession } from "../../bindings/devkit/internal/agenthub/models";
import { toSession } from "./agenthub";

const raw = (over: Partial<RawSession> = {}) => ({ ID: "s1", AgentType: "claude", ...over }) as RawSession;

describe("toSession", () => {
  test("reads the account the session runs as from the bound AccountID", () => {
    expect(toSession(raw({ AccountID: "claude-work" })).accountId).toBe("claude-work");
  });

  test("a session on the built-in account has an empty one", () => {
    expect(toSession(raw()).accountId).toBe("");
    expect(toSession(raw({ AccountID: "" })).accountId).toBe("");
  });
});
```

In `frontend/src/modules/agenthub/summary/summaryModel.test.ts`, replace:

```ts
  test("one repository is its branch, several are counted", () => {
```

with:

```ts
  test("names the account only once the session is known to run as one, right after the agent", () => {
    const base = { vendorName: "Claude", folderPath: "", configOptions: options, mode: null, availableModes: [], repos: [] };
    expect(environmentRows({ ...base, accountLabel: "Work" }).map((r) => [r.key, r.value])).toEqual([
      ["agent", "Claude"],
      ["account", "Work"],
      ["model", "GPT-6"],
      ["effort", "Medium"],
    ]);
    expect(environmentRows(base).find((r) => r.key === "account")).toBeUndefined();
    expect(environmentRows({ ...base, accountLabel: null }).find((r) => r.key === "account")).toBeUndefined();
    expect(environmentRows({ ...base, accountLabel: "" }).find((r) => r.key === "account")).toBeUndefined();
  });
  test("one repository is its branch, several are counted", () => {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/bridge/agenthubSession.test.ts src/modules/agenthub/summary src/modules/agenthub/surfaces`

Expected: `summaryModel.test.ts` `names the account only once…` fails (no `account` row), and `agenthubSession.test.ts` fails because `toSession` is not exported.

- [ ] **Step 3: Implement**

In `frontend/src/bridge/agenthub.ts`, replace:

```ts
  updatedAt?: number;
};
```

with:

```ts
  updatedAt?: number;
  /**
   * The vendor account the session runs as. "" is the built-in one (the machine's
   * own Claude login, DevKit's own Codex home); after a move back it can also be
   * the "<vendor>-default" id. The bound JSON calls this field AccountID.
   */
  accountId?: string;
};
```

In `frontend/src/bridge/agenthub.ts`, replace:

```ts
function toSession(raw: RawSession): AgentSession {
```

with:

```ts
export function toSession(raw: RawSession): AgentSession {
```

In `frontend/src/bridge/agenthub.ts`, replace:

```ts
    updatedAt: toEpoch(raw.UpdatedAt),
  };
}
```

with:

```ts
    updatedAt: toEpoch(raw.UpdatedAt),
    accountId: raw.AccountID ?? "",
  };
}
```

In `frontend/src/modules/agenthub/summary/summaryModel.ts`, replace:

```ts
  vendorName: string;
  folderPath: string;
  workspaceName?: string | null;
```

with:

```ts
  vendorName: string;
  /**
   * The name of the account the session runs as, once it is known. Absent for a
   * session on the built-in account that never moved, and while the list that
   * names the account is still on its way: the row is left out, never guessed.
   */
  accountLabel?: string | null;
  folderPath: string;
  workspaceName?: string | null;
```

In `frontend/src/modules/agenthub/summary/summaryModel.ts`, replace:

```ts
  const rows: SummaryRow[] = [{ key: "agent", label: "Agent", value: input.vendorName }];
```

with:

```ts
  const rows: SummaryRow[] = [{ key: "agent", label: "Agent", value: input.vendorName }];
  if (input.accountLabel) rows.push({ key: "account", label: "Account", value: input.accountLabel });
```

In `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx`, replace:

```tsx
import { environmentRows } from "../summary/summaryModel";
```

with:

```tsx
import { environmentRows } from "../summary/summaryModel";
import { accountLabelFor } from "../accounts/accountModel";
import { useAgentAccounts } from "../accounts/useAgentAccounts";
```

In `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx`, replace:

```tsx
  const environment = React.useMemo(
    () =>
      session && !draft
        ? environmentRows({
            vendorName: vendorInfo(session.agentType).name,
            folderPath: session.projectPath,
```

with:

```tsx
  // The account the session runs as. Asked only for a session that carries one:
  // the list is the vendor's, shared with Settings, and a session on the
  // built-in account that never moved has nothing to say.
  const accountId = session && !draft ? session.accountId ?? "" : "";
  const accounts = useAgentAccounts(session?.agentType ?? "", Boolean(accountId));
  const accountLabel = session ? accountLabelFor(accounts.data, session.agentType, accountId) : undefined;
  const environment = React.useMemo(
    () =>
      session && !draft
        ? environmentRows({
            vendorName: vendorInfo(session.agentType).name,
            accountLabel,
            folderPath: session.projectPath,
```

In `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx`, replace:

```tsx
    [session, draft, tray.status, configOptions, mode, availableModes],
```

with:

```tsx
    [session, draft, accountLabel, tray.status, configOptions, mode, availableModes],
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/bridge/agenthubSession.test.ts src/modules/agenthub/summary src/modules/agenthub/surfaces`

Expected: every test passes, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/bridge/agenthubSession.test.ts \
  frontend/src/bridge/agenthub.ts \
  frontend/src/modules/agenthub/summary/summaryModel.ts \
  frontend/src/modules/agenthub/summary/summaryModel.test.ts \
  frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx
git commit -m "agenthub: the summary card names the session's account" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 12: Keeping it fresh, and telling the person

A component that renders null (the `UpdateNotifier` shape) mounted in the Workbench next to `AgentSessionsSubscriber`: it runs `watchAgentAccounts`, and on `agentaccounts:switched` it writes the moved session's `accountId` into the sessions store (so the Account row is right at once) and toasts only when that session is on screen. The rules are a pure function over injected dependencies.

**Files:**
- Create: `frontend/src/workbench/AgentAccountsSubscriber.tsx`
- Create: `frontend/src/workbench/AgentAccountsSubscriber.test.ts`
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/workbench/AgentAccountsSubscriber.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { handleAccountSwitched, type SwitchDeps } from "./AgentAccountsSubscriber";

const event = { sessionId: "s1", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };

function deps(over: Partial<SwitchDeps> = {}) {
  const calls = { set: [] as [string, string][], toasts: [] as [string, string][] };
  const full: SwitchDeps = {
    session: () => ({ agentType: "claude", accountId: "" }),
    setAccount: (id, account) => void calls.set.push([id, account]),
    onScreen: () => true,
    labelOf: async () => "Work",
    notify: (title, description) => void calls.toasts.push([title, description]),
    ...over,
  };
  return { full, calls };
}

describe("a session moved to another account", () => {
  it("is recorded in the window's row and told in a toast when the session is on screen", async () => {
    const { full, calls } = deps();
    handleAccountSwitched(event, full);
    await Promise.resolve();
    await Promise.resolve();
    expect(calls.set).toEqual([["s1", "claude-work"]]);
    expect(calls.toasts).toEqual([["Moved to Work", "5-hour 95% ≥ 90%"]]);
  });

  it("is recorded but not announced when nobody is looking at the session", async () => {
    const { full, calls } = deps({ onScreen: () => false });
    handleAccountSwitched(event, full);
    await Promise.resolve();
    expect(calls.set).toEqual([["s1", "claude-work"]]);
    expect(calls.toasts).toEqual([]);
  });

  it("a row that already says so is left alone", async () => {
    const { full, calls } = deps({ session: () => ({ agentType: "claude", accountId: "claude-work" }) });
    handleAccountSwitched(event, full);
    expect(calls.set).toEqual([]);
  });

  it("a session this window does not know changes nothing and says nothing", async () => {
    const { full, calls } = deps({ session: () => undefined });
    handleAccountSwitched(event, full);
    await Promise.resolve();
    expect(calls.set).toEqual([]);
    expect(calls.toasts).toEqual([]);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/workbench`

Expected: `Cannot find module './AgentAccountsSubscriber'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/workbench/AgentAccountsSubscriber.tsx`:

```tsx
import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { agentAccountsList, onAgentAccountSwitched, type AccountSwitchedEvent } from "../bridge/agentAccounts";
import type { AgentSession } from "../bridge/agenthub";
import { accountLabelOf, switchedToast } from "../modules/agenthub/accounts/accountModel";
import { agentAccountsKey, watchAgentAccounts } from "../modules/agenthub/accounts/useAgentAccounts";
import { agentSessionOnScreen } from "../modules/agenthub/documents";
import { useAgentSessionsStore } from "../stores/agentSessions";

/** What a move needs from the outside, so the rules can be tested without a bridge. */
export type SwitchDeps = {
  session: (id: string) => Pick<AgentSession, "agentType" | "accountId"> | undefined;
  setAccount: (sessionId: string, accountId: string) => void;
  onScreen: (sessionId: string) => boolean;
  labelOf: (vendor: string, accountId: string) => Promise<string>;
  notify: (title: string, description: string) => void;
};

/**
 * A session moved to another account at the start of a turn. The row in the
 * window's store learns it at once (the summary card's Account line would
 * otherwise say the old account until the session is read again), and the person
 * is told in a toast only when that session is the one in front of them: the
 * transcript carries the same fact as a line, for every other session, where it
 * happened. A session this window does not know is none of its business.
 */
export function handleAccountSwitched(event: AccountSwitchedEvent, deps: SwitchDeps): void {
  const session = deps.session(event.sessionId);
  if (!session) return;
  if (session.accountId !== event.to) deps.setAccount(event.sessionId, event.to);
  if (!deps.onScreen(event.sessionId)) return;
  void deps.labelOf(session.agentType, event.to).then((label) => {
    const { title, description } = switchedToast(label, event.reason);
    deps.notify(title, description);
  });
}

function labelThroughQueries(client: QueryClient) {
  return async (vendor: string, accountId: string) => {
    try {
      const accounts = await client.fetchQuery({
        queryKey: agentAccountsKey(vendor),
        queryFn: () => agentAccountsList(vendor),
        staleTime: 10_000,
      });
      return accountLabelOf(accounts, vendor, accountId);
    } catch {
      // Nothing to name it by: the built-in account still has its own name.
      return accountLabelOf(undefined, vendor, accountId);
    }
  };
}

/**
 * Keeps the accounts lists current (a change event from Go, and the window
 * regaining focus) and reacts to a session moving. Renders nothing; mounted once,
 * beside AgentSessionsSubscriber, after the vault is open.
 */
export function AgentAccountsSubscriber() {
  const client = useQueryClient();
  useEffect(() => watchAgentAccounts(client), [client]);
  useEffect(() => {
    const deps: SwitchDeps = {
      session: (id) => useAgentSessionsStore.getState().sessionsById[id],
      setAccount: (sessionId, accountId) => {
        const known = useAgentSessionsStore.getState().sessionsById[sessionId];
        if (known) useAgentSessionsStore.getState().upsertSession({ ...known, accountId });
      },
      onScreen: (sessionId) => agentSessionOnScreen(sessionId),
      labelOf: labelThroughQueries(client),
      notify: (title, description) => toast.info(title, { description: description || undefined }),
    };
    return onAgentAccountSwitched((event) => handleAccountSwitched(event, deps));
  }, [client]);
  return null;
}
```

In `frontend/src/workbench/Workbench.tsx`, replace:

```tsx
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
```

with:

```tsx
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { AgentAccountsSubscriber } from "./AgentAccountsSubscriber";
```

In `frontend/src/workbench/Workbench.tsx`, replace:

```tsx
          <AgentSessionsSubscriber />
```

with:

```tsx
          <AgentSessionsSubscriber />
          <AgentAccountsSubscriber />
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/workbench`

Expected: every test passes, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/workbench/AgentAccountsSubscriber.tsx \
  frontend/src/workbench/AgentAccountsSubscriber.test.ts \
  frontend/src/workbench/Workbench.tsx
git commit -m "workbench: refresh accounts on events and tell the person when a session moves" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 13: The gallery

`?gallery#agent-accounts` draws the real editors from static props (the bridge is not there in a browser): the built-in account alone with switching off; three accounts in an order with an ok, a warning (83%) and a reached window; every account at its limit; a signed-out row with Claude's and Codex's sign-in; the Add account form and its busy state; a refused change, a failed read and the loading skeleton; switching on with one signed-in account, and a refused save; the terms warning; and both transcript lines. The existing transcript section gains a notice, and the summary section's Claude card an Account row. The section seeds the accounts query so the transcript line names "Work".

**Files:**
- Create: `frontend/src/dev/AgentAccountsSection.tsx`
- Create: `frontend/src/dev/AgentAccountsSection.test.tsx`
- Modify: `frontend/src/dev/ComponentGallery.tsx`
- Modify: `frontend/src/dev/AgentHubTranscriptSection.tsx`
- Modify: `frontend/src/dev/AgentSummarySection.tsx`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/dev/AgentAccountsSection.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentAccountsSection } from "./AgentAccountsSection";

describe("the accounts gallery section", () => {
  const html = renderToStaticMarkup(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <AgentAccountsSection />
      </TooltipProvider>
    </QueryClientProvider>,
  );

  test("is the page the gallery links to and draws every state the plan lists", () => {
    expect(html).toContain('id="agent-accounts"');
    for (const state of [
      "Only the built-in account",
      "Three accounts in a saved order",
      "Every account at its limit",
      "A signed-out account offers its sign-in",
      "Add account: the form",
      "A change Go refused",
      "Switching on with one signed-in account",
      "The terms warning",
      "In the transcript",
    ]) {
      expect(html).toContain(state);
    }
  });

  test("uses the real editors: meters, both tones, the add form, both notices", () => {
    expect(html).toContain('role="meter"');
    expect(html).toContain('data-tone="warning"');
    expect(html).toContain('data-tone="danger"');
    expect(html).toContain('aria-label="Add a Claude account"');
    expect(html).toContain("Switched from System default to Work");
    expect(html).toContain("Could not switch from System default to Spare");
    expect(html).toContain("Codex");
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd frontend && bun test --isolate src/dev/AgentAccountsSection.test.tsx`

Expected: `Cannot find module './AgentAccountsSection'` and `1 fail`.

- [ ] **Step 3: Implement**

Create `frontend/src/dev/AgentAccountsSection.tsx`:

```tsx
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { AgentAccount, VendorPolicy } from "@/bridge/agentAccounts";
import { AccountPolicyEditor } from "@/modules/agenthub/accounts/AccountPolicyEditor";
import { AccountRiskBody } from "@/modules/agenthub/accounts/AccountRiskDialog";
import { AccountsEditor, type AccountsEditorProps } from "@/modules/agenthub/accounts/AccountsEditor";
import { accountSignInNote } from "@/modules/agenthub/accounts/AccountSignIn";
import { agentAccountsKey } from "@/modules/agenthub/accounts/useAgentAccounts";
import { CodexSignInPanel } from "@/modules/agenthub/CodexSignIn";
import { AccountNotice } from "@/modules/agenthub/transcript/AccountNotice";

/**
 * Settings → Agents → Accounts & limits in its states, and the two transcript
 * notices, fed fixtures and no bridge. The editors are the real ones; only the
 * sign-in controls are static, because the live ones start a login. It also seeds
 * the accounts list the conversation section above reads, so its "Switched from"
 * line names Work. `?gallery#agent-accounts`.
 */
const NOW = Date.now();
const sec = NOW / 1000;
const noop = async () => true;

const builtin: AgentAccount = {
  id: "claude-default",
  vendor: "claude",
  label: "System default",
  builtin: true,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [
    { label: "5-hour", usedPercent: 41, resetsAt: sec + 2 * 3600, status: "ok" },
    { label: "Weekly", usedPercent: 22, resetsAt: sec + 4 * 86_400, status: "ok" },
  ],
  usageSource: "observed",
  observedAt: sec - 40 * 60,
};
const work: AgentAccount = {
  ...builtin,
  id: "claude-work",
  label: "Work",
  builtin: false,
  shared: true,
  usage: [
    { label: "5-hour", usedPercent: 83, resetsAt: sec + 90 * 60, status: "warning" },
    { label: "Weekly", usedPercent: 40, resetsAt: sec + 3 * 86_400, status: "ok" },
  ],
  usageSource: "checked",
  observedAt: sec - 2 * 60,
};
const spare: AgentAccount = {
  ...builtin,
  id: "claude-spare",
  label: "Spare",
  builtin: false,
  usage: [{ label: "5-hour", usedPercent: 100, resetsAt: sec + 3 * 3600, status: "reached" }],
  exhaustedUntil: sec + 3 * 3600,
  usageSource: "observed",
  observedAt: sec - 5 * 60,
};
const signedOut: AgentAccount = { ...work, signedIn: false, usage: [], usageSource: "", observedAt: 0 };

const codexBuiltin: AgentAccount = { ...builtin, id: "codex-default", vendor: "codex", label: "DevKit default" };
const codexSecond: AgentAccount = { ...signedOut, id: "codex-personal", vendor: "codex", label: "Personal", shared: false };

const off: VendorPolicy = {
  enabled: false,
  thresholdPercent: 90,
  weeklyThresholdPercent: 0,
  usageMaxAgeSeconds: 300,
  order: [],
  onLimitRefusal: "none",
};
const on: VendorPolicy = { ...off, enabled: true, weeklyThresholdPercent: 95, onLimitRefusal: "resendIfNoProgress", order: ["claude-work", "claude-default", "claude-spare"] };

const SIGN_IN_CLAUDE = (account: AgentAccount) => (
  <div className="flex w-full flex-col gap-2 text-left">
    <div className="flex flex-wrap items-center gap-2">
      <Button type="button" size="sm">
        <LogIn />
        Sign in with Claude subscription
      </Button>
    </div>
    <p className="text-caption-11 text-text-tertiary">{accountSignInNote(account.vendor)}</p>
  </div>
);
const SIGN_IN_CODEX = (account: AgentAccount) => (
  <CodexSignInPanel
    flow={null}
    starting=""
    outcome={null}
    startError=""
    onStart={() => {}}
    onCancel={() => {}}
    note={accountSignInNote(account.vendor)}
  />
);

function editorProps(over: Partial<AccountsEditorProps>): AccountsEditorProps {
  return {
    vendor: "claude",
    vendorName: "Claude",
    accounts: [builtin],
    order: [],
    loadError: "",
    busyId: "",
    error: "",
    refreshNotes: {},
    now: NOW,
    renderSignIn: SIGN_IN_CLAUDE,
    onAdd: noop,
    onRename: noop,
    onRemove: noop,
    onSetShared: () => {},
    onRefreshUsage: () => {},
    onRefreshStatus: () => {},
    onMove: () => {},
    ...over,
  };
}

const frame = "w-full max-w-170 rounded-control border border-border-subtle p-4";
const caption = "mb-2 text-label-12 text-text-tertiary";

export function AgentAccountsSection() {
  const client = useQueryClient();
  // The conversation section names accounts through the same list Settings reads.
  useEffect(() => {
    client.setQueryData(agentAccountsKey("claude"), [builtin, work, spare]);
  }, [client]);
  return (
    <section id="agent-accounts" className="flex flex-col gap-6 p-6">
      <h2 className="text-heading-18 text-text-primary">Accounts & limits</h2>

      <div className={frame}>
        <p className={caption}>Only the built-in account: nothing to reorder or remove, switching off</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({})} />
          <AccountPolicyEditor vendorName="Claude" policy={off} signedInCount={1} riskAcknowledged={false} saving={false} error="" onSave={noop} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Three accounts in a saved order: usage ok, a warning at 83% and one that reached its limit; switching on</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, work, spare], order: on.order, refreshNotes: { "claude-work": "Nothing new: DevKit asks the vendor at most every few seconds, and this reading is already the latest." } })} />
          <AccountPolicyEditor vendorName="Claude" policy={on} signedInCount={2} riskAcknowledged saving={false} error="" onSave={noop} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Every account at its limit: each one says until when</p>
        <AccountsEditor {...editorProps({ accounts: [{ ...spare, id: "claude-default", label: "System default", builtin: true }, spare] })} />
      </div>

      <div className={frame}>
        <p className={caption}>A signed-out account offers its sign-in (Claude: terminal, Codex: in DevKit)</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, signedOut] })} />
          <AccountsEditor
            {...editorProps({ vendor: "codex", vendorName: "Codex", accounts: [codexBuiltin, codexSecond], renderSignIn: SIGN_IN_CODEX })}
          />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Add account: the form, and while it adds</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ initialAddOpen: true })} />
          <AccountsEditor {...editorProps({ initialAddOpen: true, busyId: "add" })} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>A change Go refused, and a list that could not be read</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, work], error: "A session is using that account. Close it first." })} />
          <AccountsEditor {...editorProps({ accounts: undefined, loadError: "Accounts are not available." })} />
          <AccountsEditor {...editorProps({ accounts: undefined })} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Switching on with one signed-in account, and a save the store refused</p>
        <div className="flex flex-col gap-6">
          <AccountPolicyEditor vendorName="Claude" policy={{ ...on, order: [] }} signedInCount={1} riskAcknowledged saving={false} error="" onSave={noop} />
          <AccountPolicyEditor
            vendorName="Claude"
            policy={on}
            signedInCount={2}
            riskAcknowledged
            saving={false}
            error="The account switching rules changed. Refresh and apply your change again."
            onSave={noop}
          />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>The terms warning, shown the first time switching is turned on (try the switch in the first card)</p>
        <AccountRiskBody checked={false} onCheckedChange={() => {}} />
      </div>

      <div className={frame}>
        <p className={caption}>In the transcript: a move that stood, and a move that did not happen</p>
        <div className="flex flex-col gap-2">
          <AccountNotice notice={{ kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" }} fromLabel="System default" toLabel="Work" />
          <AccountNotice notice={{ kind: "failed", from: "claude-default", to: "claude-spare", reason: "That account could not be opened." }} fromLabel="System default" toLabel="Spare" />
        </div>
      </div>
    </section>
  );
}
```

In `frontend/src/dev/ComponentGallery.tsx`, replace:

```tsx
import { AccountLinksSection } from "./AccountLinksSection";
```

with:

```tsx
import { AccountLinksSection } from "./AccountLinksSection";
import { AgentAccountsSection } from "./AgentAccountsSection";
```

In `frontend/src/dev/ComponentGallery.tsx`, replace:

```tsx
      <SubagentLimitsSection />
```

with:

```tsx
      <SubagentLimitsSection />
      <AgentAccountsSection />
```

In `frontend/src/dev/AgentHubTranscriptSection.tsx`, replace:

```tsx
  { id: "u2", kind: "user_message_chunk", text: "Khám phá cấu trúc repo này", createdAt: at(10) },
```

with:

```tsx
  // The session moved to another account before the next prompt (see Accounts & limits below).
  {
    id: "an1",
    kind: "devkit_account_notice",
    accountNotice: { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" },
    createdAt: at(8),
  },
  { id: "u2", kind: "user_message_chunk", text: "Khám phá cấu trúc repo này", createdAt: at(10) },
```

In `frontend/src/dev/AgentSummarySection.tsx`, replace:

```tsx
  model,
  ...rest
}: {
  vendorName: string;
  items: TranscriptItem[];
  usage: Usage;
  checkpoints?: CheckpointIndex;
  repos?: { name: string; branch: string; own: boolean }[];
  model?: string;
}
```

with:

```tsx
  model,
  accountLabel,
  ...rest
}: {
  vendorName: string;
  items: TranscriptItem[];
  usage: Usage;
  checkpoints?: CheckpointIndex;
  repos?: { name: string; branch: string; own: boolean }[];
  model?: string;
  /** The account the session runs as, when it is not the built-in one that never moved. */
  accountLabel?: string;
}
```

In `frontend/src/dev/AgentSummarySection.tsx`, replace:

```tsx
  const environment = environmentRows({
    vendorName,
    folderPath:
```

with:

```tsx
  const environment = environmentRows({
    vendorName,
    accountLabel,
    folderPath:
```

In `frontend/src/dev/AgentSummarySection.tsx`, replace:

```tsx
<Card vendorName="Claude" items={claudeItems} usage={claudeUsage} goal=
```

with:

```tsx
<Card vendorName="Claude" accountLabel="Work" items={claudeItems} usage={claudeUsage} goal=
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd frontend && bun test --isolate src/dev/AgentAccountsSection.test.tsx`

Expected: `2 pass`, `0 fail`.

- [ ] **Step 5: Typecheck**

Run: `cd frontend && ./node_modules/.bin/tsc --noEmit -p tsconfig.json`

Expected: no output (tests are typechecked too, and `noUnusedLocals` is on).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/dev/AgentAccountsSection.tsx \
  frontend/src/dev/AgentAccountsSection.test.tsx \
  frontend/src/dev/ComponentGallery.tsx \
  frontend/src/dev/AgentHubTranscriptSection.tsx \
  frontend/src/dev/AgentSummarySection.tsx
git commit -m "dev: the Accounts & limits gallery" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


### Task 14: AGENTS.md, and the whole-suite verification

The only documentation change is in `AGENTS.md`: the multi-account section's last "not done" bullet said the frontend was still to do, and a new bullet records where the UI lives and the rules a later change must keep (the `accountId` check on the sign-in event, the event/focus refetch, the toast rule). No Go file changes in this plan: the last step proves it.

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Edit AGENTS.md**

In `AGENTS.md`, replace:

```md
    - The frontend (the accounts UI work): Settings page, add and sign-in flows, drawing the two transcript notices, the
      summary row, a `devkit`-sourced prompt for `continue`. Hand-off notes for it:
```

with:

```md
    - A `devkit`-sourced prompt for `continue` (the window's side is built: see **Accounts UI** below). The hand-off
      notes the frontend followed:
```

In `AGENTS.md`, replace:

```md
- **The window learns of a session someone else created** (`Service.OnSessionCreated`
```

with:

```md
- **Accounts UI: Settings → Agents → Accounts & limits** (plan `docs/superpowers/plans/2026-10-23-agent-accounts-settings-ui.md`;
  gallery `?gallery#agent-accounts`). Frontend only: no Go changed. It reuses the Agents pages' own parts (`VendorSwitch`,
  the container/presentational split with compare-and-swap save, `ActionTooltip` icon buttons, the summary card's
  `role="meter"` bar, `ConfirmDialog`, `AlertDialog` + `Checkbox`) and adds no library, colour or layout primitive.
  - **Where it lives.** `frontend/src/modules/agenthub/accounts/` (pure `accountModel.ts`, `accountPolicy.ts`,
    `accountActions.ts`; `useAgentAccounts.ts` keys, hooks and `watchAgentAccounts`; presentational `AccountsEditor`,
    `AccountRow`, `AddAccountForm`, `UsageMeter`, `AccountPolicyEditor`, `AccountRiskDialog`; connected
    `AgentAccountsSetting`, `AccountSignIn`). Settings only mounts it from `AgentsPages.tsx` (`AgentAccountsPage`, the
    same way it mounts `AgentEnvironmentPanel`), so `moduleBoundaryRatchet` stays at `settings -> agenthub: 3`.
    `VendorSwitch` takes `vendors` now; accounts offer Claude | Codex only (`ACCOUNT_VENDORS`).
  - **Bridge** `bridge/agentAccounts.ts` over the real bindings: `usage` and `order` come back `[]` not `null`
    (`accountOf`, `policyOf`), times are Unix seconds, the policy is replaced whole with its `revision`.
    `acknowledgeRisk` is sent only when the person accepted the terms dialog in that edit and is turning switching on.
    Hand-typed because not bound: `SignInResult.accountId`, `SessionUpdate.accountNotice`, the `agentaccounts:switched`
    payload.
  - **`agenthub:signInCompleted` carries `accountId`.** `createSignInTracker(accountId)` acts only on events about its own
    account (`""` = the built-in sign-in); `useCodexSignIn(onDone, accountId)` / `useClaudeSignIn(onDone, vendor, enabled,
    accountId)` start the account flow instead; `AgentHubView`'s config-catalog refresh ignores an added account's event.
    A new handler of that event must check `accountId` too.
  - **Fresh without polling.** `AgentAccountsSubscriber` (mounted in `Workbench` beside `AgentSessionsSubscriber`) refetches
    the lists on `agentaccounts:changed` and on window focus (the event is throttled leading-edge: the newest reading can
    go unannounced), writes a moved session's `accountId` into the sessions store, and toasts "Moved to X" only when that
    session is on screen. Refresh usage has no "asked" flag in the answer: an unchanged `observedAt` is how the row says
    "nothing new" (`refreshNote`).
  - **Rows.** Order is the saved `order` then the rest (`orderedAccounts`); Move up / Move down save the whole policy at
    once (no drag and drop). A past `exhaustedUntil` / `cooldownUntil` says nothing; a window past its `resetsAt` is drawn
    empty ("reset since the last reading"). Removal shows Go's refusal inside the confirmation, and a removal that failed
    after the row was gone (folder left behind) shows its sentence on the page.
  - **Transcript.** A move is the item kind `devkit_account_notice` (`readAccountNotice`, both `applySessionUpdates` and
    `initTranscript`; a copy that arrived while the page loaded is dropped by `dropRecordedChunks`), lands in the previous
    turn's `notes` and is drawn by `AgentTurn` through `AccountNoticeLine`. The summary card's `Account` row
    (`EnvironmentInput.accountLabel`) is absent while `AgentSession.accountId` is empty or its list has not arrived.
  - **Not done.** A toast for "every account is limited" (it reaches the window as the turn's `agent_usage_limit` error, as
    before); a drag-to-reorder list; any account UI for opencode.
- **The window learns of a session someone else created** (`Service.OnSessionCreated`
```

- [ ] **Step 2: Whole frontend suite**

Run: `cd frontend && bun run test`

Expected: `0 fail` (this is `bun test --isolate`; without `--isolate` a `mock.module` leaks across files). In the scratch run of this plan: 4954 tests, 417 files, all passing.

- [ ] **Step 3: Typecheck, production build and the performance budget**

Run: `cd frontend && bun run build`

Expected: `tsc` clean, `vite build` succeeds, and the last line is `Performance budget: initial JS ... KiB, CSS ... KiB.` with no failure. In the scratch run: JS 1238.2 KiB (budget 2 MiB), CSS 282.7 KiB (budget 320 KiB). If the CSS budget ever trips, raise `INITIAL_CSS_BUDGET` in `frontend/scripts/check-performance-budget.mjs` with a comment saying why (the user's saved rule); never move styling into inline `style` or plain CSS.

- [ ] **Step 4: The guard tests by name**

Run: `cd frontend && bun test --isolate src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts src/focusRing.test.ts src/moduleBoundaryRatchet.test.ts src/modules/settings/settingsNavigation.test.ts`

Expected: all pass with no baseline edited. What they enforce on this change: no Tailwind `text-sm`, `[13px]` or hex under `modules/settings` or `modules/agenthub` (only the type scale `text-body-12/13`, `text-caption-11`, `text-label-11-caps`, `text-body-14-medium`); no `useState([])` beside a `useEffect(` (the new code has none); no `const { a } = useXStore()` without a selector (the subscriber uses `getState()`); focus rings are `ring-border-focus/50` (the new code adds no ring of its own); and `settings -> agenthub` stays 3.

- [ ] **Step 5: Lint the new files**

Run: `cd frontend && ./node_modules/.bin/eslint src/modules/agenthub/accounts src/bridge/agentAccounts.ts src/workbench/AgentAccountsSubscriber.tsx src/dev/AgentAccountsSection.tsx src/modules/agenthub/transcript/AccountNotice.tsx`

Expected: no output (`shadcn/no-arbitrary-values` is on: no `[...]` classes in the new code).

- [ ] **Step 6: Prove no Go changed**

Run: `git log --name-only --format= HEAD~14..HEAD | grep -v '^frontend/' | grep -v '^$' | sort -u`

Expected: only `AGENTS.md` (the 14 commits of this plan touch `frontend/` and `AGENTS.md`). `git diff --name-only HEAD -- internal main.go cmd frontend/bindings` prints nothing.

- [ ] **Step 7 (manual, needs a browser): look at it**

Run: `task dev:frontend` (Vite only, port 9245) and open `http://localhost:9245/?gallery#agent-accounts`. Check, in light and dark: the three-account card (meters fill 41 / 83 / 100 percent, the 83% window warning-toned and the full one danger-toned), the order arrows (first Up and last Down dimmed), the first card's switch (flipping it on opens the terms dialog; "Turn on" stays disabled until the box is ticked), the add form, the Codex sign-in panel, the transcript section's "Switched from System default to Work" line (`?gallery#agenthub-transcript`) and the summary card's `Account  Work` row (`?gallery#agenthub-summary`). The native app cannot run headless; this page is the visual check, as for every other Agent Hub surface.

- [ ] **Step 8: Commit**

```bash
git add AGENTS.md
git commit -m "AGENTS.md: the accounts UI, where it lives and what it must keep" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Self-review

**1. Spec coverage** (the ten items of the brief, then the spec's Interface section):

| # | Requirement | Where |
| --- | --- | --- |
| 1 | Bridge wrappers over the real bindings; null to `[]`; `callBridge`; events with focus refetch; hand-typed `signInCompleted.accountId` and `accountNotice`; existing handlers ignore a non-empty `accountId` | Task 1 (wrappers, events), Task 4 (`watchAgentAccounts`, focus), Task 5 (`accountId`, tracker, `AgentHubView`, tests), Task 2 (`AccountNotice` type) |
| 2 | `agents-accounts` page, nav, icon, mount, `AgentAccountsPage`, nav tests, ratchet | Task 9 (the ratchet needs no edit: the UI lives in `modules/agenthub/accounts`, decision 1) |
| 3 | Accounts list: built-in and added rows, signed-in state, Shared, usage meters with `resetsIn` text, "Observed/Checked N ago", cooldown/exhausted, Refresh usage / status, Rename, Remove with Go's sentence, Move up/down, empty/loading/error | Tasks 2, 6, 8 |
| 4 | Add account + sign-in: create, then Claude terminal (`closeSettings()` first, toast) and Codex in-app (`accountId`-filtered, cancel, refresh) | Tasks 5, 6 (Add form, row slot), 8 (`AccountSignIn`) |
| 5 | Policy editor with CAS, enabled, thresholds, weekly, re-check age, `onLimitRefusal`, order via the list, terms dialog + `acknowledgeRisk`, server limits mirrored | Tasks 3, 7, 8 |
| 6 | Transcript notice: types, both `transcript.ts` branches, dedupe, `AgentTurn` draws `turn.notes`, `AccountNotice.tsx`, labels with fallbacks | Tasks 2 (types), 10 |
| 7 | Summary "Account" row: `accountId`, `toSession`, `accountLabel`, `environmentRows`, wiring, tests, gallery | Tasks 11, 13 |
| 8 | Switch toast, component that renders null | Task 12 |
| 9 | Gallery section and fixtures | Task 13 |
| 10 | AGENTS.md, build/test expectations, guard tests, CSS budget rule | Tasks 9, 14 |

Spec "Interface" items deliberately not built: the "Refresh for all" button and the "all accounts limited" toast (decision 15). Everything else in that paragraph (enable switch with the first-enable dialog, ordered list with arrow reordering, sign-in state, shared toggle, usage bars with source and age, per-account refresh, thresholds, `usageMaxAgeSeconds`, `onLimitRefusal`, the summary `Account` row, the switched toast, the gallery states off / two accounts / one exhausted / all exhausted / stale numbers / sign-in dead) is in a task above.

**2. Placeholder scan.** Searched the plan for the usual placeholder phrases (deferred-work markers, vague "add handling" steps, "similar to Task N", "write tests for the above"): none. Every code step is a whole file or an exact find / replace pair, and every command has its expected result. All code in this plan was applied to a scratch checkout of this branch, task by task in this order: after each task its tests passed and `tsc --noEmit` was clean; at the end the full suite (4954 tests in 417 files), `eslint` on the new files and `vite build` with the performance budget all passed.

**3. Type consistency.** Names are used exactly as defined: `AgentAccount`, `AccountPolicy`, `VendorPolicy`, `AccountWindow`, `AccountSwitchedEvent` (Task 1); `accountLabelOf` (for a sentence, never undefined) vs `accountLabelFor` (the summary row, `undefined` until known), `orderedAccounts`, `movedOrder`, `usageRows`, `readingText`, `accountNotes`, `refreshNote`, `noticeText`, `switchedToast`, `ACCOUNT_VENDORS` (Task 2); `vendorPolicyOf`, `fieldsOf`, `parseFields`, `withVendorPolicy`, `thresholdMeaning`, `REFUSAL_CHOICES` (Task 3); `AGENT_ACCOUNTS_KEY`, `agentAccountsKey`, `AGENT_ACCOUNT_POLICY_KEY`, `useAgentAccounts(vendor, enabled)`, `watchAgentAccounts(client, subscribe, focus)` (Task 4); `createSignInTracker(accountId)`, `useCodexSignIn(onDone, accountId)`, `useClaudeSignIn(onDone, vendor, enabled, accountId)`, the `note` prop (Task 5); `AccountsEditorProps` and `AccountRowProps` (Task 6) fed by `AgentAccountsSetting` (Task 8) and the gallery (Task 13); `ACCOUNT_NOTICE_KIND`, `readAccountNotice`, `sameAccountNotice` (Task 10); `SwitchDeps`, `handleAccountSwitched` (Task 12). `AgentSession.accountId` is optional and `""` is the built-in account, while notices carry `<vendor>-default`: `isBuiltinAccountId` treats both as the built-in one everywhere.

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-10-23-agent-accounts-settings-ui.md`. Two execution options:

**1. Subagent-Driven (recommended)** - a fresh subagent per task with a review between tasks (`superpowers:subagent-driven-development`). The tasks are in dependency order and each ends in a commit, so a failed review stops at a clean point.

**2. Inline Execution** - run the tasks in one session with checkpoints (`superpowers:executing-plans`).

Neither has been started. Review the "Decisions" list at the top first: each is a place the current UI did not settle, and each is cheap to change before Task 6 (the list), Task 7 (the form) and Task 12 (the toast) are built.
