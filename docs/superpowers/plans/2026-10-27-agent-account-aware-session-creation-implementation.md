# Account-aware session creation and startup failover

Date: 2026-10-27. Status: implemented (see the execution record at the end).

## Problem and observed evidence

A person reports that a limited built-in account prevents creating a new session even when another account has capacity. New-session creation must not depend on successfully opening the built-in account before the existing account policy can select another account.

Code inspected at origin/main `34c0e4ec2a6a32c3d984da7a53a262ca31dc682b`:

- `internal/agenthub/service.go`, `CreateSession`: constructs OpenSpec, opens endpoint, applies sandbox/subagents, calls `openTracked(...adapter.Open...)`, inserts a session without AccountID, then attributes runtime usage to BuiltinAccountID. It does not select or fill an account before Open.
- `internal/agenthub/account_switch.go`: `switchAccountForTurn` refreshes/decides/moves accounts at turn entry, after a session must already exist. `fillAccountSpec` resolves account home and generation for later opens, with a legacy fallback to built-in for missing account rows.
- `internal/agenthub/accountpick.go`: PickAccount already owns thresholds, order, cooldown, reset handling and the enabled-policy gate. Reuse this policy, rather than adding a separate account router.
- `internal/bridge/errors.go` already maps ErrAllAccountsLimited to public `agent_usage_limit`; preserve that contract where it describes the outcome accurately.

This establishes a creation-path gap, not a measured explanation of a particular live Claude refusal. No live account, login, quota probe or model request was used while preparing this plan. Implementation must reproduce both a known-exhausted built-in account and a typed quota refusal during Open with fake adapters.

Read first: root AGENTS.md; `docs/agent-reference/agent-hub-accounts.md`; vendors/sessions and lifecycle guides; `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`. Read the MCP guide only if public error publication or tools change.

## Required behavior

| Situation | Result |
| --- | --- |
| Switching enabled; built-in known exhausted; eligible added account exists | First Open uses the eligible account; built-in Open is never attempted |
| Switching enabled; built-in healthy | Keep existing sticky/default preference; do not rebalance merely because another account has more capacity |
| Selected account refuses Open with a positively identified quota error | Before any prompt, clean up the attempt and try the next eligible account once, under the same policy |
| All signed-in accounts genuinely exhausted | Return existing public usage-limit error with truthful reset information; open no known-exhausted account |
| Alternatives signed out, cooling down, deleted or otherwise unavailable | Do not claim all quotas are exhausted unless supported by facts; return a safe, accurate existing error or publish a narrowly defined new code |
| Switching absent/disabled | Preserve current built-in behavior; do not silently enable rotation or accept terms for the person |
| Vendor without multi-account support | Preserve current behavior |
| Unknown/stale usage below threshold | Unknown capacity is not zero capacity and not proof of availability; follow existing eligibility semantics |
| Cancellation, sandbox/grant refusal, bad model/config, temp/DB error | Stop and clean up; do not rotate accounts to work around the error |

Scope covers new top-level sessions and fresh children through the same service entry point. Children retain existing workspace, folder, mode/model approval and depth restrictions. A child does not automatically inherit its parent's account, history or session override. Existing-session turn failover remains unchanged except for shared helpers and correct initial account state.

Explicit credential configuration wins. In particular inspect the configured Codex API-key path and its precedence: do not silently replace an explicitly selected API credential with a subscription account. Document/test which credential source makes account rotation applicable. Never infer credential source from a quota error alone.

## Task 1 — Lock down the regression and actual launch order

Files: `internal/agenthub/account_switch_test.go`, a focused new `account_create_test.go`, current CreateSession tests, adapter startup-error tests as needed.

1. Extend the existing accountRig/fake adapter so Open records AccountHome, shared paths, generation and attempt count, and can refuse per account before returning a runtime.
2. Add a failing regression: enabled policy; signed-in built-in ExhaustedUntil in the future; signed-in added account below threshold; built-in Open would fail. Assert creation succeeds, only the added account is opened, no Prompt/Load/Fork is called, and row/response/runtime account agree.
3. Repeat using a reached usage window, weekly threshold, expired reset and empty built-in session AccountID. Include built-in row absent from a persisted account list; inventory existing account-list/built-in synthesis helpers before deciding how to normalize that input.
4. Inspect draft/model-catalog/sign-in preflight and the real call graph before CreateSession. Prove whether any built-in-only preflight prevents the create request from reaching Go. Capture this as a regression if present; do not assume the backend fix alone resolves that path.

Acceptance: tests fail for the intended account-selection defect, not an unrelated sandbox or policy fixture error.

## Task 2 — Prepare an initial account using the existing policy

Suggested file: `internal/agenthub/account_create.go`; shared policy helpers remain in their existing owning package.

