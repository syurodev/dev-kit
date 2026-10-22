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

Most of the accounts this is for are **team logins that several people use**, so
the usage a session sees is not the whole story: others spend the same window.
The design therefore has an active way to check an account's limit (see "Usage")
and treats DevKit's own observations as a lower bound for a shared account.

Non-goals (v1): opencode, API-key accounts, a background timer that polls usage,
calling a vendor's usage API with a subscription token, switching inside a
running turn, syncing accounts between devices,
any proxy that carries a token, an MCP tool that reads or edits accounts.

## Decisions taken in conversation

| Question | Decision |
| --- | --- |
| Switch inside a turn? | No. Only when a new turn starts. |
| How is a vendor run? | The official CLI, spawned by DevKit with a per-account home. DevKit never calls a vendor API with a subscription token. |
| Conversation continuity | Share the native conversation store between account homes; the spike (Phase 0) decides if it works. |
| Policy | Percent threshold + the person's order. Not "most quota left". |
| A real limit refuses the turn | A **setting**, `onLimitRefusal`, default `none` (do not resend). |
| Accounts several people use | Marked `shared`; their usage is **checked** through the vendor's own CLI (not only observed), on Refresh and before a turn when the reading is old. Never on a timer. |
| Claude has no official usage read | Ranked candidates, the spike picks one; DevKit never calls the usage API with the token. Without one, Claude accounts are "usage not checkable" and rely on the real-limit backstop. |

## Model

### Accounts (`internal/agenthub/accounts*.go`, migration 45)

Types and store live in `agenthub`, following `SQLStore`: `agenthub` already
imports `preferences`, and a separate package would have to import
`agenthub.LimitWindow`.

Table `agent_accounts` — local, plaintext SQLite, **not synced, no credential**:

| column | meaning |
| --- | --- |
| `id` | slug of the label, unique |
| `vendor` | `claude` \| `codex` |
| `label` | what the person calls it ("Work", "Personal") |
| `home_dir` | `<data>/agent-accounts/<vendor>/<id>` (empty for the built-in default). Beside, never inside, `agent-homes/<vendor>`: Codex's built-in home is `<data>/agent-homes/codex`, and a home inside it would be readable by every built-in Codex session (its deny list is carved to its own home), exposing the other accounts' `auth.json`. |
| `signed_in` | the vendor's own `login status` said so at last check |
| `shared` | the person says other people also use this login (a team account); default false |
| `usage_source` | `observed` \| `checked` — where the stored `usage` came from |
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
only); `usageMaxAgeSeconds` (default 300, 60–3600: how old a reading of a
`shared` account may be before a turn start re-checks it); `order []accountId` (built-ins may be listed); `onLimitRefusal`:
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

`Service.invoke`, after the busy check and before `beginTurn`: first
`refreshIfStale` (see "Usage": only for `shared` accounts whose reading is older
than `usageMaxAgeSeconds`, time-boxed at 5 s, a failure keeps the old reading,
flagged stale). `PickAccount` uses a reading as it is: a window's usage only
grows until its `resetsAt`, so an old reading above the threshold is still true;
an old reading below it proves nothing and the account stays eligible (the
real-limit backstop covers that gap). Then, if `PickAccount` says switch, `Service` (a) closes the session's idle runtime, (b) writes
`agent_account_id`, (c) records a `devkit`-sourced notice row ("Switched to
Personal — Weekly 92 % ≥ 90 %", drawn like the peer notices, never as the
person's bubble), (d) reopens through `resumeRuntime` with the new account's
home. A failure to reopen leaves the old account bound and the turn runs (or
fails) as before; it is logged and the notice says "could not switch".
A new session starts on the built-in account and, like every session, is
evaluated at the start of each turn, so its first turn chooses; it does not
inherit its parent's account. Resume/Reload keep the bound account.

## Usage: observed, and checked on demand

Two sources, never one alone for a shared account.

**Observed.** Every `Usage` update a session already receives (Claude
`_claude/rateLimit` → `limitsOfRateLimit`; Codex `account/rateLimits/updated`) is
merged into its account's `usage` by window label (Claude sends one window per
event), with `observed_at` set and `usage_source = observed`. The merge runs in
`handleUpdate` beside the existing usage handling and must not block it. It sees
only what this app's sessions spent: on a `shared` account it is a **lower
bound**, because other people spend the same window.

**Checked.** A per-vendor `UsageProbe` (`internal/agenthub/usageprobe.go`) asks
the vendor, through its own CLI under the account's home, what the account has
left. It returns the account-wide windows — other people's use included, which is
exactly what a threshold needs — and DevKit never reads a token to do it.

