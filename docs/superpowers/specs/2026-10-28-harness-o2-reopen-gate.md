# Harness O-2: a reopen with no applied reference refuses a workspace-owned executable

Branch `feat/harness-o2-reopen-gate` (base `b33f4391`). Source: the owner's acceptance of the R1a recommendation, Option B with E
(`docs/reviews/2026-10-28-r1a-fc-o2-dirswap-review.md` section 2). A hard prerequisite of AP4. Production stays inert; no executable
proposal is enabled and `ErrExecutableNotReady` is untouched. Not independently reviewed.

## Summary

- **The gap.** A session with no stored applied reference (created while no harness was wired, or whose reference an inert build deleted) had no
  baseline, so the launch gate (`previous != nil && AdoptsNewExecutables`) could not fire. If the workspace profile W owned a server or skill folder,
  its first reopen adopted it silently and wrote a reference that blessed it (R1a probes 1 and 2).
- **The rule (B).** `harnessGate` now takes a `launchKind`. On a **reopen** with no stored reference and `len(snap.Ref.WorkspaceOwned) > 0` it returns
  the existing `ErrHarnessNewSessionRequired` (published code `harness.newSessionRequired`, fixed sentence). It is stateless and reads only: no row
  is written, no reference is stamped from today's W (Option C stays rejected), the session, transcript and every other reference are untouched, and a
  running session is left alone. It states only a code fact (legacy launches never merged W); it claims nothing about W being approved.
- **Where reopen is signalled.** `launchKind` is an explicit argument of `prepareLaunch`, `prepareLaunchWithin`, `prepareLaunchOnce` and
  `harnessGate`, so "no previous" cannot be read as "new" by accident:
  - `launchCreate`: `openNewRuntime` (CreateSession, including the account retry loop). Unchanged behaviour.
  - `launchReopen`: `resumeRuntime` (resume, reload, account-move reopen), `rewindRun.openForked` (both rewind opens), and
    `preflightHarness` (reload in `recovery.go`, the account-move preflight in `account_switch.go`, the rewind preflight in `rewind.go`).
- **Unchanged on purpose.** An unreadable row is `ErrHarnessUnavailable` (it never reaches the new rule). A vendor-only change, groups, budgets or a
  W with no owned executable reopen as before and write their reference. A suppression-only W (a deny entry hiding a vendor server) is not an owned
  executable and is not refused here.
- **Presentation (E).** `HarnessStatus` maps "no applied reference and `len(Desired.WorkspaceOwned) > 0`" to `new_session_required` instead of
  `not_applied`, so the window says it before the launch refuses. The status already reaches the window through `harnessControls.sessionState`
  (`NewSessionRequired = State == HarnessNewSessionRequired`) and the frontend already renders `newSessionRequired`; no frontend change.
- **Not changed (open owner question).** Whether an inert build should keep deleting existing references (`recordHarnessApplied` with a nil snapshot,
  `rewind_store.go` with `HarnessRef == nil`). The rule makes it less urgent, because absence is judged conservatively either way.

## Tests (`internal/agenthub/harness_o2_reopen_test.go`, written first)

On `origin/main` the new tests fail (10 of the 13 top-level tests; the three controls pass); with the rule they pass. They cover: every cold reopen,
reload of a running session (runtime, endpoint and rows untouched, still answers), the rewind preflight (running session left alone) and the rewind
fork's own launch gate, the account-move preflight (session stays on its account, runtime not closed) and the reopen after a move, W with groups,
budgets, no owned executable, a vendor-only server or no profile (reopens and writes its reference), create with a W-owned server, an unreadable row,
the two R1a probes inverted, the status mapping (including the unreadable and vendor-only cases), and "a refused reopen writes nothing".

Mutation checks: removing the rule fails 10 tests; passing `launchCreate` at `resumeRuntime` fails 5, at the rewind fork 1 (D), in
`preflightHarness` 3 (B, C, E); passing `launchReopen` at create fails 1 (H).

## Limits

- Evidence is the agenthub test launch environment (real SQL store, scripted vendor), not the composed app, a real vendor or the native window.
- A session with no reference in a workspace whose W owns an executable cannot reopen; its history stays readable. The person opens a new session.
- `HarnessStatus` is not exercised through `internal/app` in a new test; its mapping to `SessionState` is a one-line comparison already in place.