1. Introduce a small internal initial-account preparation result: selected canonical id, persisted AccountID spelling, account generation and the account-scoped launch fields. It grants no capability and contains no credentials.
2. Select before endpoint/process creation using the existing policy/order/eligibility implementation. A fresh session has no previous-switch history; do not manufacture hold/return-gap state. Decide on Switch/AllLimited, never Target != Current.
3. Normalize built-in `""` versus BuiltinAccountID consistently; retain empty persisted AccountID for built-in unless the existing repository contract explicitly requires otherwise. Handle missing built-in rows using the existing canonical construction, without inventing quota or sign-in readings.
4. Distinguish policy disabled from policy/read failures. Preserve disabled behavior, but an enabled selection that cannot be safely resolved must not silently launch an exhausted built-in account. Define safe error handling and test it without changing unrelated turn-time semantics.
5. Resolve selected account home/shared paths/generation from current owning service records. Reuse established home preparation and sandbox carving. Do not use `fillAccountSpec`'s missing-account fallback for an explicitly selected added account: deletion/read failure must cause revalidation or a controlled error, not an unannounced built-in launch.
6. Reserve the account for the opening session before it can be removed. Coordinate selection/reservation with RemoveAccount's check/delete boundary; a map write alone with a check-then-delete race is insufficient. Release reservation on every failure, retain it on successful publication. Hold no service/DB mutex across vendor, endpoint, filesystem or user interaction work.

Acceptance: no vendor process, implicit sign-in, startup prompt or mandatory usage probe is needed to choose a known-eligible account. Reuse cached readings; absence of a reading does not trigger an unbounded startup probe.

## Task 3 — Integrate preparation with CreateSession publication

Files: `internal/agenthub/service.go`, `account_switch.go`, runtime usage publication helpers; tests for creation and account removal.

1. After caller/workspace/model gates and before Open, prepare the initial account and apply its fields to the same OpenSpec used by sandbox, MCP endpoint and runtime preparation. Build the jail for the selected account, not for built-in followed by an environment override.
2. Use existing `openTracked` and lifecycle admission paths. Account preparation must not introduce another runner, bypass release seals or widen grants. Do not call moveSession for an uninserted session.
3. Insert the session with the successfully opened account binding. Publish sessionAccount/runtime usage with the actual selected id and immutable generation, not BuiltinAccountID unconditionally. Response, first-turn attribution, Environment and post-restart resume must all agree.
4. Do not emit an existing-session account-switch notice for a fresh session that never ran on built-in. Do not create a fictitious move in account-move usage reports. Seed hold/previous state only if the existing product contract requires it, with an explicit test.
5. Cover failures after Open: model/effort configuration, inherited mode application, DB insert and publication. Close runtime, endpoint, temp artifacts and reservations exactly once; publish no successful session/runtime/account/usage state. Preserve the original meaningful error.

Acceptance: first creation and later resume use the same account identity; selecting a different account never changes model, effort, mode, profile or task ownership.

## Task 4 — Bounded failover for an unexpected startup quota refusal

1. Inventory adapter errors for Open. Reuse ErrAgentUsageLimit where positively identified. If a vendor exposes only a generic process failure, add vendor-specific classification only from verified structured evidence or captured sanitized fixtures. Do not match broad words such as “limit”, arbitrary stderr or model-generated text.
2. After a typed quota refusal before Prompt, mark only the attempted account using existing exhaustion/reset rules. Cancellation and unrelated failures must not mark it exhausted or cooling down as a quota fact.
3. Re-evaluate remaining candidates under current enabled policy, excluding attempted ids; cap attempts to the finite candidate set captured for this creation. Re-read policy/account identity before each attempt; newly added accounts must not extend the loop indefinitely. Stop on cancellation or disabled policy.
4. Fully close every failed attempt, including any partially opened vendor process, endpoint/token and private temp state, before another attempt. Require the adapter's failed-Open cleanup contract or fix it first. Rebuild account-specific OpenSpec/jail and endpoint state for each attempt; do not reuse stale identity/tokens.
5. Never resend a prompt or run the turn's onLimitRefusal follow-up here. No account is tried twice. An unknown/reset-free failure uses existing bounded exhaustion semantics, not a guessed reset timestamp.
6. Return truthful aggregate outcomes: all-exhausted only when every relevant signed-in account is known exhausted; otherwise preserve a safe unavailable/auth/startup error. No raw command, home path, email, token or stderr in public errors/metadata.

Acceptance: built-in unexpectedly refuses startup, second account succeeds, exactly two Opens and zero Prompts; all-limited and mixed failure cases terminate with correct cleanup and classification.

## Task 5 — Remove any UI/catalog dependency on built-in availability

Files to inspect: `frontend/src/modules/agenthub/session/useAgentHubController.ts`, draft/vendor state and config-catalog hooks, `createFailure.ts`, corresponding Go catalog services/adapters.

