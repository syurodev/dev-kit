# Agent Hub — several vendor accounts, switched at the start of a turn

Status: design approved in conversation 2026-10-22. Not built. Builds on
`AGENTS.md` ("Agent Hub", "Agent customization and recovery", "A parent knows why
a child stopped…") and `2026-09-24-codex-agent-hub-design.md`.

## Problem

Agent Hub runs one sign-in per vendor. Claude sessions use the machine's own
`~/.claude` and keychain (`ShareHome`, the default); Codex uses DevKit's
`CODEX_HOME` with one ChatGPT sign-in or one API-key account. When that account
reaches its usage window the turn is refused (`usage_limit`, `rate_limited`),
the session goes idle with a banner, and the person has to sign out and in
elsewhere by hand. People who hold more than one subscription cannot spread
their work across them.

Community tools do this from outside (claude-swap, codex-rotator, opencode
plugins, proxies). What they teach, and this design uses: a configurable
percentage threshold beats waiting for an error (Claude can spill into paid
"extra usage" before any 429); switch at a safe boundary, not mid-request; a
process keeps the token it started with; pin a session to one account;
cool an exhausted account down and probe it again; when every account is
spent, say when the first one comes back.

## Goals

1. A person can sign in several accounts per vendor (Claude, Codex) and order
   them.
2. At the **start of a turn**, a session moves to the next eligible account
   when its current account is at or over a configured percentage, or was
   refused for a real limit. A turn already running is never interrupted.
3. The conversation continues on the new account (same native conversation).
4. DevKit never holds a credential: each vendor CLI keeps its own login in a
   home directory DevKit gives it.
5. Off by default. With no setup, behaviour is exactly today's.

Non-goals (v1): opencode, API-key accounts, polling a usage endpoint for idle
accounts, switching inside a running turn, syncing accounts between devices,
any proxy that carries a token, an MCP tool that reads or edits accounts.

## Decisions taken in conversation

| Question | Decision |
| --- | --- |
| Switch inside a turn? | No. Only when a new turn starts. |
| How is a vendor run? | The official CLI, spawned by DevKit with a per-account home. DevKit never calls a vendor API with a subscription token. |
| Conversation continuity | Share the native conversation store between account homes; the spike (Phase 0) decides if it works. |
| Policy | Percent threshold + the person's order. Not "most quota left". |
| A real limit refuses the turn | A **setting**, `onLimitRefusal`, default `none` (do not resend). |

## Model

### Accounts (`internal/agentaccounts`, a leaf; migration 45)

Table `agent_accounts` — local, plaintext SQLite, **not synced, no credential**:

| column | meaning |
| --- | --- |
| `id` | slug of the label, unique |
| `vendor` | `claude` \| `codex` |
| `label` | what the person calls it ("Work", "Personal") |
| `home_dir` | `<data>/agent-homes/<vendor>/accounts/<id>` (empty for the built-in default) |
| `signed_in` | the vendor's own `login status` said so at last check |
| `exhausted_until` | Unix seconds; 0 = not marked |
| `usage` | JSON `[]LimitWindow` as last observed (see "Usage") |
| `observed_at` | Unix seconds of that observation |

Two **built-in** accounts exist without a row: `Claude · system` (the host's
`~/.claude`, today's behaviour) and `Codex · DevKit` (today's `CODEX_HOME`). A
session with no `agent_account_id` is on the built-in one. Deleting an added
account removes its row and its home directory (after the vendor's own logout
when it can run); a built-in cannot be deleted.

`agent_sessions.agent_account_id` (nullable, same migration) is the account the
session's process runs as.

### Policy (`preferences`, key `agent_account_policy`, revision/CAS like
`agent_model_profiles`)

Per vendor: `enabled` (default false); `thresholdPercent` (default 90, 50–100);
`weeklyThresholdPercent` (optional, same range, applies to the weekly windows
only); `order []accountId` (built-ins may be listed); `onLimitRefusal`:
`none` (default) | `resendIfNoProgress` | `continue`. Also `riskAcknowledgedAt`.
Get/set are UI-only bridge methods (`preferences:* agent_account_policy`). An
unknown id in `order` is ignored; an account missing from `order` is last.

## Choosing an account

`PickAccount(policy, accounts, current, now) (Decision)` is pure
(`internal/agenthub/accountpick.go`).

1. Policy disabled, fewer than two eligible accounts, or the vendor has no
   accounts → stay on `current`.
2. `current` is **fine** when it is not exhausted (`exhausted_until <= now`) and
   every known window is under its threshold. A window whose `resetsAt <= now`
   counts as reset (0 %).