- **Codex**: `account/rateLimits/read`, an official app-server request added to
  check limits without sending a message (openai/codex#5302; later versions also
  return per-name buckets). A short-lived `codex app-server` in the jail with the
  account's `CODEX_HOME`: `initialize`, one call, exit — the shape of
  `ConfigCatalog`. Field names come from `codex generate-ts` of the pinned
  version (spike), not from memory. DevKit does not call it today (only the
  `updated` notification is parsed).
- **Claude**: the CLI answers a structured **`get_usage` control request** (the data
  behind `/usage`; SDK type `SDKControlGetUsageResponse`, marked experimental) when
  started as `claude -p --input-format stream-json --output-format stream-json`
  with the account's `CLAUDE_CONFIG_DIR`: DevKit writes one control line, reads one
  `control_response`, closes stdin. Measured (see "Measured"): no model call
  (`total_cost_usd` 0), about 1 s, utilization 0–100 and an ISO `resets_at` per
  window (`five_hour`, `seven_day`, per-model, `model_scoped`, `extra_usage`),
  server-side so it is the whole login's, and DevKit never reads the token — the
  CLI uses its own. The API is experimental ("may change or be removed in any
  release"), so the probe validates the shape and reports "usage not checkable"
  with the reason on any mismatch rather than guessing. Fallbacks, in order, if
  the shape changes: `claude -p "/usage"` (free, text, parsed by its three
  `N% used · resets …` lines), then a one-token probe turn whose
  `rate_limit_event.unifiedWindows` carries both windows (spends a little of the
  account's usage, so only on an explicit Refresh). **Rejected for v1**: DevKit
  calling the undocumented `api.anthropic.com/api/oauth/usage` with the stored
  OAuth token — DevKit would hold a subscription token and use it outside Claude
  Code (see "Risk and terms"), and community reports say the endpoint answers
  429 aggressively. If every method fails, Claude accounts show "usage not
  checkable", `PickAccount` treats them as unknown, and the real-limit backstop
  ("A real limit") is what moves a session.

A check replaces the account's windows wholesale when the vendor returns the full
set (Codex), merges by label otherwise, and sets `usage_source = checked`.

**When a check runs.** Never on a timer. (a) The person presses Refresh, for one
account or all. (b) At turn start, inside `invoke` before `PickAccount`, for a
`shared` account — the session's current one, or one `PickAccount` is about to
choose — whose reading is older than `usageMaxAgeSeconds` (default 300, the
cache time community tools settled on). Guards: single-flight per account; at
most one check per account per 60 s even if asked; at most 2 checks at once
app-wide; 5 s time-box at turn start (on timeout or error, keep the last reading
and mark it stale, never block the turn). An account that is not `shared` is
checked only by (a): its own sessions already keep the observed reading true.

**What the person sees.** Every reading carries its source and age ("checked 2 m
ago", "observed 40 m ago — others may use this account"). An elapsed `resetsAt`
counts as reset in `PickAccount` (step 2) whatever the source.

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

Per added account (`<data>/agent-accounts/<vendor>/<id>/`):

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
4. Usage checks (the shared-account requirement): Codex — the real response of
   `account/rateLimits/read` (`codex generate-ts` for the pinned version: window
   fields, per-name buckets, does it include other users' use on a team seat),
   its latency, and whether a second short-lived app-server on a home that a
   running session also uses races over `auth.json` (token refresh). Claude —
   try the three candidates against a real team account in order, record which
   returns account-wide numbers (not only this machine's), their cost in usage
   and latency, and whether any of them makes DevKit read the token (then it is
   out). Also: do two people's sessions on one team login show each other's
   spend in the window the vendor reports?

If 1 or 2 fails, this spec is revised to **seed a fresh native conversation
from DevKit's transcript** on the new account (lossy, always works) before any
build. The rest of the design stands.

## Interface

Bridge (UI only; an agent or user-terminal caller is refused, like sign-in;
no MCP tool — `TestNoToolTouchesAgentAccounts`): `AgentAccountsList`,
`AgentAccountsAdd` (starts the sign-in), `AgentAccountsRename`,
`AgentAccountsRemove`, `AgentAccountsRefreshStatus` (sign-in state),
`AgentAccountsRefreshUsage` (one account or all: the "Checked" probe, subject to
its guards), `AgentAccountsSetShared`, `AgentAccountPolicyGet|Set`;
event `agentaccounts:changed`. Grants added with a test
(`TestNewContainerGrantsAgentAccounts`); every error code published
(`bridge.keptCodes` or `redactedOnPurpose`).

Frontend: new Settings → Agents page **Accounts & limits** (`agents-accounts`,
a seventh page; `settingsNavigation.ts`, keywords on its `SETTINGS_GROUPS`
entry). Per vendor: enable switch (first enable shows the risk dialog and stores
`riskAcknowledgedAt`), ordered account list with drag/arrow reordering, sign-in
state, a "Other people also use this login" toggle (`shared`), usage bars
with the source and age of each reading ("checked 2 m ago", "observed 40 m ago —
others may use this account") and its reset time, a Refresh button per account
and for all (disabled with the reason while a check is guarded out; "usage not
checkable" for a Claude account when no candidate works), threshold inputs,
`usageMaxAgeSeconds`, `onLimitRefusal` select. Session summary → Environment gains `Account`.
Toasts: switched (once per switch), all accounts limited (with the time).
Gallery: `?gallery#agent-accounts` (states: off, two accounts, one exhausted,
all exhausted, stale numbers, sign-in dead).

## Risk and terms

The feature drives the vendors' own CLIs, with logins the person created, in
homes DevKit only names. It does not read, copy or send a token. It can still
break a vendor's terms: Anthropic restricts subscription OAuth to Claude Code
and Claude.ai (the sources found say nothing about one person holding several
accounts, so that is unchecked), and other vendors may treat rotation to avoid a
limit as abuse. A team login several people use adds another question: whether
the plan's terms allow one login to be shared at all is the team's to check, not
DevKit's. So: off by default; a dialog on first enable states that the person must
use accounts they are entitled to use and check the vendor's and the plan's terms;
DevKit makes no claim that it is permitted; usage is never polled on a schedule
(checks are on Refresh or when a shared account's reading is older than
`usageMaxAgeSeconds`, rate-capped), and a Claude check never uses the token
itself.

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
- A usage check fails, times out or is guarded out → keep the last reading,
  mark it stale in the UI, never block or fail the turn; the error is shown on
  Refresh only. A check never changes `signed_in` except when the vendor says
  auth is required.
- Someone else spends a shared account's window after the last check → the turn
  may still be refused for a real limit; the backstop ("A real limit") marks
  the account and the next turn moves. The first such turn is the cost of not
  polling.

## Testing

Go: `PickAccount` table tests (threshold, weekly threshold, reset elapsed,
unknown usage, order, anti flip-flop, all exhausted, dead sign-in); `Service`
tests with a fake adapter for switch-before-turn, never-mid-turn, reopen failure
keeps the old account, each `onLimitRefusal` mode including "no progress" read
from rows and a single resend; usage merge by label; `refreshIfStale` (only for
`shared`, only when older than `usageMaxAgeSeconds`, single-flight, 60 s floor,
2 concurrent, 5 s time-box that keeps the old reading, never blocks the turn)
with a fake `UsageProbe`; a Codex probe test against a scripted app-server
(`account/rateLimits/read`); `recordStop` marks
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
messages; bwrap on Linux; every field of `account/rateLimits/read` (seen only
through a PR description and a third-party type); whether any Claude usage-check
candidate works without DevKit holding the token; whether a check of a team
seat reports the whole login's use or only the seat's.

## Measured

Phase 0, in progress (2026-10-22). Machine: `claude` 2.1.308, `codex-cli` 0.147.0 (DevKit pins
0.163.0; re-check there before building the probe).

**Codex `account/rateLimits/read` — schema only (from `codex app-server generate-ts`, no account used yet).**
It is a request with no params. The response is
`{ rateLimits: RateLimitSnapshot, rateLimitsByLimitId: { [limitId]?: RateLimitSnapshot } | null, rateLimitResetCredits }`.
A `RateLimitSnapshot` is `{ limitId, limitName, primary, secondary, credits, individualLimit,
spendControlReached, planType, rateLimitReachedType }` and a window is
`{ usedPercent, windowDurationMins, resetsAt }` — the same fields `codexLimitWindows` already reads from the
`account/rateLimits/updated` notification, so one translator serves both. `rateLimitsByLimitId` carries the
per-bucket view (`limitName` becomes the label prefix there too). The same schema also lists
`account/usage/read` and `account/workspaceMessages/read`; whether either says more about a team workspace
than `rateLimits/read` is to be measured. Whether the numbers include other people's use of a team login is
still open (needs a team seat).

**Claude sign-in per home.** `claude auth` has `login`, `logout` and `status`, so a per-account sign-in is
`CLAUDE_CONFIG_DIR=<home> claude auth login`, and `claude auth status` fills `signed_in`.

**Still to measure (needs two real accounts of each vendor):** shared `projects/` resume for Claude, shared
`sessions/` resume for Codex and the smallest file set it needs, Claude's usage-check candidates, the
`auth.json` race, and whether a team seat's numbers include other users.

**Claude — resume under another home with a shared `projects/` (Linux, CLI proxy for `session/load`).**
Account A is the machine's own login; B is `CLAUDE_CONFIG_DIR=<spike>/claude-b` holding a copy of
`claudeAiOauth` only, with `projects` symlinked to A's. A turn started under A
(`claude -p`, session `28603a2c…`) was resumed under B with `--resume <id>`: B answered the word A had been
given, kept the same session id, and appended its turn to the **same** `.jsonl` (both turns in one file).
`claude auth status` under B reported signed in with only `claudeAiOauth` copied (`.claude.json` and `backups/`
are created in the new home by the CLI itself). B's credential file was not rewritten (no token refresh).
Limits of this measurement: A and B are the **same** team account, so server-side state tied to an account (and the
cache miss a different account would pay: B read 26 148 cached tokens) is not measured; Linux only (file-based
credentials — macOS keychain naming under `CLAUDE_CONFIG_DIR` is open); a CLI proxy, not `claude-agent-acp`
`session/load` through DevKit's jail (the accounts UI plan's e2e repeats it, including whether a symlinked `projects/`
is writable inside bwrap).

**Claude — usage-check candidates, all tried against the team account.**
1. `get_usage` control request over the CLI's stream-json input: works. `five_hour` 69, `seven_day` 16
   (percent, 0–100), `resets_at` ISO, `limits[]`, `extra_usage`; 0.9 s; `total_cost_usd` 0. **Chosen.**
2. `claude -p "/usage"`: works, no pty needed, free, 1.7 s; text only
   (`Current session: 69% used · resets Oct 22, 2:20pm (Asia/Ho_Chi_Minh)`, `Current week (all models): 16% used …`).
   Same numbers as 1. Fallback.
3. One-token probe turn, `stream-json --verbose`: emits `rate_limit_event` with
   `rate_limit_info.unifiedWindows.{five_hour,seven_day}.{utilization,resetsAt}`; cost $0.064, 1.6 s. Fallback of last
   resort. **`utilization` here is a 0–1 fraction (0.69 while `/usage` said 69%), and the event carries no top-level
   `utilization` when `status` is `allowed`.**
4. Status-line payload: not tried (interactive only; superseded).
Whether the numbers include a teammate's use of the same login is **not** verified (no second person): the
figures come from the claude.ai usage endpoint, i.e. per seat on the server, which says yes, but it needs a
teammate's concurrent use to be a measurement.

**Consequence for DevKit's existing code (to verify, not yet fixed).** `claude-agent-acp` 0.85.1 forwards the SDK's
`rate_limit_info` unchanged as `_meta["_claude/rateLimit"]` on a `usage_update` (and only when a turn has an assistant
usage). `parseRateLimit`/`limitsOfRateLimit` read only the top-level `utilization` as a **percentage** and ignore
`unifiedWindows`; their fixtures use 82/83. Against the real event above the summary card gets no window while
`status` is `allowed` and, when a top-level value does arrive, its scale is unobserved (the SDK type does not say).
The accounts plan therefore reads `unifiedWindows` (×100) for observed Claude usage.

**Codex — resume under another home with a shared `sessions/` (0.147.0, Linux, two `codex login` homes of one ChatGPT account).**
Two homes, each signed in by its own `codex login` (independent tokens, no copying). A turn run under A
(`codex exec`, thread `01a16257…`) wrote `sessions/2026/10/22/rollout-…jsonl` plus per-home state:
`state_5.sqlite`, `goals_1.sqlite`, `queue_1.sqlite`, `memories_1.sqlite`, `logs_2.sqlite`, `installation_id`,
`models_cache.json`, `skills/`, `plugins/`. Control: `codex exec resume <thread>` under B with nothing shared fails with
`thread/resume failed: no rollout found for thread id … (code -32600)` — the same answer DevKit already documents
for a never-prompted thread. With **only `sessions/` symlinked** to A's, B resumed the thread and answered the word
A had been given; B's turn was appended to the same rollout file, and A then resumed the thread again and still
had it. So the smallest shared set is `sessions/` alone; `state_5.sqlite` and the other databases stay per account
(B created its own). `sharedStoreEntries[codex] = ["sessions"]` stands. Limits: same ChatGPT account in both homes,
so what an account change does server-side is not measured; the cache miss on a different account is not measured;
a CLI proxy for `thread/resume`, not DevKit's jailed app-server.

**Codex — `account/rateLimits/read` (real, plan `plus`).** Works with a `codex app-server` started on the account's
home: `initialize`, `initialized`, one request, no model call. Answer:
`rateLimits{limitId:"codex", primary{usedPercent:0, windowDurationMins:300, resetsAt}, secondary{usedPercent:10,
windowDurationMins:10080, resetsAt}, credits, planType:"plus", rateLimitReachedType:null}`, `rateLimitsByLimitId{codex:{…}}`,
`rateLimitResetCredits{availableCount:0}`. `usedPercent` is a percentage 0–100 (an integer here), the same
fields `codexLimitWindows` reads from the `updated` notification, so one translator serves both. A team seat has not
been tried.

Note: `codex login` accepts `--device-auth` in this version for a machine without a browser; each home prints a
harmless warning ("could not create PATH aliases … under temporary dir") when its home is under `/tmp`.
