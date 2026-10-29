- **A vendor can have several accounts, and a session moves between them at the start of a turn** (spec
  `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`, its "Measured" section is what was run;
  `internal/agenthub/accounts*.go`, `accountpick.go`, `account_home.go`, `account_switch.go`, `account_usage.go`,
  `account_limit.go`, `claude_limits.go`, `turn_progress.go`, `account_manage.go`, `account_check.go`,
  `account_signin.go`, `account_policy_cache.go`, `account_probe.go`; migrations 45, 46; backend plans
  `docs/superpowers/plans/2026-10-22-agent-multi-account-failover.md` and
  `docs/superpowers/plans/2026-10-22-agent-accounts-window-backend.md`). **Backend, bridge and window UI** (the UI is
  its own bullet, **Accounts UI**, below; it has only been rendered in tests and the gallery). `Service.Accounts` / `AccountPolicy` / `AccountSharedHome` / `AccountsDir` /
  `AccountVendors` and the two events are set in `main.go`; nothing enables a vendor except writing the preference
  `agent_account_policy` (the UI-only `AgentAccountPolicySet`, below). With no policy entry for a vendor nothing is
  switched, so the feature is off and a session runs as before. The two notices are `session_info_update` rows carrying
  `_meta.devkit.accountSwitch` / `accountSwitchFailed` (`{from, to, reason}`, typed as `SessionUpdate.AccountNotice`),
  kept as ordinary rows, which the window now draws as a transcript line (**Accounts UI**). An account is a row
  (`SQLAccountStore`, no credential): the vendor CLI keeps its login in the account's own home. `AgentSession.AccountID`
  is `""` for the built-in account (`<vendor>-default`, `BuiltinAccountID`, empty `HomeDir`), which is byte-for-byte what
  sessions always ran as.
  - **Homes.** An added account's home is `<data>/agent-accounts/<vendor>/<id>` (`Service.AddAccount` creates it, below),
    beside and never inside the built-in Codex home `<data>/agent-homes/codex` (a built-in session's deny list is carved to its own
    home only). The built-in Claude account is the machine's own `~/.claude`
    (`main.go`; the Claude sandbox never reads `$CLAUDE_CONFIG_DIR`, so a built-in session uses `~/.claude` whatever
    DevKit's own environment says). `PrepareAccountHome` links
    only the conversation store, `sharedStoreEntries`: Claude `projects/`, Codex `sessions/` (the smallest set measured to
    resume; the sign-in, databases and `.claude.json` stay per account). It never replaces a real directory or a link to
    elsewhere, and refuses a home inside the shared store. `fillAccountSpec` fills `OpenSpec.AccountHome` /
    `AccountSharedPaths` from the session row, never from a tool, and returns the account id the spec runs as (the
    built-in one when the row is missing or there is no book); it records nothing, so the read-only `Environment()` can
    call it, and only `resumeRuntime` and rewind's `openForked` record the result (`recordSessionAccount`, the
    `sessionAccount` cache); both adapters then set `CODEX_HOME` /
    `CLAUDE_CONFIG_DIR` and carve exactly the account home and the shared store out of the data-directory deny list with
    `carveOutAll` (`protect.go`; `agent-accounts` is in `dataDirSecrets`, so one account's session never reads another's
    home). `carveOutAll` is not `carveOut`: a keep equal to a deny path is allowed (`carveOut` still denies it); a deny
    directory that does not exist and would hold a keep is denied whole; matching is lexical, so callers build keeps and
    denies from the same base string (a symlinked data directory or `/var` against `/private/var` stays denied, which fails
    closed). The built-in account's paths and `carveOut` are unchanged.
  - **Not an isolation boundary under a shared `$HOME`** (the default): an account's Claude session can still read the
    host's `~/.claude`, and so the built-in login, as built-in sessions always could, and the shared store means every
    account's session can read every conversation in it.
  - **`PickAccount` is pure.** Off (policy disabled, fewer than two accounts, `Current` not in the list) → stay. Else stay
    while the current account is fine: not `ExhaustedUntil` in the future and every window not yet reset (`ResetsAt` past,
    or 0 = no known reset stays live) under `ThresholdPercent` (default 90, 50–100), a weekly window
    (`isWeeklyLabel`) under `WeeklyThresholdPercent` when set. A move is held 5 minutes (`accountFlipHold`) unless the
    current account was refused for real (`ExhaustedUntil`); the target is the first account in the person's `Order`
    (unlisted ones after, input order) that is signed in and fine, and the account the session just left must be
    `accountReturnGap` (10 points) under the limit to take it back. Every signed-in account exhausted → `AllLimited` +
    `RetryAt`, and the turn ends with `ErrAllAccountsLimited` before anything runs. **Callers decide on `Switch` and
    `AllLimited`, never on `Target != Current`** (the early stays return `Current` as given, `""` for the built-in, while
    a decision names `BuiltinAccountID`); `Previous` is an account id, never `""`; `Current == ""` is the built-in. A
    reading is used as it is: usage only grows until a reset, so an old reading over the threshold is still true.
  - **Strategy and a limit per account** (`preferences.AccountVendorPolicy.Strategy` / `AccountThresholds`, no migration: both live
    in the `agent_account_policy` JSON; bridge `AgentAccountVendorPolicyState.strategy` / `accountThresholds`; the form's "Which account a
    session moves to" and one "<account> (% used)" field per added account). The thresholds still decide WHETHER a session leaves its
    account (a healthy account is never left for a better one, so there is no rebalancing); the strategy only ranks the accounts that are
    fine to move to (`bestTarget`, in the person's order): `order` (default, the first), `best` (the lowest fullest live window), and
    `consumeFirst` (the earliest known reset of a live weekly window, cswap's "consume first"). A tie, or no reading, keeps the order;
    an account with a reading ranks before one without. A per-account limit replaces `thresholdPercent` for that account (a candidate's
    included, with the return margin still applied), and `weeklyThresholdPercent`, when set, still wins for weekly windows. Creation
    (`accountChooser.first`) uses the same pick, so it follows the strategy too. The built-in account has no field on the page but may
    carry a stored limit and ranks by the same rules. An unset or unknown strategy reads as `order`.
  - **A model's own window counts only for that model** (`windowAppliesToModel`, `PickInput.Model`, `Service.modelForPick`). A window
    labelled `Weekly · <name>` (`Weekly · Fable`, from `unifiedWindows` or the usage check's `model_scoped`) is read only when the
    session's model (the `model` option of its last `config_option_update` row, read only when an account carries such a window)
    contains `<name>`, ignoring case and punctuation (an alias such as `opus` matches too); with no known model, or another one,
    it is skipped, for the current account and for a candidate alike. The plain `5-hour`, `Weekly` and `Extra usage` windows are
    unchanged, and a real refusal still marks the whole account (`reachedReset` reads every reached window). Account creation passes no
    model yet, so a model window does not count there.
  - **A new session chooses its account before anything opens** (`account_create.go`, `Service.CreateSession`; plan
    `docs/superpowers/plans/2026-10-27-agent-account-aware-session-creation-implementation.md`). Before this, creation opened
    the built-in account and recorded it, so an out-of-quota built-in account could keep a session from starting while
    another account had room. Now `accountChooser.first` asks `PickAccount` as for a never-moved session on the built-in
    account (no hold, no previous account): a healthy built-in account stays (no rebalancing toward an emptier one), an
    ineligible one gives way to the first eligible account in the person's order, and every signed-in account exhausted is
    `ErrAllAccountsLimited` (public `agent_usage_limit`) with nothing opened. The OpenSpec's `AccountHome`,
    `AccountSharedPaths` and `UsageAccountGeneration` come from that account's record (`fillFromAccount`, shared with
    `fillAccountSpec`), before the jail, endpoint and harness snapshot are built, so the sandbox, `AgentSession.AccountID`
    (`""` for the built-in account), the published `sessionAccount`/`runtimeUsage` and a later resume all name the account
    that opened. Choosing a first account is not a move: no notice, no `OnAccountSwitched`, no `accountSwitches` entry, no
    account-move usage row. Switching off (no book, no policy entry, disabled, an unreadable policy) is the old built-in
    launch; an enabled vendor whose book cannot be read fails closed (`ErrAccountsUnavailable`, public "Accounts are not
    available") rather than launching blind. A built-in account **pinned to a credential the person chose** (Codex's API
    key, `BuiltinCredentialPin`) switches nothing: added accounts are ChatGPT sign-ins and never use that key. Which
    credential applies is that setting only, never inferred from an error. A fresh child goes through the same entry point
    and picks for itself; it does not inherit its parent's account.
    **Held against removal.** The chosen added account is read again and recorded in `Service.creating` under `accountMgmt`
    (the lock `RemoveAccount` checks and deletes under), so a removal either finishes first, and the choice is made again
    among what is left, or sees `ErrAccountInUse`. `accountInUse` reads `creating`; `CreateSession` releases it on every exit
    and `sessionAccount` takes over at publication. No lock is held across the vendor, the endpoint or the filesystem.
    **Bounded startup failover.** Only an `Open` refusal typed `ErrAgentUsageLimit` (the adapter's own classification; no
    stderr or wording match) on a live context marks that account (`markExhausted`, the same rule as a refused turn: reached
    window reset, else the hint's retry time, else `accountProbeCooldown`), then the policy picks the next account. The
    candidates are those listed when the choice began and each is tried once, so an account added meanwhile never joins and
    the loop is finite; switching turned off mid-way, a cancellation, a generic process failure, a sign-in failure or an
    unprepared home each end the creation with the vendor's own error and mark nothing. Each attempt builds its own endpoint,
    harness snapshot, jail and temp directory (`openNewRuntime`) and the failed one is closed before the next; no prompt is
    sent, so none can replay. All signed-in accounts exhausted after refusals returns `ErrAllAccountsLimited`; a cooling-down
    alternative is not "exhausted", so the vendor's refusal is what is reported. The window is unchanged: it never
    pre-checks the built-in account (the draft's catalog read is optional and a failure only shows beside the model picker),
    and `agent_usage_limit` is not read as sign-in or "runtime not installed" (`createFailure.test.ts`).
    **Not verified:** whether the real Claude or Codex adapters refuse `Open` for quota at all (the Codex adapter raises
    `ErrAgentUsageLimit` only for a turn; the ACP runtime only from a prompt), so the startup fallback is exercised with
    fake adapters only. No live account, login or paid request was used. Turn-time failover still moves a session off an
    API-key-pinned built-in account; only creation respects the pin.
  - **The move** (`switchAccountForTurn` in `Service.invoke`, under the session's claim, before the runtime is resolved):
    never inside a running turn. `moveSession` installs a `s.resuming` barrier (waiting first for a resume in flight, as
    Reload does), closes the idle process, flushes the recorder, writes `agent_account_id` (`UpdateSessionAccount`) and
    updates the cache; `resumeRuntime` re-reads the row's `AccountID` after registering its call, so a caller holding a
    stale copy reopens where the session is bound now. The process is then opened in the new home at once. A failed move
    leaves the session where it was and the turn runs as before. A failed reopen is undone (`abandonMove`): the row goes
    back, the target is kept out for `accountProbeCooldown` (15 min), and an `accountSwitchFailed` notice is recorded (its `reason` is a fixed sentence per failure kind —
    folder, sign-in, process, other — never the error text, which could hold the home path or the vendor's stderr; the
    notice used to quote it); when the turn's own context ended, the account is not blamed (no cooldown, no notice). Only a move whose process
    opened is remembered (`accountSwitches`, for the hold and return rules) and announced. **One edge announces a move
    whose process never opened**: when the reopen failed and the move back cannot be written either, the row stays on the
    target, so `noteAccountSwitch` runs (the notice, `OnAccountSwitched` and `agentaccounts:changed` fire) — the row is what
    the window and the next pick read. Known gaps: a panic in vendor
    code inside the switch window can leak the `invoking` claim (same as the existing `runtimeFor` window); the
    pending-elicitation card of the closed idle process is not cancelled (as in Reload).
  - **Usage is observed, not polled** (`observeAccountUsage`, from `UpdateUsage` inside `handleUpdate`, only for a vendor
    with a policy entry, enabled or not — `vendorConfigured`, the same gate as `markAccountLimited` and
    `upgradeStopForReachedWindow` — so everyone else gets no `agent_accounts` row and no write; each limits update of an
    unconfigured vendor costs one session-row read and one policy read; windows merge into
    the account the runtime runs as, by label, and `mergeWindows` keeps the stored window when a reading of the same label
    and the same non-zero `ResetsAt` is lower (usage only grows within a window, and two sessions on one account re-send
    their own last-known snapshots), while a new reset or `ResetsAt == 0` replaces; `observed_at` is still refreshed by such a
    stale write; `usageSeen` skips an identical reading, a failed write forgets it so the
    next identical update retries, a deleted account is dropped quietly, and the built-in account's row is created on
    first use). Claude's windows come from `rate_limit_event.unifiedWindows` (`claude_limits.go`) as 0–1 **fractions**
    ×100 (a value over 1.5 is read as already a percentage; measured against Claude Code 2.1.308), and `status:
    "rejected"` forces `reached` on the named window (`applyRateLimitStatus`); **when the event carries `isUsingOverage`, a
    window above 100% is a `warning`, not `reached`, unless the event says `rejected`** — without that flag `percentStatus`
    still returns `reached` at 100% or more. This is a visible change for ALL Claude users, not only account failover: a
    window past 100% with `isUsingOverage` and no rejected status shows as a warning in the summary card instead of reached. Codex gives percent 0–100 (rounded and clamped on the live path too) and
    marks both windows `reached` when its snapshot carries a reached type. On a shared team login the numbers are a lower
    bound (others spend the same window). `reachedWindowFresh` (10 min) is measured from `ObservedAt`, which is neither
    refreshed by an identical reading nor by a stale one (`SaveUsage` leaves it when `mergeWindows` rejects every fresh
    window), so the backstop can miss a repeated identical one. A saved reading tells the page through
    `accountsChangedThrottled` (at most one `agentaccounts:changed` per 2 s). The policy read behind `vendorConfigured`
    is cached for 3 s (`account_policy_cache.go`; a policy write drops it), so a limits update of an unconfigured vendor
    costs one session-row read. The Summary's "Plan limits" do not read these account windows: they come from the session's own stored/stream usage, kept across updates by label until their reset (`mergeUsage`, vendors-sessions guide), so a session that has not yet seen a rate-limit event shows none even when its account has windows.
  - **A real limit** (`recordStop` → `upgradeStopForReachedWindow` → `markAccountLimited`). Only Codex returns
    `ErrAgentUsageLimit`. **Claude Code's real refusal wording is not measured** (a limit cannot be provoked on demand), so
    Claude's refusal classes as `failed`; `upgradeStopForReachedWindow` (only for a configured vendor) turns a plain `failed` stop into `usage_limit`
    (not retryable, `retryAfter` = the reset) when the account has a `reached` window with a reset ahead, observed within
    10 minutes. If the refused request was the first of its turn no `rate_limit_event` may have arrived (ACP sends the
    usage update only after an assistant message): then only the percent threshold moves a Claude session.
    `markAccountLimited` only for a vendor with a policy entry (enabled or not, so an opencode or mock turn creates no
    row): `exhausted_until` = `reachedReset` (the reset of a reached window at 100%, else the earliest reset among the
    reached ones, since Codex marks both windows reached), else the stop's `retryAfter`, else 15 minutes; only ever later.
    A 429 (`rate_limited`) marks the account the same way, and is never followed (`endedInUsageLimit` reads only
    `usage_limit`).
  - **`onLimitRefusal`** (`none` default, `resendIfNoProgress`, `continue`; `Invoke` → `runLimitFollowUp`): at most one
    follow-up, through `invoke` (so it is never followed in turn), and only when `PickAccount` would switch; quiet (the
    original error returned) on busy or all-limited. A peer-message turn (`invoke` with a peerRun) never gets one.
    "Progress" is an in-memory per-session flag set from the update stream (agent message / thought chunks, tool calls;
    `turn_progress.go`), reset when a turn starts, not read from transcript rows. `continue` sends a fixed sentence
    (`continueAfterLimit`) and a resend sends the same input; both are recorded as ordinary user prompts, and the accounts UI did not add a
    `devkit`-sourced one. Edges: the parent notice and `agenthub.resumeSession`'s refusal read the
    stored stop, so they can be stale once failover is on (the session would move on its next turn); an agent's
    `invokeAgent` can take up to two turn time limits (each `invoke` bounds itself); a follow-up whose own account move
    fails runs on the refused account and is refused again.
  - **Account management is the window's, and UI-only** (`account_manage.go`, bridge `agenthub_accounts.go`). Methods
    `AgentAccountsList|Add|Rename|Remove|SetShared|RefreshStatus|RefreshUsage|StartSignIn|StartBrowserSignIn|StartDeviceSignIn`
    (scopes `account:*` under `builtin.agenthub`) and `AgentAccountPolicyGet|Set` (`agent_account_policy` under
    `builtin.settings`; `acknowledgeRisk` sets `RiskAcknowledgedAt` on the server, a client cannot send it). The bridge guard is
    `uiOnly` and the service-level one is `requireWindow`; **both reject only a caller that is present and not
    `identity.KindUI`** (an unstamped caller passes; server mode is covered by the separate `inServerMode` check, not by the
    caller kind). **In server mode only `AgentAccountsList` and `AgentAccountPolicyGet` work**: every other account method
    and `AgentAccountPolicySet` answer `errAgentAccountsDesktopOnly`. An unknown vendor is refused. **No MCP tool touches accounts** (`TestNoToolTouchesAgentAccounts`), and `AfterAccountSignIn` has no caller
    identity on purpose, so it is called from exactly two places: the bridge's terminal-exit goroutine and `main.go`'s
    Codex `OnSignInCompleted` (in a goroutine, only when `r.AccountID != ""`) — never expose it elsewhere. Errors are
    published (`bridge/errors.go`; `ErrAccountFolderLeft` says a removal left the folder behind, `ErrAccountHome` carries no
    path), and a global `publicError` mapping now sends `agenthub.ErrPermissionDenied` from **any** subject to
    `permission_denied` (the settings-subject UI-only methods — model profiles, subagent limits — used to reach the window as
    `internal_error`). `ErrAllAccountsLimited` reaches the window as `agent_usage_limit` with its own sentence.
  - **Account lifecycle** (`AddAccount`, `editAccount`, `RemoveAccount`, serialised by the `accountMgmt` mutex; lock order
    `accountMgmt`, then `mu`). Ids are `<vendor>-<slug of the label>`; the canonical home is
    `<AccountsDir>/<vendor>/<id>` (`accountHomeFor`, which also validates what `Put` is asked to point at: absolute, clean,
    inside `AccountsDir`, no symlink on the way, no escape). Removal refuses an account a live process runs as or a resume is
    opening (`ErrAccountInUse`), deletes the row, and `RemoveAll`s **only the canonical home** (anything else, or a home that
    turned into a link, is left and reported as `ErrAccountFolderLeft`). `AccountVendors` is Claude and Codex only.
    **`AccountsDir` must be built from the same string as `ProtectDataDir`**: `main.go` calls `agenthub.AccountsDirOf(dataDir)`
    and `ProtectedPaths` uses the same `dataDir`, and `TestAccountsDirIsAnEntryOfTheProtectedList` pins that the folder is one
    of the deny entries (`carveOutAll` is lexical).
  - **Cooldown is not exhaustion** (`MarkCooldown`, `CooldownUntil`, migration 46). A real limit sets `exhausted_until`;
    `cooldown_until` is set ONLY by a failed reopen in the account's home (`abandonMove`, `accountProbeCooldown`, 15 min, and
    it fires `agentaccounts:changed`). A refusal that names no reset is not a cooldown: `markAccountLimited` gives it
    `exhausted_until` = now + 15 min. A cooldown only keeps an account out as a TARGET: if the current account is exhausted and the only alternative
    is cooling, the session stays and the vendor's own refusal shows. A signed-out CURRENT account is left for the best
    signed-in fine target; a sign-out alone is never `AllLimited`. `PickAccount` names the signed-out accounts it skipped in
    the reason sentence only.
  - **Signed out is learned, not guessed** (`noteSignedOut`, `noteSignedIn`): only a turn that failed with
    `ErrAgentAuthRequired` (not "not installed") sets `signed_in = false` on the account the turn ran as. A bad chosen
    Codex API key wraps both `ErrAgentAuthRequired` and `ErrAgentAPIKeyUnusable` and is marked too: for the built-in account
    a bad key reads as signed out, which is intended. A status refresh, a finished sign-in or a completed turn sets it back
    to true. The built-in Codex account's status check (`CheckSignIn` with no home) answers through `SignInStatus`, so the
    global API key counts as signed in; an added account's check asks `account/read` at its own home and never reads that
    key. `Put` writes `signed_in` only when the row is created; edits go through `SetLabel` / `SetShared` and
    `SetSignedIn`, so an edit cannot write back a stale sign-in state.
  - **Usage checks on demand** (`account_check.go`, `account_probe.go`). `checkUsage` is single flight per account, with a
    floor between vendor calls of 10 s for a manual refresh and 60 s before a turn, at most 2 vendor calls at once, a 30 s
    bound on one call, and a joiner never inherits another caller's context error. A running Codex session answers through
    `RateLimitReader` (no new process); Claude has no live reader, so a probe can still run a second Claude process on a home
    a session is using — **the two-process token-refresh race is unverified**. `refreshStaleForTurn` (start of `invoke`)
    asks only for a shared account that is signed in and stale, only with two or more signed-in accounts, in one 5 s box
    shared by both checks. `usageMaxAgeSeconds` (policy, 60–3600, default 300 s) is read here. **Stale means the last
    successful CHECK is older than that, or none has happened since the app started — not the last observation**: observed
    readings keep refreshing `observed_at`, which would hide other people's use of a shared login. The check time is
    in memory (`usageChecks.checkedAt`, set only when a check stored its answer, so a failed or empty check does not make
    the account look fresh), so after a restart a shared account is checked on its first turn. Probes run under `accountProbeSpec` (the data-dir deny list, the account home and shared store,
    the private/shared-home choice). **Claude** (`claudeCLI`, `claude_cli.go`): the adapter's `index.js` is started with
    `--cli`, so `claude --cli -p --verbose --input-format stream-json --output-format stream-json
    --no-session-persistence` takes a `get_usage` control request (`rate_limits.<window>.utilization` is **0–100**, not the
    0–1 of `rate_limit_event`, so `unifiedPercent` is not reused) and `claude auth status` (JSON, exit 1 when signed out);
    host credential variables are stripped from an added account's probe. **Codex**: `account/rateLimits/read` and
    `account/read` through a one-shot app-server (`oneShot` takes a spec).
  - **Sign-in into an account home** (`account_signin.go`). Claude: a terminal tab (`StartAccountSignIn`,
    `AccountSignInCommand`) with `CLAUDE_CONFIG_DIR` = the account's home (the host's value loses). Codex: in-app, at the home,
    file credential store, and port 1455, so only one Codex sign-in runs at a time; `SignInResult.AccountID` says which
    account finished and `agenthub:signInCompleted` carries it as `accountId` (`""` for the built-in path, which still calls
    `RecordAgentSignIn`; an added account does not appear in the Accounts list). `RefreshAccountStatus` reads the home's
    sign-in state through `SignInChecker`.
  - **Events** (`main.go`): `agentaccounts:changed` has no payload (the page refetches `AgentAccountsList`);
    `agentaccounts:switched` carries `sessionId`, `from`, `to`, `reason` and nothing else. `Service.OnAccountSwitched` fires
    from `noteAccountSwitch`, i.e. after the new process opened and never for an undone move (`account_events_test.go`).
  - **Not done / known gaps**:
    - A `devkit`-sourced prompt for `continue` (the window's side is built: see **Accounts UI** below). The hand-off
      notes the frontend followed:
      `agenthub:signInCompleted` also fires for added-account flows, carrying `accountId` — every existing handler (the
      Runtimes page toast, status, "Signed in") must filter on it; `agentaccounts:changed` is payload-free, and the ones caused by observed usage are throttled
      leading-edge only (at most one per 2 s, no trailing event, so the last reading can go unannounced; the page should also
      refetch on window focus);
      `agentaccounts:switched` is `{sessionId, from, to, reason}`; `SessionUpdate.accountNotice` and
      `SignInResult.accountId` are **not in the generated bindings** (hand-type them in the frontend); account ids are
      `<vendor>-default` for the built-in one, and a removed account has no label, so the UI needs fallback text;
      `AgentSession.AccountID` is PascalCase in the bound JSON while the account DTOs are camelCase; `usage` and `order`
      are non-null arrays in Go although the bindings type them `T[] | null`; `exhaustedUntil` / `cooldownUntil` may be past
      values (treat as not limited); the policy's `Order` may hold ids of removed accounts (tolerate them);
      `acknowledgeRisk` must only be set from an explicit user action.
    - **Not verified with real vendors**: a different account on the resumed side (server state, cache miss); macOS keychain
      naming under `CLAUDE_CONFIG_DIR`, and `AllowKeychain` (still on for Claude account sessions, so one can reach the
      built-in keychain item — measure before relying on isolation); a symlinked store inside bwrap; the two-process
      token-refresh race; whether a team seat's numbers include a teammate's use; the real `get_usage` and `auth status`
      shapes beyond the measured fields; Claude's real refusal wording. Tests use fakes (`account_*_test.go`); Phase 0
      resume checks were a CLI proxy, not DevKit's jail. The `main.go` wiring is not unit-tested (it needs the desktop
      host); it compiles under both build tags.
    - A removal racing an in-flight sign-in, a resume that has not reached `recordSessionAccount`, or a `moveSession` aimed
      at the account can leave an orphan home or a process that fails to open. A probe, status refresh or sign-in on an
      already removed account can likewise recreate its folder (an orphan): `accountInUse` does not know in-flight sign-ins.
      `AddAccount` does not remove the prepared folder if `Put` fails.
    - `prepareForSignIn` creates the home before the vendor and method are validated (server mode, unknown method).
    - `noteSignedIn` and `observeAccountUsage` cost an extra SQLite read per update/turn (the session row, the account row);
      a short-TTL cache of those is still to do before the feature is on for people who use Codex/Claude heavily.
    - `PickAccount` names skipped signed-out accounts only inside the reason string; a structured `Skipped []string` on the
      decision would serve the UI better.
    - From the failover plan (`docs/superpowers/plans/2026-10-22-agent-multi-account-failover.md`): the progress race at `kickPeers`; `continue` / a resend recorded as an ordinary user prompt; no
      `no-shared-history` marker after a failed resume; `SaveUsage` runs synchronously on the vendor's update goroutine
      (after the emit, before the recorder).
    - `PrepareAccountHome` has a TOCTOU between `noLinkBetween` and `MkdirAll`/`RemoveAll`: it assumes nothing else writes to
      the accounts folder.
    - A window with `ResetsAt == 0` stays live for ever (no known reset to expire it).
    - Claude's summary card shows both `unifiedWindows` windows for everyone (intended).
- **Accounts UI: Settings → Agents → Accounts & limits** (plan `docs/superpowers/plans/2026-10-23-agent-accounts-settings-ui.md`;
  gallery `?gallery#agent-accounts`). Frontend only: no Go changed. It reuses the Agents pages' own parts (`VendorSwitch`,
  the container/presentational split with compare-and-swap save, `ActionTooltip` icon buttons, the summary card's
  `role="meter"` bar, `ConfirmDialog`, `AlertDialog` + `Checkbox`) and adds no library, colour or layout primitive.
  - **Where it lives.** `frontend/src/modules/agenthub/accounts/` (pure `accountModel.ts`, `accountPolicy.ts`,
    `accountActions.ts`; `useAgentAccounts.ts` keys, hooks and `watchAgentAccounts`; presentational `AccountsEditor`,
    `AccountRow`, `AddAccountForm`, `UsageMeter`, `AccountPolicyEditor`, `AccountRiskDialog`; connected
    `AgentAccountsSetting`, `AccountSignIn`). Settings only mounts it from `AgentsPages.tsx` (`AgentAccountsPage`, page id
    `agents-accounts`, the same way it mounts `AgentEnvironmentPanel`), so `moduleBoundaryRatchet` stays at
    `settings -> agenthub: 3`. `VendorSwitch` takes `vendors` now; accounts offer Claude | Codex only (`ACCOUNT_VENDORS`).
  - **Bridge** `bridge/agentAccounts.ts` over the real bindings: `usage` and `order` come back `[]` not `null`
    (`accountOf`, `policyOf`), times are Unix seconds, the policy is replaced whole with its `revision`.
    `acknowledgeRisk` is sent only when the person accepted the terms dialog in that edit and is turning switching on.
    Hand-typed because not bound: `SignInResult.accountId`, `SessionUpdate.accountNotice`, the `agentaccounts:switched`
    payload.
  - **`agenthub:signInCompleted` carries `accountId`, and every handler filters on it.** `createSignInTracker(accountId)`
    acts only on events about its own account (`""` = the built-in sign-in); `useCodexSignIn(onDone, accountId)` /
    `useClaudeSignIn(onDone, vendor, enabled, accountId)` start the account flow instead; `AgentHubView`'s config-catalog
    refresh ignores an added account's event. A new handler of that event must check `accountId` too.
  - **Fresh without polling.** `AgentAccountsSubscriber` (mounted in `Workbench` beside `AgentSessionsSubscriber`) refetches
    the lists on `agentaccounts:changed` and on window focus (the event is throttled leading-edge: the newest reading can
    go unannounced), writes a moved session's `accountId` into the sessions store, and toasts "Moved to X" only when that
    session is on screen. Refresh usage has no "asked" flag in the answer: an unchanged `observedAt` is how the row says
    "nothing new" (`refreshNote`).
  - **The built-in account has no row on this page** (`addedAccounts`, applied in `AgentAccountsSetting`; Go is unchanged: it still
    lists, ranks and runs sessions on it, and `signedInCount`, notices and the summary card still read the full list). Its login lives
    under Settings → Agents → Runtimes, so `AccountsEditor` says that instead (`builtinAccountHiddenHelp`) when no listed account is built-in.
    Move up / Move down now save the order of the added accounts only, so the built-in id is dropped from the saved `order` and Go
    ranks it after them (unlisted accounts come last). `AccountRow`'s built-in branches stay for the gallery and for a later
    single sign-in place.
  - **Rows.** Order is the saved `order` then the rest (`orderedAccounts`); Move up / Move down save the whole policy at
    once (no drag and drop). A past `exhaustedUntil` / `cooldownUntil` says nothing; a window past its `resetsAt` is drawn
    empty ("reset since the last reading"). Removal shows Go's refusal inside the confirmation, and a removal that failed
    after the row was gone (folder left behind) shows its sentence on the page. The built-in account's sign-in is not
    drawn there (Go refuses it, `prepareForSignIn`): a signed-out built-in row says "Sign in to this account under Settings →
    Agents → Runtimes." and mounts none of the sign-in hooks, while "Check again" stays. Icon buttons carry the account's
    name in their `aria-label` ("Remove Work"); the tooltip text stays the bare action. The sign-in and Remove copy does not
    say where the login lands (the macOS keychain naming under `CLAUDE_CONFIG_DIR` is unverified).
  - **Two honest notes on the rules and the events.** (a) The first Move on a vendor with no stored policy writes a default
    policy entry (`enabled=false`, 90, 300, `none`) so the order has somewhere to live; Go's `vendorConfigured` is "has an
    entry", so from then on that vendor gets usage observation and `agent_accounts` rows. With no entry the usage meters stay
    at "No usage reading yet" until rules are saved or Refresh is pressed. Not changed here. (b) `agentaccounts:changed` can
    arrive before `agenthub:signInCompleted`, so a Codex row can flip to signed in (and unmount its panel, with its handler)
    before the success toast fires: the row is right, the toast can be lost. Not changed here.
  - **The terms flow is pure** (`accountPolicy.ts` `riskFlow`: `toggle`, `confirm`, `cancel`, `storedChanged`, `saved`, state
    `{enabled, askRisk, accepted}`), tested in `accountPolicy.test.ts`; `AccountPolicyEditor` only calls it. The terms warning
    (`TERMS_WARNING`) claims only what the design spec does: rotation to avoid a limit and sharing one login may be
    restricted; holding several accounts was unchecked.
  - **Transcript.** A move is the item kind `devkit_account_notice` (`readAccountNotice`, both `applySessionUpdates` and
    `initTranscript`; a copy that arrived while the page loaded is dropped by `dropRecordedChunks`), lands in the previous
    turn's `notes` and is drawn by `AgentTurn` through `AccountNoticeLine` (`transcript/AccountNotice.tsx`). The summary
    card's `Account` row (`EnvironmentInput.accountLabel`) is absent while `AgentSession.accountId` is empty or its list has
    not arrived.
  - **Not verified.** The UI has been rendered only by `renderToStaticMarkup` tests and the dev gallery. It has never been
    driven in the running desktop app (no session of this work started it) and never against real Claude or Codex
    accounts: sign-in at an added account's home, a real usage reading, a real move and the toast are untested outside
    fakes. The Remove-while-in-use refusal, the focus refetch and the throttle gap come from reading the Go side, not from
    running it.
  - **Not done.** A toast for "every account is limited" (it reaches the window as the turn's `agent_usage_limit` error, as
    before; `agenthub.allAccountsLimited` is still unpublished in `keptCodes`); a drag-to-reorder list; any account UI for
    opencode; "Refresh all"; usage polling.

- **Token usage report: Settings → Agents → Accounts & limits** (`internal/agenthub/turn_usage.go`, `usage_report.go`, `usage_source.go`;
  bridge `agenthub_usage.go` `AgentUsageReport`, scope `usage:report`, UI-only; frontend `modules/agenthub/usage/`; spec
  `docs/superpowers/specs/2026-10-26-subscription-harness-tuning-design.md`, phase 1a). Numbers and ids only, never a prompt, title or path.
  **Usage is re-derived from each turn's raw meta** with the vendor's current `NormalizeUsage`, because the stored `InputTokens` /
  `OutputTokens` / `CostUSD` are always serialized and turns stored before the normalizer read `quota.token_count` hold a false 0 beside
  real numbers in `RawMeta`; the stored fields are used only for a row with no raw meta. Every aggregate carries `Known` coverage per metric (DTO `known`):
  missing values do not increase coverage, real zeros do. Cells without coverage show `—`, incomplete sums say `(partial)`, and
  percentages require complete coverage of both operands. Model rows expose total coverage only; other cells show `—`.
  Fresh input is displayed beside cache read/write and output. The query explicitly refetches stale data on window focus.
  A turn whose normalized answer has no pointer
  field and only zeros is **unknown** (counted in `TurnsWithoutData`, never added as 0), so legacy input/output-only meta reporting zeros
  is indistinguishable from no data; explicit pointer counters at 0 remain known. `model_usage` is the per-model split and can include helper models that the turn total leaves out,
  so model totals can differ from the overall total; `PartialSplitTurns` counts incomplete, unequal or uncomparable splits.
  The warning does not attribute a mismatch to helper models without evidence. The panel says these are tokens, not a share of
  the subscription limit (how the limit counts cached reads is not known). Claude only; opencode and Codex have no usable usage rows.
  **Phase 1b:** migration 51 gives local accounts an immutable `usage_generation` (built-in: `builtin`). Each new recorded turn,
  including a billed failed turn, captures the actual opened runtime account/generation, start time and workspace just before
  `Prompt`; legacy turns remain unknown-account and retain Phase 1a's workspace fallback. Profile/override revisions are nullable
  until profiles exist. `ByAccount` partitions the same totals and coverage; current labels require an id + generation match.
  `AgentToolUsageReport` (`usage:tools`) and `AgentAccountMoveUsageReport` (`usage:moves`) are additional window-only reads under
  `agenthub:read`, never MCP tools or vendor probes. The first reads persisted host text-byte aggregates (see `mcp-tools.md`);
  the second projects account notices, at most 100 per page, default 30/max 90 UTC days, with filter-bound cursors. Only a host-stamped
  reopened-runtime notice can match the first subsequently started turn of the same account generation before the next move.
  Binding-only, failed, legacy and ambiguous moves stay qualified; absent accounting is not zero or proof of a cache miss.
  The UI shows each report's own error, UTC date filters, collection health and stale focus refresh. It does not poll or change failover.
  Server mode preserves the vendor's pure usage decoder through `ForServerMode`; creating sessions and signing in remain refused.