3. Otherwise the target is the first account in `order` that is signed in,
   not exhausted, and has every known window under threshold. Unknown usage is
   eligible. Accounts with a dead sign-in are skipped and listed in the reason.
4. **Anti flip-flop**: after a switch the session keeps its new account for 5
   minutes whatever the numbers say (except a real refusal), and a session
   returns to an earlier account only when that account is fine again by a
   margin of 10 points under the threshold, or its window has reset.
5. No eligible target: if `current` is fine → stay; if every account is
   exhausted → refuse the turn with `agenthub.allAccountsLimited`, whose
   sentence names the earliest `exhausted_until`/`resetsAt`; otherwise stay and
   let the vendor answer.

The decision carries a human reason (`Weekly 92% ≥ 90%`, `refused: usage limit`)
for the transcript notice and the audit line.

### Where it runs

`Service.invoke`, after the busy check and before `beginTurn`: if `PickAccount`
says switch, `Service` (a) closes the session's idle runtime, (b) writes
`agent_account_id`, (c) records a `devkit`-sourced notice row ("Switched to
Personal — Weekly 92 % ≥ 90 %", drawn like the peer notices, never as the
person's bubble), (d) reopens through `resumeRuntime` with the new account's
home. A failure to reopen leaves the old account bound and the turn runs (or
fails) as before; it is logged and the notice says "could not switch".
Sessions created by an agent choose by the same rule at creation; they do
not inherit the parent's account. Resume/Reload keep the bound account.

## Usage

No new endpoint is polled. Every `Usage` update a session already receives
(Claude `_claude/rateLimit` → `limitsOfRateLimit`; Codex
`account/rateLimits/updated`) is merged into its account's `usage` by window
label (Claude sends one window per event) and `observed_at` is set. The merge
runs in `handleUpdate` beside the existing usage handling and must not block it.
An account that runs no session is not refreshed: the UI shows the age of its
numbers, and step 2 above treats an elapsed `resetsAt` as reset. This is a known
weakness, accepted for v1; polling is a later, separate decision (a vendor
may treat it as abuse).

## A real limit

`recordStop` (already classifying `usage_limit` / `rate_limited`) marks the
session's account: `exhausted_until` = the reached window's `resetsAt`, else the
stop's `retryAfter`, else now + 15 min (a half-open probe: the next turn may try
that account again; it is never trusted for longer than that without a number).
The refused turn ends as it does today. Then `onLimitRefusal`:

- `none` — nothing is sent. The session is idle; the next prompt the person
  sends starts on the next account.