1. Separate global vendor installation/configuration failures from account-specific quota/auth availability. A built-in-only status must not disable creation when policy can select an eligible added account.
2. Keep account choice authoritative in Go. The UI may show actual selected account from the service result; it must not select an account via unchecked input or reimplement eligibility.
3. If catalog bootstrap actually requires an account, reuse a safe account-aware read preparation path. Catalog reads must not spend a prompt, create/publish a user session, change a session binding, consume an account-switch hold or mark usage based on an unrelated catalog error. Keep a usable existing model selection available while optional refresh fails, if supported by current UI contracts.
4. Preserve existing public error codes where accurate. For any new code update bridge mapping, keptCodes/redactedOnPurpose and boundary tests. No AppService signature change unless necessary; regenerate committed bindings if one changes.

Acceptance: an instrumented UI creation flow reaches the backend and opens the alternate account while built-in is limited; all-limited copy is distinct from sign-in or “runtime not installed”.

## Task 6 — Race and regression matrix

Use deterministic barriers, fake clock and fake adapters; no sleep-based assertions or real paid requests.

- Policy absent/disabled/enabled; unsupported vendor; explicit API-key source.
- Built-in healthy/threshold/reached/refused/signed out; alternate healthy/no reading/expired reset/signed out/cooldown; all exhausted versus none eligible.
- Order respected; absent built-in record; builtin id/empty id; weekly threshold and reset semantics unchanged.
- Top-level and fresh child; parent on an added account; child scope/mode/model restrictions unchanged.
- First selected added account refuses quota; second succeeds; generic startup error stops; no duplicate candidate, no prompt replay.
- Remove or replace account between selection/reservation/Open/publication; generation changes cannot be attributed to an older account. Two concurrent creates; cancellation at each boundary; policy disabled mid-retry.
- Endpoint/temp/Open/config/mode/insert failure; partial-process cleanup; no orphan active endpoint/runtime/reservation or false session-created event.
- First-turn usage and restart/resume match the successful startup account; fresh selection produces no fictitious account-move report.
- Existing turn-time failover, disabled-policy behavior, vendor-specific credential selection and PR343 writer-stop/admission tests remain green.

## Task 7 — Integration, validation and delivery

Start from freshly fetched main. This plan was prepared from an older primary checkout using git-show of origin/main; do not implement on that old checkout blindly. H3b PR350 edits the same CreateSession/OpenSpec/publication region. Prefer landing/rebasing against its reviewed implementation before wiring this fix, or coordinate one owner for that region. This bug fix does not depend on activating H4/H5 and must not modify the harness resolver to select accounts. Preserve H3's single prepared harness/endpoint snapshot, applied-state publication and cleanup; each retry must remain coherent with that pipeline.

Suggested delivery: one focused PR for tests + initial selection/publication + bounded startup fallback; add a separate UI/catalog PR only if Task 1 proves an independent blocker. Do not split away necessary failure cleanup, attribution or race protection from the backend fix.

Run focused tests first, then required checks:

```sh
go test -race ./internal/agenthub ./internal/preferences ./internal/bridge ./internal/storage
go test ./internal/arch/
go test ./internal/... ./cmd/...
```

If UI changes, run `cd frontend && bun test --isolate` and `bun run build` from frontend. Build frontend assets before checks that need the embedded bundle. Use Go >= 1.25 and Bun. Fix baseline/toolchain issues transparently; never bypass a verifier. Do not use live vendor credentials as an automated fixture.

Update the accounts guide: creation now selects before Open, initial selection is not a move, bounded startup refusal behavior, and remaining vendor-classification limitations. PR description must list actual tests and whether verification used mocks, a local app or real accounts. A real-account smoke test is optional only with the person's available environment; do not claim it was run from fake-adapter results.

Completion: with enabled policy and an eligible added account, creating a session cannot depend on built-in opening successfully; all persisted/runtime/UI usage identity matches the account actually opened; failure paths release all staged resources; existing scope, credential and lifecycle guards remain intact.

## Execution record

Built on `origin/harness/h3a-snapshot` (21a80491), the tip that holds H2c, H3a and H3b: those three PRs were merged into their stack bases and are not in `main`, so a branch from `main` would not have the prepare/open/publish pipeline this fix goes through. Changes: `account_create.go` (chooser, hold, bounded fallback, `openNewRuntime`), `CreateSession` (chooses first, publishes the opened account), `accountInUse`, `fillFromAccount`/`markExhausted` extracted from the turn path, `BuiltinCredentialPin` (Codex, wired in `main.go`). Tests: `account_create_test.go` (reproduction first, then the matrix), vendor hook extensions, a harness-coherence retry test; older tests that created a session after filling the built-in account now create it first (they test turn-time moves). UI: no built-in dependency found in the create path; one `createFailure` regression test.

Not done / not verified: real-vendor Open refusal behaviour (fake adapters only); turn-time failover still ignores an API-key pin; an unreadable preference is treated as switching off, as at a turn.