- `resendIfNoProgress` — if the turn produced no agent output and no tool call
  (read from the turn's transcript rows), DevKit sends the same prompt once on
  the next account. Otherwise as `none`, and the banner offers "Continue on
  <account>".
- `continue` — DevKit sends one fixed message ("The previous account reached its
  usage limit; continue where you left off.") on the next account, as a
  `devkit`-sourced prompt.

A resend is never repeated: a second refusal ends the chain. It does not count
as a person's prompt for peer-hop purposes and never re-attaches PDFs twice
(the session already holds them).

## Isolation and continuity

Per added account (`accounts/<id>/`):

- **Claude**: `CLAUDE_CONFIG_DIR=<home_dir>`. The directory holds the account's
  own credentials and `.claude.json`. Conversation store (`projects/`) is a
  symlink to the built-in account's, so `session/load` finds the conversation
  after a switch. Settings people expect everywhere (`settings.json`, `skills`,
  `agents`, `commands`, `plugins`, `CLAUDE.md`) are symlinked from the built-in
  account **if the spike shows they are needed**. MCP login state is not
  shared (it belongs to the account).
- **Codex**: `CODEX_HOME=<home_dir>` with its own `auth.json`; `sessions/` (and
  whatever thread index `thread/resume` needs — spike) shared with `Codex ·
  DevKit`. `prepareCodexHome` still resets the policy files per process.
- The jail grants exactly the bound account's home read-write (and, for Claude,
  the shared store it links to). Another account's home is hidden
  (`DenyReadPaths`). `OpenSpec` gains `AccountHome`; adapters stop reading a
  single global home for sessions.
- Sign-in: the vendor's own login in a terminal tab run with the account's home
  (`AuthProvider.SignInCommand` takes the home; Codex's in-app sign-in
  likewise). No credential passes through DevKit. `login status` fills
  `signed_in`.

### Phase 0 — spike (gate; nothing else is built before it)

With the real runtimes, on a fake HOME, record the answers in a "Measured"
section added to this spec:

1. Claude: a conversation started under account A, then `session/load` under
   account B with `projects/` symlinked — does it resume with full history?
   Does the macOS keychain item name depend on `CLAUDE_CONFIG_DIR`, and does the
   jail need `AllowKeychain` for it? Is a symlinked `projects/` writable from
   inside the jail?
2. Codex: the same with `sessions/` shared — does `thread/resume` need more than
   rollouts (state db, session index)? Is a second `CODEX_HOME` signed in as a
   different ChatGPT account accepted (a 2026 article claims separate homes
   stopped working reliably; unverified)?
3. A refused-by-limit turn: the exact `Usage`/stop text each vendor produces
   for the reached window (so `exhausted_until` is read, not guessed).

If 1 or 2 fails, this spec is revised to **seed a fresh native conversation
from DevKit's transcript** on the new account (lossy, always works) before any
build. The rest of the design stands.

## Interface

Bridge (UI only; an agent or user-terminal caller is refused, like sign-in;
no MCP tool — `TestNoToolTouchesAgentAccounts`): `AgentAccountsList`,
`AgentAccountsAdd` (starts the sign-in), `AgentAccountsRename`,
`AgentAccountsRemove`, `AgentAccountsRefreshStatus`, `AgentAccountPolicyGet|Set`;
event `agentaccounts:changed`. Grants added with a test
(`TestNewContainerGrantsAgentAccounts`); every error code published
(`bridge.keptCodes` or `redactedOnPurpose`).

Frontend: new Settings → Agents page **Accounts & limits** (`agents-accounts`,
a seventh page; `settingsNavigation.ts`, keywords on its `SETTINGS_GROUPS`
entry). Per vendor: enable switch (first enable shows the risk dialog and stores
`riskAcknowledgedAt`), ordered account list with drag/arrow reordering, sign-in
state, usage bars with the age of each reading and its reset time, threshold
inputs, `onLimitRefusal` select. Session summary → Environment gains `Account`.
Toasts: switched (once per switch), all accounts limited (with the time).
Gallery: `?gallery#agent-accounts` (states: off, two accounts, one exhausted,
all exhausted, stale numbers, sign-in dead).

## Risk and terms

The feature drives the vendors' own CLIs, with logins the person created, in
homes DevKit only names. It does not read, copy or send a token. It can still
break a vendor's terms: Anthropic restricts subscription OAuth to Claude Code
and Claude.ai (the sources found say nothing about one person holding several
accounts, so that is unchecked), and other vendors may treat rotation to avoid a limit as abuse. So: off by default;
a dialog on first enable states that the person must use accounts they own and
check the vendor's terms; DevKit makes no claim that it is permitted; usage is
never polled on a schedule.

## Failure handling

- Home cannot be created / symlink refused → the account is added as
  `signed_in = false` with the reason; never half-usable.
- A refresh token dies (vendor says auth required) → `signed_in = false`,
  skipped by `PickAccount`, shown "Sign in again".
- Resume on the new account fails (`session not found`) → stay on the old
  account, notice "could not continue on <label>", and mark that pair
  `no-shared-history` so the next turn does not retry it.
- Two sessions race to switch → switching is per session; the account row is
  written under the store's existing transaction rules; `exhausted_until` only
  ever moves later.
- App restarts → `agent_account_id`, `exhausted_until` and `usage` persist;
  anti-flip-flop timers are in memory and reset.

## Testing

Go: `PickAccount` table tests (threshold, weekly threshold, reset elapsed,
unknown usage, order, anti flip-flop, all exhausted, dead sign-in); `Service`
tests with a fake adapter for switch-before-turn, never-mid-turn, reopen failure
keeps the old account, each `onLimitRefusal` mode including "no progress" read
from rows and a single resend; usage merge by label; `recordStop` marks
`exhausted_until` from `resetsAt` / `retryAfter` / 15 min; migration up/down on an
existing database; jail policy lists only the bound home;
`TestNoToolTouchesAgentAccounts`, grants test, `go test ./internal/arch/` (the new
package is a leaf). Real runtimes: `cmd/pdfe2e/accounts_test.go` (skipped unless
the usual `DEVKIT_*_E2E_*` variables are set) runs the Phase 0 questions as
tests. Frontend: pure tests for ordering/threshold validation and the usage-age
label; gallery for the page.

## Not verified at design time

Every behaviour of a real second account; keychain naming under
`CLAUDE_CONFIG_DIR`; whether a vendor accepts a resumed conversation after an
account change (and how large the cache miss is — a switch resends context on
the new account, which costs its usage); the exact wording of the limit
messages; bwrap on Linux.
