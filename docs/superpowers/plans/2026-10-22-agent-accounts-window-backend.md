# Agent accounts — the Go side the window needs (Plan 2 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Everything the window needs from Go to add, sign in, list, refresh, reorder-by-policy and remove several vendor accounts: account lifecycle, on-demand usage checks (Codex `account/rateLimits/read`, Claude `get_usage`), per-account sign-in, the UI-only bridge with grants, published errors and an event, and the pre-enable gaps that Plan 1's reviews left open.

**Architecture:** Plan 1 (PR #301, merged first) is the dormant backend: account book, `PickAccount`, switch at turn start, usage observation, limit marking. This plan adds a thin `Service` layer on top (`account_manage.go`, `account_check.go`, `account_signin.go`), optional adapter interfaces (`UsageChecker`, `SignInChecker`, `AccountAuthProvider`, `AccountInAppSignIn`) implemented by the Claude and Codex adapters, and `internal/bridge/agenthub_accounts.go`. Every new entry point is **UI-only** (`identity.KindUI`) and no MCP tool touches accounts. Plan 3 is the frontend and starts from the bindings this plan regenerates.

**Tech Stack:** Go 1.25+, SQLite (`modernc.org/sqlite`, ONE connection), Wails v3 bindings (`wails3 generate bindings -clean=true -ts -i`), existing `internal/agenthub`, `internal/bridge`, `internal/preferences`, `internal/codexapp`, `internal/sandbox`.

**Spec:** `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md` (read "Measured"). **Plan 1:** `docs/superpowers/plans/2026-10-22-agent-multi-account-failover.md`. **Project notes:** the bullet "A vendor can have several accounts…" in `AGENTS.md` lists exactly what this plan closes ("Not done" and "Before the feature is turned on").

**Preconditions.** Work on a branch cut from `main` AFTER PR #301 merged (or from `feat/agent-multi-account-backend` if it has not: then rebase later). `go test ./internal/agenthub/ ./internal/arch/ ./internal/bridge/ -count=1` is green before you start.

**Conventions (from `AGENTS.md`):** Go tests `go test ./internal/agenthub/ ./internal/preferences/ ./internal/storage/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1`; vet `go vet -tags server ./internal/... ./cmd/...`; `gofmt -l internal main.go`. One connection to SQLite: never hold `s.mu` or a transaction while calling the book from the same goroutine in a way that issues a second query on `st.db`. Commit after each task, message ending `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.

**Facts measured against the real CLIs (do not re-derive):**
- The Claude Code CLI is started by DevKit as `<node> <claude-agent-acp dist/index.js> --cli <claude args>` (the adapter's `index.js` sees `--cli`, strips it and spawns the bundled native `claude`, or `$CLAUDE_CODE_EXECUTABLE`). So `Cmd{Path: l.path, Args: append(l.args, "--cli", …)}` works for both "bundled" and "on this machine" with no new resolution code.
- `claude -p --output-format stream-json` **requires `--verbose`**.
- `get_usage` control request, stdin line `{"type":"control_request","request_id":"u1","request":{"subtype":"get_usage","skip_behaviors":true}}`; closing stdin makes the CLI exit 0 after one `control_response`. Its `rate_limits.<window>.utilization` is **0–100** (NOT a 0–1 fraction), `resets_at` is an ISO 8601 string; signed out → `rate_limits_available:false`, `rate_limits:null`. Do NOT reuse `unifiedPercent` (it reads values ≤ 1.5 as fractions).
- `claude auth status` prints JSON `{"loggedIn":bool,"authMethod":…,"email":…,"subscriptionType":…}`, exit 1 when signed out.
- Codex `account/rateLimits/read` has no params; answer `{rateLimits, rateLimitsByLimitId, rateLimitResetCredits}`; `usedPercent` is an integer 0–100 in 0.147.0.
- The built-in Claude account is the machine's own `~/.claude`; added homes live in `<data>/agent-accounts/<vendor>/<id>`.

---

## File structure

| File | Action | Responsibility |
| --- | --- | --- |
| `internal/storage/sqlite.go`, `migrations_test.go` | modify | migration 46 (`cooldown_until`) |
| `internal/agenthub/accounts.go`, `accounts_store.go` | modify | `CooldownUntil`; `Delete`, `SetSignedIn`, `ReplaceUsage`, `MarkCooldown` |
| `internal/agenthub/accountpick.go` | modify | cooldown ≠ exhausted; skipped-account reason |
| `internal/agenthub/account_switch.go` | modify | `abandonMove` uses the cooldown; notify; pre-turn refresh hook |
| `internal/agenthub/account_manage.go` | create | add / rename / shared / remove / list, id and home rules, `OnAccountsChanged` |
| `internal/agenthub/account_policy_cache.go` | create | 3-second cache of the policy read |
| `internal/agenthub/account_probe.go` | create | `UsageChecker`, `SignInChecker`, `SignInState`, `ErrUsageNotCheckable`, `RateLimitReader` |
| `internal/agenthub/account_check.go` | create | `checkUsage` (guards, single flight), `RefreshAccountUsage`, `refreshStaleForTurn` |
| `internal/agenthub/account_signin.go` | create | per-account sign-in and status (`AccountSignInCommand`, `StartAccountSignIn`, `RefreshAccountStatus`, `AfterAccountSignIn`) |
| `internal/codexapp/protocol.go`, `items.go` | modify | `MethodRateLimitsRead`, response type, float percent |
| `internal/agenthub/adapter_codex.go`, `codex_auth.go`, `codex_translate.go` | modify | `oneShot` takes a spec; `CheckUsage`; `CheckSignIn`; sign-in at a home; `AccountID` on the result; runtime `ReadRateLimits` |
| `internal/agenthub/adapter_claude.go`, `claude_cli.go` (new) | modify/create | `claudeCLI` helper; `CheckUsage`; `CheckSignIn`; `SignInCommandFor` |
| `internal/agenthub/claude_limits.go` | modify | overage windows are not "reached" |
| `internal/agenthub/update.go` | modify | `AccountNotice` on `SessionUpdate` |
| `internal/agenthub/stop_service.go`, `account_limit.go` | modify | auth required → signed out |
| `internal/agenthub/account_usage.go`, `accounts.go` | modify | observed-usage freshness |
| `internal/bridge/agenthub_accounts.go`, `errors.go` | create/modify | DTOs, methods, published errors |
| `internal/app/container.go`, `app_test.go` | modify | grants |
| `main.go` | modify | wiring and the event |
| `frontend/bindings/**`, `AGENTS.md` | regenerate/modify | bindings; notes |

---

### Task 1: The account book learns to delete, to mark signed-out, to replace usage and to cool down

**Files:**
- Modify: `internal/storage/sqlite.go` (after `m45`; the runner list), `internal/storage/migrations_test.go` (version pin 45 → 46)
- Modify: `internal/agenthub/accounts.go`, `internal/agenthub/accounts_store.go`
- Test: `internal/agenthub/accounts_manage_store_test.go` (external package `agenthub_test`)

- [ ] **Step 1: Write the failing tests.** In `internal/storage/migrations_test.go` change the pin to `46` (both lines) and add:

```go
func TestMigration46AddsAgentAccountCooldown(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, TableAgentAccounts, "cooldown_until").Scan(&n); err != nil || n != 1 {
		t.Fatalf("agent_accounts.cooldown_until missing after migration 46 (n=%d, err=%v)", n, err)
	}
}
```
Create `internal/agenthub/accounts_manage_store_test.go`:

```go
package agenthub_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newAccountDB(t *testing.T) (*agenthub.SQLAccountStore, *agenthub.SQLStore, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return agenthub.NewSQLAccountStore(db), agenthub.NewSQLStore(db), db
}

func addWork(t *testing.T, book *agenthub.SQLAccountStore) {
	t.Helper()
	if err := book.Put(context.Background(), agenthub.AccountRecord{ID: "codex-work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountBookDeleteReturnsItsSessionsToTheBuiltInAccount(t *testing.T) {
	book, store, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	bound := sampleSession("sess-on-work")
	bound.AccountID = "codex-work"
	other := sampleSession("sess-elsewhere")
	other.AccountID = "codex-other"
	for _, s := range []agenthub.AgentSession{bound, other} {
		if err := store.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.Delete(ctx, "codex-work"); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Get(ctx, "codex-work"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Fatalf("the row must be gone, got %v", err)
	}
	got, _ := store.GetSession(ctx, "sess-on-work")
	if got.AccountID != "" {
		t.Errorf("its session is bound to %q, want the built-in account (empty)", got.AccountID)
	}
	if got, _ := store.GetSession(ctx, "sess-elsewhere"); got.AccountID != "codex-other" {
		t.Errorf("another account's session was touched: %q", got.AccountID)
	}
}

func TestAccountBookDeleteRefusesTheBuiltInAndTheUnknown(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	if _, err := book.List(ctx, agenthub.AgentTypeCodex); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := book.Delete(ctx, agenthub.BuiltinAccountID(agenthub.AgentTypeCodex)); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("deleting the built-in account: %v, want ErrInvalidAccount", err)
	}
	if err := book.Delete(ctx, "nope"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("deleting an unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookSetSignedIn(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	if err := book.SetSignedIn(ctx, "codex-work", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := book.Get(ctx, "codex-work"); got.SignedIn {
		t.Error("SetSignedIn(false) did not stick")
	}
	if err := book.SetSignedIn(ctx, "codex-work", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := book.Get(ctx, "codex-work"); !got.SignedIn {
		t.Error("SetSignedIn(true) did not stick")
	}
	if err := book.SetSignedIn(ctx, "missing", true); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookReplaceUsageStoresTheWholeReading(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	at := time.Unix(1_800_000_000, 0)
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 90}, {Label: "Weekly", UsedPercent: 40}}, "observed", at); err != nil {
		t.Fatal(err)
	}
	if err := book.ReplaceUsage(ctx, "codex-work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 5}}, "checked", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 5 {
		t.Fatalf("usage = %+v, want exactly the checked 5-hour window (the weekly one is not in the new reading)", got.Usage)
	}
	if got.UsageSource != "checked" || !got.ObservedAt.Equal(at.Add(time.Minute)) {
		t.Errorf("source/observedAt = %q / %v", got.UsageSource, got.ObservedAt)
	}
	if err := book.ReplaceUsage(ctx, "missing", nil, "checked", at); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookMarkCooldownOnlyMovesLaterAndIsNotAnExhaustion(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	late, early := time.Unix(1_800_003_600, 0), time.Unix(1_800_000_600, 0)
	if err := book.MarkCooldown(ctx, "codex-work", late); err != nil {
		t.Fatal(err)
	}
	if err := book.MarkCooldown(ctx, "codex-work", early); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if !got.CooldownUntil.Equal(late) {
		t.Errorf("cooldown_until = %v, want %v (never earlier)", got.CooldownUntil, late)
	}
	if !got.ExhaustedUntil.IsZero() {
		t.Errorf("a cooldown must not mark the account as having reached a limit: %v", got.ExhaustedUntil)
	}
	if err := book.MarkCooldown(ctx, "missing", late); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}
```
(`sampleSession` is the helper in `store_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/storage/ ./internal/agenthub/ -run 'TestMigration46|TestAccountBook(Delete|SetSignedIn|ReplaceUsage|MarkCooldown)' -count=1`
Expected: FAIL (`undefined: TableAgentAccounts` is fine, then `book.Delete undefined`, `CooldownUntil undefined`).

- [ ] **Step 3: Implement.** `sqlite.go`, after `m45`:

```go
	// m46: an account whose reopen failed (or any other reason that is not "it reached
	// a limit") is kept out of selection for a while: agent_accounts.cooldown_until,
	// Unix seconds. exhausted_until keeps meaning "a limit was reached".
	m46 := Migration{
		Version: 46,
		Name:    "agent accounts cooldown",
		Up: func(ctx context.Context) error {
			return addColumnIfNotExists(ctx, db, TableAgentAccounts, "cooldown_until", "INTEGER NOT NULL DEFAULT 0")
		},
	}
```
and append `, m46` to the runner list. `accounts.go`: add to `AccountRecord` after `ExhaustedUntil`:

```go
	// CooldownUntil keeps the account out of selection until then without saying
	// it reached a limit: a reopen in its home failed. Zero or past: not cooling.
	CooldownUntil time.Time
```
and to the `AccountBook` interface:

```go
	// Delete removes an added account and returns its sessions to the built-in
	// account. The built-in account cannot be deleted.
	Delete(ctx context.Context, id string) error
	// SetSignedIn records what the vendor's own status said.
	SetSignedIn(ctx context.Context, id string, signedIn bool) error
	// ReplaceUsage stores windows as the whole reading: an on-demand check returns
	// the full set, unlike an observed update, which carries some windows.
	ReplaceUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error
	// MarkCooldown moves the cooldown later, never earlier.
	MarkCooldown(ctx context.Context, id string, until time.Time) error
```
`accounts_store.go`: `accountColumns` becomes `id, vendor, label, home_dir, signed_in, shared, exhausted_until, cooldown_until, usage, usage_source, observed_at`; in `scanAccount` add `var cooldown int64`, scan it between `&exhausted` and `&usage`, and `if cooldown > 0 { a.CooldownUntil = time.Unix(cooldown, 0) }`. Add (imports `fmt` already present):

```go
func (st *SQLAccountStore) Delete(ctx context.Context, id string) error {
	acct, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account cannot be removed", ErrInvalidAccount)
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed
	if _, err := tx.ExecContext(ctx, `UPDATE `+tableAgentSessions+` SET agent_account_id = '' WHERE agent_account_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableAgentAccounts+` WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// updateAccount runs one UPDATE and reports an unknown id.
func (st *SQLAccountStore) updateAccount(ctx context.Context, set, id string, args ...any) error {
	res, err := st.db.ExecContext(ctx, `UPDATE `+tableAgentAccounts+` SET `+set+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (st *SQLAccountStore) SetSignedIn(ctx context.Context, id string, signedIn bool) error {
	return st.updateAccount(ctx, `signed_in = ?`, id, boolInt(signedIn))
}

func (st *SQLAccountStore) ReplaceUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error {
	raw, err := json.Marshal(windows)
	if err != nil {
		return err
	}
	return st.updateAccount(ctx, `usage = ?, usage_source = ?, observed_at = ?`, id, string(raw), source, at.Unix())
}

func (st *SQLAccountStore) MarkCooldown(ctx context.Context, id string, until time.Time) error {
	if _, err := st.Get(ctx, id); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET cooldown_until = ? WHERE id = ? AND cooldown_until < ?`,
		until.Unix(), id, until.Unix())
	return err
}
```
Give any other `AccountBook` implementation (a test fake that the compiler flags) the four methods.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/storage/ ./internal/agenthub/ -run 'TestMigration|TestAccountBook' -race -count=1 && go build ./internal/... ./cmd/... && go vet ./internal/agenthub/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage internal/agenthub
git commit -m "agenthub: the account book can delete, mark signed-out, replace usage and cool an account down"
```

---

### Task 2: A cooling-down account is not an exhausted one

An account whose reopen failed is out for fifteen minutes but has not reached a limit. Plan 1 stored that in `ExhaustedUntil`, which made `ErrAllAccountsLimited` say "reached its limit" when the other account merely had a dead sign-in.

**Files:**
- Modify: `internal/agenthub/accountpick.go` (`accountFine`, the candidate loop), `internal/agenthub/account_switch.go` (`abandonMove`)
- Modify: `internal/agenthub/account_turn_test.go` (the two tests that read `ExhaustedUntil` of the abandoned target)
- Test: `internal/agenthub/accountpick_test.go`

- [ ] **Step 1: Write the failing tests.** Append to `accountpick_test.go` (helpers `pickAccount`, `pickWindow`, `pickPolicy`, `pickNow`, `pickBuiltin` already exist there):

```go
func TestPickAccountSkipsAnAccountThatIsCoolingDown(t *testing.T) {
	cooling := pickAccount("b")
	cooling.CooldownUntil = pickNow.Add(time.Minute)
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy:   pickPolicy(),
		Accounts: []agenthub.AccountRecord{pickAccount(pickBuiltin, pickWindow("5-hour", 99)), cooling, pickAccount("c")},
		Current:  pickBuiltin, Now: pickNow,
	})
	if !got.Switch || got.Target != "c" {
		t.Fatalf("got %+v, want a move to c (b is cooling down)", got)
	}
}

func TestPickAccountACooldownAloneIsNotAllLimited(t *testing.T) {
	exhausted := pickAccount(pickBuiltin)
	exhausted.ExhaustedUntil = pickNow.Add(time.Hour)
	cooling := pickAccount("b")
	cooling.CooldownUntil = pickNow.Add(time.Minute)
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy: pickPolicy(), Accounts: []agenthub.AccountRecord{exhausted, cooling}, Current: pickBuiltin, Now: pickNow,
	})
	if got.AllLimited {
		t.Fatalf("got %+v: b has not reached a limit, so the turn must not be refused with \"every account reached its limit\"", got)
	}
	if got.Switch {
		t.Fatalf("got %+v: b is cooling down, it must not be chosen", got)
	}
}

func TestPickAccountNamesTheSignedOutAccountsItSkipped(t *testing.T) {
	out := pickAccount("b")
	out.SignedIn = false
	out.Label = "Work"
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy:   pickPolicy(),
		Accounts: []agenthub.AccountRecord{pickAccount(pickBuiltin, pickWindow("5-hour", 99)), out, pickAccount("c")},
		Current:  pickBuiltin, Now: pickNow,
	})
	if !got.Switch || got.Target != "c" {
		t.Fatalf("got %+v, want a move to c", got)
	}
	if !strings.Contains(got.Reason, "Work") || !strings.Contains(got.Reason, "signed out") {
		t.Errorf("reason = %q, want it to say that Work was skipped because it is signed out", got.Reason)
	}
}
```
(add `strings` to that file's imports if missing). In `account_turn_test.go`, in the tests that assert the abandoned target is "marked" (`TestAccountTurnStaysOnItsAccountWhenTheNewOneCannotOpen`, and the cooldown assertions in `TestAccountTurnWhoseMoveBackCannotBeWrittenStaysOnTheNewAccount`) change `.ExhaustedUntil` to `.CooldownUntil` and add `if !got.ExhaustedUntil.IsZero() { t.Error("a failed reopen must not claim a limit") }`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agenthub/ -run 'TestPickAccount|TestAccountTurn' -count=1`
Expected: FAIL (cooling account is chosen; reason lacks the skipped name; `abandonMove` still writes `ExhaustedUntil`).

- [ ] **Step 3: Implement.** `accountFine`, right after the `ExhaustedUntil` check:

```go
	if a.CooldownUntil.After(now) {
		return false, "could not be opened a moment ago"
	}
```
In `PickAccount`'s candidate loop collect the signed-out accounts passed over, and add them to the reason of the move:

```go
	var skipped []string
	for _, a := range ordered {
		if a.ID == cur.ID {
			continue
		}
		if !a.SignedIn {
			skipped = append(skipped, a.Label)
			continue
		}
		margin := 0.0
		if a.ID == in.Previous {
			margin = accountReturnGap
		}
		if fine, _ := accountFine(a, in.Policy, in.Now, margin); fine {
			reason := why
			if len(skipped) > 0 {
				reason += "; skipped " + strings.Join(skipped, ", ") + " (signed out)"
			}
			return PickDecision{Target: a.ID, Switch: true, Reason: reason}
		}
	}
```
(`strings` import.) `earliestReturn` already requires every signed-in account to be *exhausted* (`ExhaustedUntil` in the future), so a cooling account makes it return `false` — no change. In `abandonMove` replace `s.Accounts.MarkExhausted(ctx, target, now.Add(accountProbeCooldown))` with `s.Accounts.MarkCooldown(ctx, target, now.Add(accountProbeCooldown))` and update its doc comment ("a cooldown, not an exhaustion").

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agenthub/ -run 'TestPickAccount|TestAccountTurn|TestAccountMove' -race -count=1 && go vet ./internal/agenthub/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: a reopen that failed is a cooldown, not a limit; the switch reason names skipped accounts"
```

---

### Task 3: Adding, renaming, removing and listing accounts

**Files:**
- Create: `internal/agenthub/account_manage.go`
- Modify: `internal/agenthub/service.go` (three fields after `AccountSharedHome`)
- Test: `internal/agenthub/account_manage_test.go` (external, uses the rig)

- [ ] **Step 1: Write the failing tests** (`package agenthub_test`; the rig `newAccountRig`, `uiCtx`, `agentCtx`, `projectDir`, `startSession` come from earlier tests):

```go
package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

// manageRig is the account rig plus the folders and vendors accounts are added under.
func manageRig(t *testing.T) (*accountRig, string, string) {
	t.Helper()
	rig := newAccountRig(t, preferences.RefusalNone)
	dir, shared := t.TempDir(), t.TempDir()
	rig.svc.AccountsDir = dir
	rig.svc.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeCodex, agenthub.AgentTypeMock}
	rig.svc.AccountSharedHome = func(agenthub.AgentType) string { return shared }
	return rig, dir, shared
}

func TestAddAccountMakesTheRowTheHomeAndTheStoreLink(t *testing.T) {
	rig, dir, shared := manageRig(t)
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "  Work team  ")
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID != "codex-work-team" || acct.Label != "Work team" || acct.SignedIn || acct.Builtin() {
		t.Fatalf("account = %+v", acct)
	}
	if want := filepath.Join(dir, "codex", "codex-work-team"); acct.HomeDir != want {
		t.Errorf("home = %q, want %q", acct.HomeDir, want)
	}
	if target, err := os.Readlink(filepath.Join(acct.HomeDir, "sessions")); err != nil || target != filepath.Join(shared, "sessions") {
		t.Errorf("the conversation store is not linked to the built-in one: %q, %v", target, err)
	}
	if changed != 1 {
		t.Errorf("OnAccountsChanged fired %d times, want 1", changed)
	}
	if got, err := rig.book.Get(context.Background(), acct.ID); err != nil || got.HomeDir != acct.HomeDir {
		t.Errorf("stored = %+v, %v", got, err)
	}
}

func TestAddAccountRefusesWhatCannotBeAnAccount(t *testing.T) {
	rig, _, _ := manageRig(t)
	if _, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		vendor agenthub.AgentType
		label  string
		want   error
	}{
		"empty label":          {agenthub.AgentTypeCodex, "   ", agenthub.ErrInvalidAccount},
		"label too long":       {agenthub.AgentTypeCodex, strings.Repeat("x", 41), agenthub.ErrInvalidAccount},
		"control character":    {agenthub.AgentTypeCodex, "a\nb", agenthub.ErrInvalidAccount},
		"same label, any case": {agenthub.AgentTypeCodex, "WORK", agenthub.ErrAccountLabelTaken},
		"vendor with none":     {agenthub.AgentTypeClaude, "Work", agenthub.ErrInvalidAccount},
	}
	for name, tc := range cases {
		if _, err := rig.svc.AddAccount(uiCtx(), tc.vendor, tc.label); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
	}
}

func TestAddAccountNeverTakesTheBuiltInId(t *testing.T) {
	rig, _, _ := manageRig(t)
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "default")
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID == agenthub.BuiltinAccountID(agenthub.AgentTypeCodex) || acct.Builtin() {
		t.Fatalf("an added account took the built-in id: %+v", acct)
	}
	second, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Default 2")
	if err != nil || second.ID == acct.ID {
		t.Fatalf("ids must be unique: %+v, %v", second, err)
	}
}

func TestAccountManagementIsForTheWindowOnly(t *testing.T) {
	rig, _, _ := manageRig(t)
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	agent := agentCtx("11111111-1111-1111-1111-111111111111")
	for name, call := range map[string]func() error{
		"list":   func() error { _, err := rig.svc.ListAccounts(agent, agenthub.AgentTypeCodex); return err },
		"add":    func() error { _, err := rig.svc.AddAccount(agent, agenthub.AgentTypeCodex, "X"); return err },
		"rename": func() error { _, err := rig.svc.RenameAccount(agent, acct.ID, "Y"); return err },
		"shared": func() error { _, err := rig.svc.SetAccountShared(agent, acct.ID, true); return err },
		"remove": func() error { return rig.svc.RemoveAccount(agent, acct.ID) },
	} {
		if err := call(); !errors.Is(err, agenthub.ErrPermissionDenied) {
			t.Errorf("%s by an agent: error = %v, want ErrPermissionDenied", name, err)
		}
	}
}

func TestRenameAndSharedKeepWhatElseIsStored(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	acct, _ := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err := rig.book.SaveUsage(context.Background(), acct.ID, []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 30}}, "observed", nowForTest()); err != nil {
		t.Fatal(err)
	}
	renamed, err := rig.svc.RenameAccount(ctx, acct.ID, "Personal")
	if err != nil || renamed.Label != "Personal" || len(renamed.Usage) != 1 {
		t.Fatalf("renamed = %+v, %v (usage must survive)", renamed, err)
	}
	shared, err := rig.svc.SetAccountShared(ctx, acct.ID, true)
	if err != nil || !shared.Shared || shared.Label != "Personal" {
		t.Fatalf("shared = %+v, %v", shared, err)
	}
	if _, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Other"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.RenameAccount(ctx, acct.ID, "other"); !errors.Is(err, agenthub.ErrAccountLabelTaken) {
		t.Errorf("renaming onto another account's label: %v, want ErrAccountLabelTaken", err)
	}
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeCodex)
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeCodex); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.RenameAccount(ctx, builtin, "Mine"); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("renaming the built-in account: %v, want ErrInvalidAccount", err)
	}
}

func TestRemoveAccountDeletesTheRowTheHomeAndKeepsTheSharedStore(t *testing.T) {
	rig, _, shared := manageRig(t)
	ctx := uiCtx()
	acct, _ := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err := os.WriteFile(filepath.Join(shared, "sessions", "keep.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acct.HomeDir, "auth.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(acct.HomeDir); !os.IsNotExist(err) {
		t.Errorf("the account's home must be gone (stat error %v)", err)
	}
	if _, err := os.Stat(filepath.Join(shared, "sessions", "keep.jsonl")); err != nil {
		t.Errorf("removing an account must not touch the shared conversation store: %v", err)
	}
	if _, err := rig.book.Get(context.Background(), acct.ID); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("row still there: %v", err)
	}
	if changed != 1 {
		t.Errorf("OnAccountsChanged fired %d times, want 1", changed)
	}
}

func TestRemoveAccountRefusesTheBuiltInAndOneInUse(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(ctx, agenthub.BuiltinAccountID(agenthub.AgentTypeMock)); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("removing the built-in account: %v, want ErrInvalidAccount", err)
	}
	// A session whose process runs as the account blocks its removal.
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Busy")
	if err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, acct.ID); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(ctx, sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(ctx, acct.ID); !errors.Is(err, agenthub.ErrAccountInUse) {
		t.Fatalf("removing an account a live session runs as: %v, want ErrAccountInUse", err)
	}
	if err := rig.svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatalf("after the session is closed: %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("the closed session is bound to %q, want the built-in account", got)
	}
}

func TestListAccountsStartsWithTheBuiltInAccount(t *testing.T) {
	rig, _, _ := manageRig(t)
	if _, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work"); err != nil {
		t.Fatal(err)
	}
	list, err := rig.svc.ListAccounts(uiCtx(), agenthub.AgentTypeCodex)
	if err != nil || len(list) != 2 || !list[0].Builtin() || list[1].ID != "codex-work" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestAccountManagementIsOffWithoutABookOrAFolder(t *testing.T) {
	rig, _, _ := manageRig(t)
	rig.svc.AccountsDir = ""
	if _, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work"); !errors.Is(err, agenthub.ErrAccountsUnavailable) {
		t.Errorf("no accounts folder: %v, want ErrAccountsUnavailable", err)
	}
	rig.svc.Accounts = nil
	if _, err := rig.svc.ListAccounts(uiCtx(), agenthub.AgentTypeCodex); !errors.Is(err, agenthub.ErrAccountsUnavailable) {
		t.Errorf("no book: %v, want ErrAccountsUnavailable", err)
	}
}
```
with `func nowForTest() time.Time { return time.Now() }` added to the file (import `time`).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agenthub/ -run 'AddAccount|AccountManagement|RenameAndShared|RemoveAccount|ListAccounts' -count=1`
Expected: FAIL (`undefined: ErrAccountLabelTaken`, `svc.AddAccount undefined`).

- [ ] **Step 3: Implement.** `service.go`, after `AccountSharedHome`:

```go
	// AccountsDir is where added accounts' homes live (<data>/agent-accounts); the
	// layout under it is <vendor>/<id>. AccountVendors are the vendors that may have
	// added accounts (Claude and Codex in main.go). OnAccountsChanged fires after
	// anything the Accounts page shows changed; it carries nothing.
	AccountsDir       string
	AccountVendors    []AgentType
	OnAccountsChanged func()
```
`account_manage.go`:

```go
package agenthub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"devkit/internal/identity"
)

var (
	// ErrAccountsUnavailable: no account book, folder or shared home is wired.
	ErrAccountsUnavailable = errors.New("agenthub.accountsUnavailable")
	// ErrAccountLabelTaken: another account of the vendor has that name.
	ErrAccountLabelTaken = errors.New("agenthub.accountLabelTaken")
	// ErrAccountInUse: a running session's process runs as the account.
	ErrAccountInUse = errors.New("agenthub.accountInUse")
)

const maxAccountLabel = 40

// requireWindow refuses anything but the person's own window: accounts steer which
// of their subscriptions a session spends, so an agent never manages them.
func requireWindow(ctx context.Context) error {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return ErrPermissionDenied
	}
	return nil
}

func (s *Service) accountBook(ctx context.Context) (AccountBook, error) {
	if err := requireWindow(ctx); err != nil {
		return nil, err
	}
	if s.Accounts == nil {
		return nil, ErrAccountsUnavailable
	}
	return s.Accounts, nil
}

func (s *Service) accountsChanged() {
	if s.OnAccountsChanged != nil {
		s.OnAccountsChanged()
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func accountSlug(label string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if slug == "" {
		slug = "account"
	}
	if len(slug) > 24 {
		slug = strings.TrimRight(slug[:24], "-")
	}
	return slug
}

// newAccountID is "<vendor>-<slug of the label>", numbered when taken, and never the
// built-in account's id.
func newAccountID(vendor AgentType, label string, taken map[string]bool) string {
	base := string(vendor) + "-" + accountSlug(label)
	id := base
	for n := 2; taken[id] || id == BuiltinAccountID(vendor); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func cleanAccountLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	switch {
	case label == "":
		return "", fmt.Errorf("%w: an account needs a name", ErrInvalidAccount)
	case utf8.RuneCountInString(label) > maxAccountLabel:
		return "", fmt.Errorf("%w: a name is at most %d characters", ErrInvalidAccount, maxAccountLabel)
	case strings.ContainsFunc(label, unicode.IsControl):
		return "", fmt.Errorf("%w: a name cannot hold control characters", ErrInvalidAccount)
	}
	return label, nil
}

// accountHomeFor is where an added account's home lives, and the only place Put may
// be asked to point at: absolute, clean, inside AccountsDir. carveOutAll matches
// paths lexically, so it must be built from the same AccountsDir string the data
// directory protection (ProtectDataDir) was built from.
func (s *Service) accountHomeFor(vendor AgentType, id string) (string, error) {
	if s.AccountsDir == "" || !filepath.IsAbs(s.AccountsDir) || s.AccountSharedHome == nil {
		return "", fmt.Errorf("%w: the folder for added accounts is not set", ErrAccountsUnavailable)
	}
	home := filepath.Join(s.AccountsDir, string(vendor), id)
	if !pathIsInside(home, s.AccountsDir) {
		return "", fmt.Errorf("%w: %s is not inside the accounts folder", ErrAccountHome, home)
	}
	return home, nil
}

func (s *Service) ListAccounts(ctx context.Context, vendor AgentType) ([]AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return nil, err
	}
	return book.List(ctx, vendor)
}

func (s *Service) AddAccount(ctx context.Context, vendor AgentType, label string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	if !slices.Contains(s.AccountVendors, vendor) {
		return AccountRecord{}, fmt.Errorf("%w: %s has no added accounts", ErrInvalidAccount, vendor)
	}
	if label, err = cleanAccountLabel(label); err != nil {
		return AccountRecord{}, err
	}
	existing, err := book.List(ctx, vendor)
	if err != nil {
		return AccountRecord{}, err
	}
	taken := make(map[string]bool, len(existing))
	for _, a := range existing {
		taken[a.ID] = true
		if strings.EqualFold(a.Label, label) {
			return AccountRecord{}, ErrAccountLabelTaken
		}
	}
	id := newAccountID(vendor, label, taken)
	home, err := s.accountHomeFor(vendor, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if err := PrepareAccountHome(vendor, home, s.AccountSharedHome(vendor)); err != nil {
		return AccountRecord{}, err
	}
	if err := book.Put(ctx, AccountRecord{ID: id, Vendor: vendor, Label: label, HomeDir: home}); err != nil {
		return AccountRecord{}, err
	}
	s.accountsChanged()
	return book.Get(ctx, id)
}

// editAccount reads an added account, lets change adjust the editable fields and
// writes them back. Usage and markers are never touched (Put does not write them).
func (s *Service) editAccount(ctx context.Context, id string, change func(*AccountRecord) error) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if acct.Builtin() {
		return AccountRecord{}, fmt.Errorf("%w: the built-in account cannot be changed here", ErrInvalidAccount)
	}
	if err := change(&acct); err != nil {
		return AccountRecord{}, err
	}
	if err := book.Put(ctx, acct); err != nil {
		return AccountRecord{}, err
	}
	s.accountsChanged()
	return book.Get(ctx, id)
}

func (s *Service) RenameAccount(ctx context.Context, id, label string) (AccountRecord, error) {
	if err := requireWindow(ctx); err != nil {
		return AccountRecord{}, err
	}
	label, err := cleanAccountLabel(label)
	if err != nil {
		return AccountRecord{}, err
	}
	return s.editAccount(ctx, id, func(a *AccountRecord) error {
		others, err := s.Accounts.List(ctx, a.Vendor)
		if err != nil {
			return err
		}
		for _, o := range others {
			if o.ID != a.ID && strings.EqualFold(o.Label, label) {
				return ErrAccountLabelTaken
			}
		}
		a.Label = label
		return nil
	})
}

func (s *Service) SetAccountShared(ctx context.Context, id string, shared bool) (AccountRecord, error) {
	return s.editAccount(ctx, id, func(a *AccountRecord) error {
		a.Shared = shared
		return nil
	})
}

// accountInUse reports whether a running session's process runs as the account.
func (s *Service) accountInUse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID := range s.runtimes {
		if s.sessionAccount[sessionID] == id {
			return true
		}
	}
	return false
}

// RemoveAccount deletes an added account, returns its sessions to the built-in
// account and removes its home. os.RemoveAll never follows a symlink, so the shared
// conversation store its sessions link to is untouched.
func (s *Service) RemoveAccount(ctx context.Context, id string) error {
	book, err := s.accountBook(ctx)
	if err != nil {
		return err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return err
	}
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account cannot be removed", ErrInvalidAccount)
	}
	if s.accountInUse(id) {
		return ErrAccountInUse
	}
	if err := book.Delete(ctx, id); err != nil {
		return err
	}
	s.forgetUsageSeen(id)
	if s.AccountsDir != "" && pathIsInside(acct.HomeDir, s.AccountsDir) {
		if err := os.RemoveAll(acct.HomeDir); err != nil {
			return fmt.Errorf("%w: the account was removed but its folder could not be: %v", ErrAccountHome, err)
		}
	}
	s.accountsChanged()
	return nil
}
```
If `forgetUsageSeen` or `requireWindow` collides with an existing name, rename yours and say so.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agenthub/ -run 'AddAccount|AccountManagement|RenameAndShared|RemoveAccount|ListAccounts' -race -count=1 && go vet ./internal/agenthub/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: add, rename, share, list and remove vendor accounts (window only)"
```

---

### Task 4: Four gaps the reviews left open

(a) an account whose sign-in died is skipped; (b) an overage window is not "reached"; (c) observed usage stays fresh without a stale reading looking new; (d) the policy read is cached for a few seconds.

**Files:**
- Create: `internal/agenthub/account_policy_cache.go`
- Modify: `internal/agenthub/account_limit.go` (`vendorConfigured`), `account_switch.go` (`accountPolicyFor`), `account_usage.go`, `accounts.go` (`mergeWindows`), `accounts_store.go` (`SaveUsage`), `claude_limits.go`, `update.go` (`RateLimit`, `parseRateLimit`), `stop_service.go`, `adapter_mock.go`, `service.go` (`usageSeen` type)
- Test: `account_gaps_test.go` (external), plus rows in `accounts_internal_test.go`, `claude_limits_test.go`, `account_usage_test.go`

- [ ] **Step 1: Write the failing tests.**

(a) `account_gaps_test.go`, with a mock marker `FAIL_AUTH_REQUIRED` that makes the mock prompt fail with `ErrAgentAuthRequired` (added in Step 3):

```go
package agenthub_test

import (
	"context"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func TestASignInThatDiedMarksTheAccountSignedOut(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"}); err == nil {
		t.Fatal("the turn must fail")
	}
	got, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	if err != nil {
		t.Fatal(err)
	}
	if got.SignedIn {
		t.Error("an auth-required stop must mark the account the process ran as signed out")
	}
}

func TestASignInThatDiedIsIgnoredForAVendorNobodyConfigured(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountPolicy = nil
	sess := startSession(t, rig)
	_, _ = rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"})
	if _, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock)); err == nil {
		t.Error("nothing configured: no account row may be created or changed")
	}
}
```
(b) append to `claude_limits_test.go` (internal package; `ParseSessionUpdate` is exported):

```go
func TestClaudeOverageIsNotAReachedWindow(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"status":"allowed","rateLimitType":"five_hour","isUsingOverage":true,"overageStatus":"allowed",
		"unifiedWindows":{"five_hour":{"utilization":1.2,"resetsAt":1792653600},"seven_day":{"utilization":0.4,"resetsAt":1793080800}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v", got.Usage)
	}
	if status := got.Usage.Limits[0].Status; status != "warning" {
		t.Errorf("a window past 100%% while the plan goes on through overage is %q, want warning (the account still answers)", status)
	}
}

func TestClaudeOverageThatIsRejectedStaysReached(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"status":"rejected","rateLimitType":"five_hour","isUsingOverage":true,
		"unifiedWindows":{"five_hour":{"utilization":1.2,"resetsAt":1792653600}}}}}`)
	if got := ParseSessionUpdate(update); got.Usage == nil || got.Usage.Limits[0].Status != "reached" {
		t.Fatalf("limits = %+v, want the rejected window reached", got.Usage)
	}
}
```
(c) rows in `accounts_internal_test.go`'s `TestMergeWindows` are already in place for the stale rule; add a new test for the report and one for the store:

```go
func TestMergeWindowsReportsWhetherAnythingWasAccepted(t *testing.T) {
	stored := []LimitWindow{{Label: "5-hour", UsedPercent: 90, ResetsAt: 1000}}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "5-hour", UsedPercent: 10, ResetsAt: 1000}}); accepted {
		t.Error("a lower reading of the same window is stale: nothing accepted")
	}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "5-hour", UsedPercent: 95, ResetsAt: 1000}}); !accepted {
		t.Error("a higher reading is accepted")
	}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "Weekly", UsedPercent: 1, ResetsAt: 2000}}); !accepted {
		t.Error("a new window is accepted")
	}
}
```
and in `accounts_manage_store_test.go`:

```go
func TestAccountBookSaveUsageKeepsObservedAtWhenEveryWindowWasStale(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	first := time.Unix(1_800_000_000, 0)
	w := agenthub.LimitWindow{Label: "5-hour", UsedPercent: 90, ResetsAt: 1_800_010_000}
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{w}, "observed", first); err != nil {
		t.Fatal(err)
	}
	stale := w
	stale.UsedPercent = 10
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{stale}, "observed", first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if !got.ObservedAt.Equal(first) || got.Usage[0].UsedPercent != 90 {
		t.Errorf("a stale reading must not make the usage look newer: observedAt=%v usage=%+v", got.ObservedAt, got.Usage)
	}
}
```
and in `account_usage_test.go` (internal, rig `usageRig`) a test that an identical reading is written again once a minute has passed: give `Service` a package var `usageRefreshEvery = 60 * time.Second` and `usageClock func() time.Time` (unexported, defaulting to `time.Now`) so the test sets the clock:

```go
func TestAnIdenticalUsageReadingIsWrittenAgainAfterAMinute(t *testing.T) {
	svc := &Service{usageSeen: map[string]seenUsage{}}
	now := time.Unix(1_800_000_000, 0)
	svc.usageClock = func() time.Time { return now }
	w := []LimitWindow{{Label: "5-hour", UsedPercent: 50, ResetsAt: 1_900_000_000}}
	if !svc.usageChanged("a", w) {
		t.Fatal("the first reading is a change")
	}
	now = now.Add(30 * time.Second)
	if svc.usageChanged("a", w) {
		t.Error("the same reading 30 s later needs no write")
	}
	now = now.Add(31 * time.Second)
	if !svc.usageChanged("a", w) {
		t.Error("the same reading after a minute must be written so observed_at stays fresh")
	}
}
```
(d) `account_policy_cache_test.go` (internal):

```go
package agenthub

import (
	"testing"
	"time"

	"devkit/internal/preferences"
)

func TestThePolicyReadIsCachedBrieflyAndCanBeInvalidated(t *testing.T) {
	reads := 0
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		reads++
		return preferences.AgentAccountPolicy{Revision: int64(reads)}, nil
	}}
	now := time.Unix(1_800_000_000, 0)
	svc.policyClock = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, err := svc.cachedAccountPolicy(); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Fatalf("reads = %d, want one for five calls inside the TTL", reads)
	}
	now = now.Add(accountPolicyTTL + time.Second)
	got, _ := svc.cachedAccountPolicy()
	if reads != 2 || got.Revision != 2 {
		t.Errorf("after the TTL: reads=%d revision=%d, want a fresh read", reads, got.Revision)
	}
	svc.InvalidateAccountPolicy()
	if _, _ = svc.cachedAccountPolicy(); reads != 3 {
		t.Errorf("after InvalidateAccountPolicy: reads = %d, want 3", reads)
	}
}

func TestAPolicyReadErrorIsNotCached(t *testing.T) {
	calls := 0
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		calls++
		return preferences.AgentAccountPolicy{}, errAccountsUnavailableForTest
	}}
	svc.policyClock = time.Now
	_, _ = svc.cachedAccountPolicy()
	_, _ = svc.cachedAccountPolicy()
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (an error must be retried, not remembered)", calls)
	}
}

var errAccountsUnavailableForTest = ErrAccountsUnavailable
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agenthub/ -run 'ASignInThatDied|ClaudeOverage|MergeWindowsReports|KeepsObservedAt|IdenticalUsageReading|ThePolicyRead|APolicyReadError' -count=1`
Expected: FAIL (does not compile: `mergeWindowsReport`, `seenUsage`, `cachedAccountPolicy`, mock marker undefined).

- [ ] **Step 3: Implement.**

(a) `adapter_mock.go`, next to `FAIL_USAGE_LIMIT`: `if strings.Contains(text, "FAIL_AUTH_REQUIRED") { return PromptOutcome{}, vendorError(AgentTypeMock, ErrAgentAuthRequired) }` (use the same constructor shape the neighbouring markers use; `vendorError(vendor, err)` is what `resolveCommand` uses). `account_limit.go`: add

```go
// noteSignedOut marks the account a refused turn ran as signed out when the vendor
// said sign-in is required, so PickAccount skips it until the person signs in again
// (Refresh status, or signing in, sets it back). Only for a vendor the person configured.
func (s *Service) noteSignedOut(ctx context.Context, sess AgentSession, stop SessionStop) {
	if s.Accounts == nil || stop.Kind != StopAuthRequired || !s.vendorConfigured(sess.AgentType) {
		return
	}
	if _, err := s.Accounts.List(ctx, sess.AgentType); err != nil { // creates the built-in row
		log.Printf("agenthub: accounts of %s: %v", sess.AgentType, err)
		return
	}
	id, err := s.observedAccountID(ctx, sess.ID)
	if err != nil || id == "" {
		id = accountIDOf(sess)
	}
	if err := s.Accounts.SetSignedIn(ctx, id, false); err != nil && !errors.Is(err, ErrAccountNotFound) {
		log.Printf("agenthub: account %s: marking it signed out: %v", id, err)
		return
	}
	s.accountsChanged()
}
```
(adapt `observedAccountID`'s real return shape — it returns `(string, ...)` per Plan 1; keep what the code has). Call it in `recordStop` right after `markAccountLimited`.
(b) `update.go`: add `IsUsingOverage bool \`json:"isUsingOverage,omitempty"\`` to `RateLimit` and read `"isUsingOverage"` in `parseRateLimit`'s anonymous struct. `claude_limits.go`, in `applyRateLimitStatus` (or right after it in the `UpdateUsage` case): 

```go
	// A plan that goes on through overage still answers: a window past 100 % is a
	// warning, not a refusal, unless the event itself says "rejected".
	if rl != nil && rl.IsUsingOverage && rl.Status != "rejected" {
		for i := range limits {
			if limits[i].Status == "reached" {
				limits[i].Status = "warning"
			}
		}
	}
```
(c) `accounts.go`:

```go
// mergeWindowsReport is mergeWindows that also says whether any fresh window was
// accepted (not rejected as a stale reading of the same window).
func mergeWindowsReport(old, fresh []LimitWindow) ([]LimitWindow, bool) { /* the body of mergeWindows, returning accepted = any fresh window replaced or appended */ }

func mergeWindows(old, fresh []LimitWindow) []LimitWindow {
	merged, _ := mergeWindowsReport(old, fresh)
	return merged
}
```
(move the existing stale-rule body into `mergeWindowsReport`; `accepted` is true when at least one fresh window was replaced, appended or equal-and-not-stale.) `SaveUsage` uses it: when `!accepted` it commits nothing and returns nil without the UPDATE. `account_usage.go`/`service.go`: `usageSeen map[string]seenUsage`, `type seenUsage struct{ windows []LimitWindow; at time.Time }`, a package var `usageRefreshEvery = 60 * time.Second`, `Service.usageClock func() time.Time` (nil means `time.Now`), and `usageChanged` returns true when the windows differ OR the stored `at` is older than `usageRefreshEvery`, then stores `{slices.Clone(windows), now}`; update `NewService`'s map literal and `forgetUsageSeen`.
(d) `account_policy_cache.go`:

```go
package agenthub

import (
	"time"

	"devkit/internal/preferences"
)

// accountPolicyTTL is how long a policy read is reused. Every usage update and every
// refused turn asks whether a vendor is configured; that is a preference read on the
// one SQLite connection. A few seconds keeps it off the hot path, and a change made
// in Settings calls InvalidateAccountPolicy so it still applies at the next turn.
const accountPolicyTTL = 3 * time.Second

func (s *Service) cachedAccountPolicy() (preferences.AgentAccountPolicy, error) {
	if s.AccountPolicy == nil {
		return preferences.AgentAccountPolicy{}, nil
	}
	now := s.policyNow()
	s.mu.Lock()
	if !s.policyAt.IsZero() && now.Sub(s.policyAt) < accountPolicyTTL {
		cached := s.policyCached
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()
	policy, err := s.AccountPolicy() // outside the lock: a preference read hits the database
	if err != nil {
		return preferences.AgentAccountPolicy{}, err
	}
	s.mu.Lock()
	s.policyCached, s.policyAt = policy, now
	s.mu.Unlock()
	return policy, nil
}

// InvalidateAccountPolicy drops the cached read; the bridge calls it after a save.
func (s *Service) InvalidateAccountPolicy() {
	s.mu.Lock()
	s.policyAt = time.Time{}
	s.mu.Unlock()
}

func (s *Service) policyNow() time.Time {
	if s.policyClock != nil {
		return s.policyClock()
	}
	return time.Now()
}
```
with fields `policyCached preferences.AgentAccountPolicy`, `policyAt time.Time`, `policyClock func() time.Time` on `Service` (unexported, next to the other account maps). `accountPolicyFor` and `vendorConfigured` call `cachedAccountPolicy()` instead of `s.AccountPolicy()`. Keep the `AccountPolicy == nil` early returns they have.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agenthub/ -run 'ASignInThatDied|ClaudeOverage|MergeWindows|KeepsObservedAt|IdenticalUsageReading|ThePolicyRead|APolicyReadError|TestUsage|TestAccount' -race -count=1 && go test ./internal/agenthub/ ./internal/arch/ -count=1 && go vet ./internal/agenthub/`
Expected: PASS (the existing `usageSeen`, usage and policy tests are updated to the new types where the compiler asks).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: a dead sign-in is skipped, overage is not a refusal, observed usage stays fresh, the policy read is cached"
```

---

### Task 5: The adapters can be asked what an account has left, and whether it is signed in

Codex answers `account/rateLimits/read` through a short-lived app-server; Claude answers a `get_usage` control request and `auth status` through its CLI. Both run under the session jail with the account's home.

**Files:**
- Create: `internal/agenthub/account_probe.go`, `internal/agenthub/claude_cli.go`
- Modify: `internal/codexapp/protocol.go`, `internal/codexapp/items.go`, `internal/agenthub/codex_auth.go` (`oneShot`, `SignInStatus`), `adapter_codex.go`, `codex_translate.go`
- Test: `internal/codexapp/items_test.go`, `internal/agenthub/adapter_codex_test.go` (the fake app-server `runFakeCodex` gets the new request), `internal/agenthub/claude_cli_test.go` (internal package)

- [ ] **Step 1: Write the failing tests.**

`internal/codexapp/items_test.go` (append; package as in that file):

```go
func TestRateLimitsReadResponseDecodesTheMeasuredAnswerAndAFractionalPercent(t *testing.T) {
	const measured = `{"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1792669235},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1793108266},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"individualLimit":null,"spendControlReached":false,"planType":"plus","rateLimitReachedType":null},"rateLimitsByLimitId":{"codex":{"limitId":"codex","limitName":null,"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1792669235},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1793108266},"credits":null,"planType":"plus","rateLimitReachedType":null}},"rateLimitResetCredits":{"availableCount":0,"credits":[]}}`
	var res RateLimitsReadResponse
	if err := json.Unmarshal([]byte(measured), &res); err != nil {
		t.Fatal(err)
	}
	if res.RateLimits.Secondary == nil || res.RateLimits.Secondary.UsedPercent != 10 || len(res.RateLimitsByLimitID) != 1 {
		t.Fatalf("decoded = %+v", res)
	}
	var fractional RateLimitWindow
	if err := json.Unmarshal([]byte(`{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1}`), &fractional); err != nil || fractional.UsedPercent != 12.5 {
		t.Fatalf("a fractional usedPercent must not fail the whole answer: %+v, %v", fractional, err)
	}
}
```
`internal/agenthub/adapter_codex_test.go`: in `runFakeCodex`, next to `case "account/read":`, add the new request (use the same reply helper the neighbours use):

```go
	case codexapp.MethodRateLimitsRead:
		reply(id, `{"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1792669235},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1793108266},"credits":null,"individualLimit":null,"spendControlReached":false,"planType":"plus","rateLimitReachedType":null},"rateLimitsByLimitId":{"codex":{"limitId":"codex","limitName":null,"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1792669235},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1793108266},"credits":null,"planType":"plus","rateLimitReachedType":null}},"rateLimitResetCredits":{"availableCount":0,"credits":[]}}`)
```
and these tests (fixture `newCodexFixture(t, "turn")` is the one the other app-server tests use; pick any scenario that answers `initialize`):

```go
func TestCodexCheckUsageReadsBothWindowsFromTheMeasuredAnswer(t *testing.T) {
	fx := newCodexFixture(t, "turn")
	windows, err := fx.adapter.CheckUsage(t.Context(), OpenSpec{})
	if err != nil {
		t.Fatal(err)
	}
	want := []LimitWindow{
		{Label: "5-hour", UsedPercent: 0, ResetsAt: 1792669235, Status: "ok"},
		{Label: "Weekly", UsedPercent: 10, ResetsAt: 1793108266, Status: "ok"},
	}
	if len(windows) != len(want) || windows[0] != want[0] || windows[1] != want[1] {
		t.Fatalf("windows = %+v, want %+v", windows, want)
	}
}

func TestCodexCheckUsageRunsInTheAccountsHome(t *testing.T) {
	fx := newCodexFixture(t, "turn")
	home := filepath.Join(t.TempDir(), "work")
	if _, err := fx.adapter.CheckUsage(t.Context(), OpenSpec{AccountHome: home}); err != nil {
		t.Fatal(err)
	}
	pol, _ := fx.runner.started()
	if pol.Env["CODEX_HOME"] != home {
		t.Errorf("CODEX_HOME = %q, want the account home %q", pol.Env["CODEX_HOME"], home)
	}
}

func TestCodexRuntimeReadsRateLimitsOnItsOwnConnection(t *testing.T) {
	fx := newCodexFixture(t, "turn")
	rt, err := fx.adapter.Open(t.Context(), OpenSpec{Cwd: fx.cwd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	reader, ok := rt.(RateLimitReader)
	if !ok {
		t.Fatal("the Codex runtime must read rate limits so a running account is never probed by a second process")
	}
	windows, err := reader.ReadRateLimits(t.Context())
	if err != nil || len(windows) != 2 {
		t.Fatalf("windows = %+v, %v", windows, err)
	}
}
```
`internal/agenthub/claude_cli_test.go` (package `agenthub`):

```go
package agenthub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"devkit/internal/sandbox"
)

// cliRunner stands in for the Claude CLI: it records its arguments, its environment
// and everything written to stdin, then prints reply and exits.
type cliRunner struct {
	reply string
	args  *[]string
	env   *map[string]string
	stdin string // path the script writes stdin to
}

func (r cliRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	if r.args != nil {
		*r.args = append([]string(nil), c.Args...)
	}
	if r.env != nil {
		*r.env = p.Env
	}
	cmd := exec.Command("sh", "-c", `cat >"$CAP"; printf '%s\n' "$REPLY"`)
	cmd.Env = append(os.Environ(), "CAP="+r.stdin, "REPLY="+r.reply)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return sandbox.NewProcPiped(cmd, in, out, &bytes.Buffer{}), nil
}

func claudeWithCLI(t *testing.T, reply string) (*ClaudeAdapter, *[]string, *map[string]string, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := new([]string)
	env := new(map[string]string)
	stdin := filepath.Join(t.TempDir(), "stdin")
	return NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: cliRunner{reply: reply, args: args, env: env, stdin: stdin}}), args, env, stdin
}

const measuredUsageReply = `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"subscription_type":"team","rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":69,"resets_at":"2026-10-22T07:20:00.067385+00:00"},"seven_day":{"utilization":16,"resets_at":"2026-10-27T06:00:00.067406+00:00"},"seven_day_opus":null,"model_scoped":[{"display_name":"Fable","utilization":0,"resets_at":"2026-10-27T06:00:00+00:00"}]}}}}`

func TestClaudeCheckUsageSendsGetUsageThroughTheCLIAndReadsPercentages(t *testing.T) {
	a, args, _, stdinPath := claudeWithCLI(t, measuredUsageReply)
	windows, err := a.CheckUsage(t.Context(), OpenSpec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--cli", "-p", "--verbose", "--input-format", "stream-json", "--output-format"} {
		if !slices.Contains(*args, want) {
			t.Errorf("args %v lack %q (the CLI refuses stream-json output without --verbose)", *args, want)
		}
	}
	sent, _ := os.ReadFile(stdinPath)
	if !strings.Contains(string(sent), `"subtype":"get_usage"`) || !strings.Contains(string(sent), `"skip_behaviors":true`) {
		t.Errorf("stdin = %q, want the get_usage control request", sent)
	}
	if len(windows) != 3 || windows[0].Label != "5-hour" || windows[0].UsedPercent != 69 || windows[1].Label != "Weekly" || windows[1].UsedPercent != 16 || windows[2].Label != "Weekly · Fable" {
		t.Fatalf("windows = %+v", windows)
	}
	if want := time.Date(2026, 10, 22, 7, 20, 0, 0, time.UTC).Unix(); windows[0].ResetsAt != want {
		t.Errorf("resetsAt = %d, want %d (the ISO time, to the second)", windows[0].ResetsAt, want)
	}
}

func TestClaudeCheckUsageReadsUtilizationAsPercentNotAsAFraction(t *testing.T) {
	reply := `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":1,"resets_at":"2026-10-22T07:20:00+00:00"}}}}}`
	a, _, _, _ := claudeWithCLI(t, reply)
	windows, err := a.CheckUsage(t.Context(), OpenSpec{})
	if err != nil || len(windows) != 1 || windows[0].UsedPercent != 1 {
		t.Fatalf("1 means 1 %% here (get_usage is 0-100, unlike rate_limit_event): %+v, %v", windows, err)
	}
}

func TestClaudeCheckUsageSaysNotCheckableWhenThereIsNothingToRead(t *testing.T) {
	for name, reply := range map[string]string{
		"signed out":    `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"subscription_type":null,"rate_limits_available":false,"rate_limits":null}}}`,
		"error answer":  `{"type":"control_response","response":{"subtype":"error","request_id":"u1","error":"unknown request"}}`,
		"no answer":     `{"type":"system","subtype":"init"}`,
		"empty windows": `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"rate_limits_available":true,"rate_limits":{}}}}`,
	} {
		a, _, _, _ := claudeWithCLI(t, reply)
		if _, err := a.CheckUsage(t.Context(), OpenSpec{}); !errors.Is(err, ErrUsageNotCheckable) {
			t.Errorf("%s: error = %v, want ErrUsageNotCheckable", name, err)
		}
	}
}

func TestClaudeCheckUsageSkipsLinesThatAreNotItsAnswer(t *testing.T) {
	a, _, _, _ := claudeWithCLI(t, `{"type":"system","subtype":"init"}`+"\n"+measuredUsageReply)
	if windows, err := a.CheckUsage(t.Context(), OpenSpec{}); err != nil || len(windows) != 3 {
		t.Fatalf("windows = %+v, %v", windows, err)
	}
}

func TestClaudeCLIRunsInTheAccountsConfigDirectory(t *testing.T) {
	a, _, env, _ := claudeWithCLI(t, measuredUsageReply)
	home := filepath.Join(t.TempDir(), "work")
	if _, err := a.CheckUsage(t.Context(), OpenSpec{AccountHome: home}); err != nil {
		t.Fatal(err)
	}
	if (*env)["CLAUDE_CONFIG_DIR"] != home {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", (*env)["CLAUDE_CONFIG_DIR"], home)
	}
}

func TestClaudeCheckSignInReadsAuthStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		reply string
		want  SignInState
	}{
		"signed in":  {`{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","subscriptionType":"team"}`, SignInState{SignedIn: true, Email: "a@b.c", Plan: "team"}},
		"signed out": {`{"loggedIn":false,"authMethod":"none"}`, SignInState{}},
	} {
		a, args, _, _ := claudeWithCLI(t, tc.reply)
		got, err := a.CheckSignIn(t.Context(), OpenSpec{})
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", name, got, err, tc.want)
		}
		if !slices.Contains(*args, "auth") || !slices.Contains(*args, "status") || !slices.Contains(*args, "--cli") {
			t.Errorf("%s: args = %v", name, *args)
		}
	}
	a, _, _, _ := claudeWithCLI(t, `not json at all`)
	if _, err := a.CheckSignIn(t.Context(), OpenSpec{}); err == nil {
		t.Error("an unreadable status answer is an error, not \"signed out\"")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/codexapp/ ./internal/agenthub/ -run 'RateLimitsRead|CodexCheckUsage|CodexRuntimeReads|ClaudeCheck|ClaudeCLI' -count=1`
Expected: FAIL (does not compile: `RateLimitsReadResponse`, `CheckUsage`, `RateLimitReader`, `SignInState` undefined).

- [ ] **Step 3: Implement.**

`internal/codexapp/protocol.go`, with the other request names: `MethodRateLimitsRead = "account/rateLimits/read"`. `items.go`: change `RateLimitWindow.UsedPercent` to `float64` (grep every use: `codexLimitWindows` converts with `float64(w.UsedPercent)`, which stays valid; fix any `int64` arithmetic the compiler flags), add `LimitID *string \`json:"limitId"\`` and `PlanType *string \`json:"planType"\`` to `RateLimitSnapshot`, and:

```go
// RateLimitsReadResponse is the answer to account/rateLimits/read (measured against
// codex 0.147.0). RateLimits is the legacy single-bucket view; RateLimitsByLimitID is
// keyed by metered limit id ("codex") and may be null.
type RateLimitsReadResponse struct {
	RateLimits          RateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
}
```
`account_probe.go`:

```go
package agenthub

import (
	"context"
	"errors"
)

// ErrUsageNotCheckable: the vendor could not say what the account has left (signed
// out, an API-key login with no plan limits, a CLI that does not know the request).
var ErrUsageNotCheckable = errors.New("agenthub.usageNotCheckable")

// SignInState is what a vendor's own status check says about a home.
type SignInState struct {
	SignedIn bool
	Email    string
	Plan     string
}

// UsageChecker asks the vendor, through its own CLI under the session jail and with
// spec's account home, what an account has left. It returns the account-wide windows
// (other people's use of a shared login included) and never reads a token.
type UsageChecker interface {
	CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error)
}

// SignInChecker asks the vendor whether a home is signed in, without a prompt.
type SignInChecker interface {
	CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error)
}

// RateLimitReader is a running session's own connection answering the same question
// as a UsageChecker. Used instead of starting a second process on a home a session is
// already using (two processes could refresh the login's tokens at once).
type RateLimitReader interface {
	ReadRateLimits(ctx context.Context) ([]LimitWindow, error)
}
```
`codex_translate.go`:

```go
// limitWindowsOfRead is every window an account/rateLimits/read answer carries: the
// per-bucket view when present (it already holds the "codex" bucket the legacy field
// repeats), else the legacy one. Labels are the ones codexLimitWindows gives the
// updated notification, so one translator serves both.
func limitWindowsOfRead(res codexapp.RateLimitsReadResponse) []LimitWindow {
	if len(res.RateLimitsByLimitID) == 0 {
		return codexLimitWindows(res.RateLimits)
	}
	keys := make([]string, 0, len(res.RateLimitsByLimitID))
	for k := range res.RateLimitsByLimitID {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []LimitWindow
	for _, k := range keys {
		out = append(out, codexLimitWindows(res.RateLimitsByLimitID[k])...)
	}
	return out
}
```
`codex_auth.go`: change `oneShot(ctx, call)` to `oneShot(ctx, spec OpenSpec, call)`, pass `spec` to `a.start(...)` instead of `OpenSpec{}`, and update every caller to pass `OpenSpec{}`. Extract the body of `SignInStatus` into `signInStateAt(ctx, spec OpenSpec) (SignInStatus, error)` (it already runs `initialize` then `account/read {"refreshToken":false}`; it must NOT read the global API key when `spec.AccountHome != ""`), keep `SignInStatus(ctx)` calling it with `OpenSpec{}`. `adapter_codex.go`:

```go
// CheckUsage reads account/rateLimits/read through a short-lived app-server in spec's
// home (the built-in home for the zero spec).
func (a *CodexAdapter) CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error) {
	var res codexapp.RateLimitsReadResponse
	err := a.oneShot(ctx, spec, func(callCtx context.Context, p *codexProcess) error {
		if err := p.conn.Call(callCtx, codexapp.MethodRateLimitsRead, nil, &res); err != nil {
			return p.failure("account/rateLimits/read", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	windows := limitWindowsOfRead(res)
	if len(windows) == 0 {
		return nil, ErrUsageNotCheckable
	}
	return windows, nil
}

// CheckSignIn is the status check for a home, through account/read.
func (a *CodexAdapter) CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error) {
	status, err := a.signInStateAt(ctx, spec)
	if err != nil {
		return SignInState{}, err
	}
	return SignInState{SignedIn: status.SignedIn, Email: status.Email, Plan: status.Plan}, nil
}

// ReadRateLimits answers a UsageChecker's question on this session's own connection.
func (r *codexRuntime) ReadRateLimits(ctx context.Context) ([]LimitWindow, error) {
	var res codexapp.RateLimitsReadResponse
	if err := r.conn.Call(ctx, codexapp.MethodRateLimitsRead, nil, &res); err != nil {
		return nil, err
	}
	windows := limitWindowsOfRead(res)
	if len(windows) == 0 {
		return nil, ErrUsageNotCheckable
	}
	return windows, nil
}

var (
	_ UsageChecker    = (*CodexAdapter)(nil)
	_ SignInChecker   = (*CodexAdapter)(nil)
	_ RateLimitReader = (*codexRuntime)(nil)
)
```
`claude_cli.go`:

```go
package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const claudeCLITimeout = 20 * time.Second

// claudeCLI runs the Claude Code CLI under the session jail with spec's account home
// and returns its stdout. It starts the same node + adapter entry a session does and
// adds --cli, which makes the entry spawn the CLI itself (the bundled native binary
// or $CLAUDE_CODE_EXECUTABLE), so both the "bundled" and "on this machine" choices work.
// stdin, when not empty, is written and closed: the CLI answers and exits. The exit
// status is not an error (`claude auth status` exits 1 when signed out and still answers).
func (a *ClaudeAdapter) claudeCLI(ctx context.Context, spec OpenSpec, args []string, stdin string) ([]byte, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	defer l.releaseLease()
	work, err := os.MkdirTemp("", "devkit-claude-cli-")
	if err != nil {
		return nil, fmt.Errorf("%w: scratch directory: %v", ErrProcessFailed, err)
	}
	defer os.RemoveAll(work)
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	pol, cmd := sandboxFor(work, l, spec)
	cmd.Args = append(append([]string(nil), cmd.Args...), append([]string{"--cli"}, args...)...)
	callCtx, cancel := context.WithTimeout(ctx, claudeCLITimeout)
	defer cancel()
	proc, err := a.runner().StartPiped(callCtx, pol, cmd)
	if err != nil {
		return nil, mapSandboxErr(err)
	}
	in, out := proc.StdinWriter(), proc.StdoutReader()
	if in == nil || out == nil {
		_ = proc.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}
	defer func() { _ = proc.Kill(); _ = proc.Wait() }()
	go func() { <-callCtx.Done(); _ = out.Close() }() // unblocks the read when the time is up
	if stdin != "" {
		_, _ = io.WriteString(in, stdin)
	}
	_ = in.Close()
	data, _ := io.ReadAll(io.LimitReader(out, 4<<20))
	if callCtx.Err() != nil {
		return nil, fmt.Errorf("%w: the Claude CLI did not answer in time", ErrProcessFailed)
	}
	return data, nil
}

const claudeUsageRequest = `{"type":"control_request","request_id":"u1","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// CheckUsage asks the CLI for the data behind /usage (the SDK's get_usage control
// request: experimental, so a shape it does not recognise is "not checkable", never a guess).
func (a *ClaudeAdapter) CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error) {
	out, err := a.claudeCLI(ctx, spec,
		[]string{"-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json"}, claudeUsageRequest)
	if err != nil {
		return nil, err
	}
	return parseClaudeUsage(out)
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// parseClaudeUsage reads the control_response line of a get_usage answer. get_usage's
// utilization is 0-100 already (SDKControlGetUsageResponse), unlike rate_limit_event's
// 0-1 fractions: unifiedPercent must not be used here.
func parseClaudeUsage(out []byte) ([]LimitWindow, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var line struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
				Response  struct {
					Available  bool `json:"rate_limits_available"`
					RateLimits *struct {
						FiveHour       *claudeUsageWindow `json:"five_hour"`
						SevenDay       *claudeUsageWindow `json:"seven_day"`
						SevenDayOpus   *claudeUsageWindow `json:"seven_day_opus"`
						SevenDaySonnet *claudeUsageWindow `json:"seven_day_sonnet"`
						ModelScoped    []struct {
							DisplayName string  `json:"display_name"`
							Utilization *float64 `json:"utilization"`
							ResetsAt    *string  `json:"resets_at"`
						} `json:"model_scoped"`
					} `json:"rate_limits"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "control_response" || line.Response.RequestID != "u1" {
			continue
		}
		if line.Response.Subtype != "success" {
			return nil, fmt.Errorf("%w: %s", ErrUsageNotCheckable, clipLine(line.Response.Error))
		}
		rl := line.Response.Response.RateLimits
		if !line.Response.Response.Available || rl == nil {
			return nil, fmt.Errorf("%w: the account has no plan limits to read", ErrUsageNotCheckable)
		}
		var out []LimitWindow
		for _, w := range []struct {
			key    string
			window *claudeUsageWindow
		}{{"five_hour", rl.FiveHour}, {"seven_day", rl.SevenDay}, {"seven_day_opus", rl.SevenDayOpus}, {"seven_day_sonnet", rl.SevenDaySonnet}} {
			if lw, ok := claudeUsageLimit(claudeLimitLabel(w.key), w.window); ok {
				out = append(out, lw)
			}
		}
		for _, m := range rl.ModelScoped {
			if lw, ok := claudeUsageLimit("Weekly · "+m.DisplayName, &claudeUsageWindow{Utilization: m.Utilization, ResetsAt: m.ResetsAt}); ok {
				out = append(out, lw)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: the answer lists no window", ErrUsageNotCheckable)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: the CLI sent no answer", ErrUsageNotCheckable)
}

func claudeUsageLimit(label string, w *claudeUsageWindow) (LimitWindow, bool) {
	if w == nil || w.Utilization == nil {
		return LimitWindow{}, false
	}
	pct := math.Round(math.Min(100, math.Max(0, *w.Utilization))*100) / 100
	lw := LimitWindow{Label: label, UsedPercent: pct, Status: percentStatus(pct)}
	if w.ResetsAt != nil {
		if at, err := time.Parse(time.RFC3339, *w.ResetsAt); err == nil {
			lw.ResetsAt = at.Unix()
		}
	}
	return lw, true
}

func clipLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// CheckSignIn runs `claude auth status` (JSON; no model call, no prompt).
func (a *ClaudeAdapter) CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error) {
	out, err := a.claudeCLI(ctx, spec, []string{"auth", "status"}, "")
	if err != nil {
		return SignInState{}, err
	}
	var st struct {
		LoggedIn         bool   `json:"loggedIn"`
		Email            string `json:"email"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &st); err != nil {
		return SignInState{}, fmt.Errorf("%w: the status answer could not be read", ErrProcessFailed)
	}
	return SignInState{SignedIn: st.LoggedIn, Email: st.Email, Plan: st.SubscriptionType}, nil
}

var (
	_ UsageChecker  = (*ClaudeAdapter)(nil)
	_ SignInChecker = (*ClaudeAdapter)(nil)
)
```
(`sort` import in `codex_translate.go`; adjust `claudeUsageLimit`'s rounding if a `roundPercent` helper with the same meaning exists. `gofmt` realigns the struct tags.)

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/codexapp/ ./internal/agenthub/ -run 'RateLimitsRead|CodexCheckUsage|CodexRuntimeReads|ClaudeCheck|ClaudeCLI' -race -count=1 && go test ./internal/agenthub/ ./internal/codexapp/ -count=1 && go vet ./internal/agenthub/ ./internal/codexapp/`
Expected: PASS; every existing Codex sign-in and catalog test passes unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/codexapp internal/agenthub
git commit -m "agenthub: Codex and Claude can be asked what an account has left and whether it is signed in"
```

---

### Task 6: Checking an account's usage on demand, and before a turn for a shared one

**Files:**
- Create: `internal/agenthub/account_check.go`
- Modify: `internal/agenthub/service.go` (fields `checks usageChecks`, `checkClock func() time.Time`), `internal/agenthub/account_switch.go` (`switchAccountForTurn` calls `refreshStaleForTurn`), `internal/agenthub/export_test.go` (seams)
- Test: `internal/agenthub/account_check_test.go` (external; extends the rig's vendor with `CheckUsage`)

- [ ] **Step 1: Write the failing tests.** Extend the rig's vendor wrapper (`countedMock` in `account_switch_test.go`) so a test can supply a usage checker:

```go
// in account_switch_test.go
type usageProbe struct {
	calls   atomic.Int32
	specs   []agenthub.OpenSpec
	mu      sync.Mutex
	answer  func(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error)
}

func (m *countedMock) CheckUsage(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
	if m.probe == nil {
		return nil, agenthub.ErrUsageNotCheckable
	}
	m.probe.calls.Add(1)
	m.probe.mu.Lock()
	m.probe.specs = append(m.probe.specs, spec)
	m.probe.mu.Unlock()
	return m.probe.answer(ctx, spec)
}
```
(add a `probe *usageProbe` field to `countedMock` and a `rigOptions.probe`; keep `newAccountRig` unchanged for existing tests.) Seams in `export_test.go`:

```go
// SetCheckClockForTest replaces the clock the usage-check floor reads.
func (s *Service) SetCheckClockForTest(now func() time.Time) { s.mu.Lock(); s.checkClock = now; s.mu.Unlock() }

// SetTurnCheckTimeoutForTest shortens how long a turn waits for a pre-turn check; call the returned func to restore it.
func SetTurnCheckTimeoutForTest(d time.Duration) (restore func()) {
	old := turnCheckTimeout
	turnCheckTimeout = d
	return func() { turnCheckTimeout = old }
}

// SetRuntimeForTest puts rt in the service's runtime map for the session and records the account it runs as.
func (s *Service) SetRuntimeForTest(sessionID, accountID string, rt SessionRuntime) {
	s.mu.Lock()
	s.runtimes[sessionID] = rt
	s.sessionAccount[sessionID] = accountID
	s.mu.Unlock()
}
```
`account_check_test.go`:

```go
package agenthub_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func windows(pct float64) []agenthub.LimitWindow {
	return []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: pct, ResetsAt: time.Now().Add(24 * time.Hour).Unix(), Status: "ok"}}
}

func checkRig(t *testing.T, answer func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error)) (*accountRig, *usageProbe) {
	t.Helper()
	probe := &usageProbe{answer: answer}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{probe: probe})
	return rig, probe
}

func TestRefreshAccountUsageStoresTheCheckedReadingWholesale(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(42), nil })
	ctx := uiCtx()
	if err := rig.book.SaveUsage(context.Background(), "work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 90}}, "observed", time.Now()); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	got, err := rig.svc.RefreshAccountUsage(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 42 || got.UsageSource != "checked" {
		t.Fatalf("account = %+v, want exactly the checked window (the observed 5-hour one is not in the new reading)", got)
	}
	if probe.calls.Load() != 1 || changed != 1 {
		t.Errorf("checks = %d, events = %d, want 1 and 1", probe.calls.Load(), changed)
	}
	if home := probe.specs[0].AccountHome; home != rig.home {
		t.Errorf("the probe ran with home %q, want the account's %q", home, rig.home)
	}
}

func TestRefreshAccountUsageSaysWhenUsageCannotBeRead(t *testing.T) {
	rig, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		return nil, agenthub.ErrUsageNotCheckable
	})
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); !errors.Is(err, agenthub.ErrUsageNotCheckable) {
		t.Fatalf("error = %v, want ErrUsageNotCheckable", err)
	}
	got, _ := rig.book.Get(context.Background(), "work")
	if len(got.Usage) != 0 {
		t.Errorf("a failed check must store nothing: %+v", got.Usage)
	}
}

func TestRefreshAccountUsageIsForTheWindowOnly(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(1), nil })
	if _, err := rig.svc.RefreshAccountUsage(agentCtx("11111111-1111-1111-1111-111111111111"), "work"); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Fatalf("an agent: %v, want ErrPermissionDenied", err)
	}
	if probe.calls.Load() != 0 {
		t.Error("an agent must not make DevKit start a vendor process")
	}
}

func TestTwoRefreshesOfOneAccountAreOneCheck(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	rig, probe := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		<-release
		return windows(10), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	time.Sleep(50 * time.Millisecond) // the second caller reaches the flight and waits on it
	close(release)
	wg.Wait()
	if probe.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 (single flight per account)", probe.calls.Load())
	}
}

func TestARefreshInsideTheFloorAsksNothing(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(10), nil })
	now := time.Unix(1_800_000_000, 0)
	rig.svc.SetCheckClockForTest(func() time.Time { return now })
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1: a second press within 10 s must not start another process", probe.calls.Load())
	}
	now = now.Add(11 * time.Second)
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil || probe.calls.Load() != 2 {
		t.Fatalf("after the floor: checks = %d, err = %v, want 2", probe.calls.Load(), err)
	}
}

func markShared(t *testing.T, rig *accountRig, id string) {
	t.Helper()
	acct, err := rig.book.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	acct.Shared = true
	if err := rig.book.Put(context.Background(), acct); err != nil {
		t.Fatal(err)
	}
}

func TestATurnChecksASharedAccountWhoseReadingIsOldAndActsOnTheAnswer(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(95), nil })
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock)) // a team login: never read, so stale
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() == 0 {
		t.Fatal("the shared account's reading is older than the policy allows: it must be checked before the pick")
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("the checked 95%% must move the turn to work; account = %q", got)
	}
}

func TestATurnDoesNotCheckAnUnsharedOrFreshAccount(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(95), nil })
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("an account nobody else uses is read from its own sessions: checks = %d", probe.calls.Load())
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	rig.fillBuiltin(t, 10) // a reading made just now
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("a fresh reading needs no check: checks = %d", probe.calls.Load())
	}
}

func TestATurnDoesNotWaitLongForACheck(t *testing.T) {
	restore := agenthub.SetTurnCheckTimeoutForTest(50 * time.Millisecond)
	defer restore()
	rig, _ := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	sess := startSession(t, rig)
	done := make(chan error, 1)
	go func() {
		_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a check that timed out must not fail the turn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn waited for a check that never answers")
	}
}

type fakeRateRuntime struct {
	agenthub.SessionRuntime
	windows []agenthub.LimitWindow
}

func (f fakeRateRuntime) ReadRateLimits(context.Context) ([]agenthub.LimitWindow, error) { return f.windows, nil }

func TestARunningAccountIsReadThroughItsOwnSessionNotASecondProcess(t *testing.T) {
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(1), nil })
	rig.svc.SetRuntimeForTest("sess-running", "work", fakeRateRuntime{windows: windows(77)})
	got, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Error("a session already runs on this home: a second process could refresh its tokens at once")
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 77 {
		t.Errorf("usage = %+v, want the running session's reading", got.Usage)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agenthub/ -run 'RefreshAccountUsage|TwoRefreshes|InsideTheFloor|ATurnChecks|ATurnDoesNot|RunningAccountIsRead' -count=1`
Expected: FAIL (does not compile: `RefreshAccountUsage`, `probe` option, seams undefined).

- [ ] **Step 3: Implement.** Fields in `service.go` (after the account maps): `checks usageChecks` and `checkClock func() time.Time` (unexported). `account_check.go`:

```go
package agenthub

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	usageCheckSlots = 2                // checks running at once, app-wide
	manualCheckGap  = 10 * time.Second // a person's Refresh, per account
	autoCheckGap    = 60 * time.Second // a pre-turn check, per account
)

// turnCheckTimeout is how long a turn waits for a pre-turn check; a var for tests.
var turnCheckTimeout = 5 * time.Second

type usageFlight struct {
	done    chan struct{}
	windows []LimitWindow
	err     error
}

// usageChecks keeps what the guards need: one check per account at a time, a floor on
// how often one account is asked, and a cap on checks running at once. The zero value works.
type usageChecks struct {
	once   sync.Once
	mu     sync.Mutex
	last   map[string]time.Time
	flight map[string]*usageFlight
	slots  chan struct{}
}

func (c *usageChecks) init() {
	c.once.Do(func() {
		c.last = map[string]time.Time{}
		c.flight = map[string]*usageFlight{}
		c.slots = make(chan struct{}, usageCheckSlots)
	})
}

func (s *Service) checkNow() time.Time {
	s.mu.Lock()
	clock := s.checkClock
	s.mu.Unlock()
	if clock != nil {
		return clock()
	}
	return time.Now()
}

// accountProbeSpec is the OpenSpec a vendor probe runs under for an account: its home
// and shared store, the data-directory protection and the person's home-sharing choice.
// (Not applySandboxExtras: a probe needs no Docker, customization or subagents.)
func (s *Service) accountProbeSpec(acct AccountRecord) OpenSpec {
	spec := OpenSpec{}
	if !acct.Builtin() {
		spec.AccountHome = acct.HomeDir
		if s.AccountSharedHome != nil {
			spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
		}
	}
	if s.ProtectedPaths != nil {
		spec.DenyReadPaths = append(spec.DenyReadPaths, s.ProtectedPaths()...)
	}
	if s.SandboxExtras != nil && s.SandboxExtras().HomeShared() {
		shared := false
		spec.PrivateHome = &shared
	}
	return spec
}

// liveRuntimeOn is a running session's runtime that runs as the account, if any.
func (s *Service) liveRuntimeOn(accountID string) SessionRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, rt := range s.runtimes {
		if s.sessionAccount[sessionID] == accountID {
			return rt
		}
	}
	return nil
}

// checkUsage asks the vendor what the account has left and stores the answer as the
// account's whole "checked" reading. asked is false when nothing was started because
// the account was asked less than minGap ago. Concurrent callers for one account share
// one check; at most usageCheckSlots run at once. A session already running on the
// account's home answers on its own connection instead of a second process (two
// processes could refresh the login's tokens at the same moment).
func (s *Service) checkUsage(ctx context.Context, acct AccountRecord, minGap time.Duration) (windows []LimitWindow, asked bool, err error) {
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return nil, false, ErrVendorUnknown
	}
	checker, isChecker := adapter.(UsageChecker)
	c := &s.checks
	c.init()
	now := s.checkNow()
	c.mu.Lock()
	if running, ok := c.flight[acct.ID]; ok {
		c.mu.Unlock()
		select {
		case <-running.done:
			return running.windows, true, running.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	if last := c.last[acct.ID]; !last.IsZero() && now.Sub(last) < minGap {
		c.mu.Unlock()
		return nil, false, nil
	}
	f := &usageFlight{done: make(chan struct{})}
	c.flight[acct.ID] = f
	c.last[acct.ID] = now
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.flight, acct.ID)
		c.mu.Unlock()
		f.windows, f.err = windows, err
		close(f.done)
	}()

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	if rt := s.liveRuntimeOn(acct.ID); rt != nil {
		if reader, ok := rt.(RateLimitReader); ok {
			windows, err = reader.ReadRateLimits(ctx)
		}
	}
	if windows == nil && err == nil {
		if !isChecker {
			return nil, true, ErrUsageNotCheckable
		}
		windows, err = checker.CheckUsage(ctx, s.accountProbeSpec(acct))
	}
	if err != nil {
		return nil, true, err
	}
	if s.Accounts != nil {
		if saveErr := s.Accounts.ReplaceUsage(ctx, acct.ID, windows, "checked", now); saveErr != nil {
			return nil, true, saveErr
		}
		// The next identical observed reading must not be skipped as "unchanged".
		s.forgetUsageSeen(acct.ID)
		s.accountsChanged()
	}
	return windows, true, nil
}

// RefreshAccountUsage is the Refresh button: one account, window only.
func (s *Service) RefreshAccountUsage(ctx context.Context, id string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if _, _, err := s.checkUsage(ctx, acct, manualCheckGap); err != nil {
		return acct, err
	}
	return book.Get(ctx, id)
}

// refreshStaleForTurn runs at the start of a turn, before the pick: a shared account's
// reading older than the policy allows is asked again, because what this app's sessions
// saw is only a lower bound of a login other people use. The session's own account is
// checked first, then the one the pick is about to choose. Each check is time-boxed; a
// failure keeps the old reading and never fails or delays the turn beyond the timeout.
func (s *Service) refreshStaleForTurn(ctx context.Context, sess *AgentSession) {
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return
	}
	maxAge := time.Duration(vp.UsageMaxAgeSeconds) * time.Second
	if maxAge <= 0 {
		maxAge = 300 * time.Second
	}
	stale := func(a *AccountRecord) bool {
		return a != nil && a.Shared && a.SignedIn && (a.ObservedAt.IsZero() || s.checkNow().Sub(a.ObservedAt) > maxAge)
	}
	check := func(a AccountRecord) {
		cctx, cancel := context.WithTimeout(ctx, turnCheckTimeout)
		defer cancel()
		if _, _, err := s.checkUsage(cctx, a, autoCheckGap); err != nil && !errors.Is(err, ErrUsageNotCheckable) {
			log.Printf("agenthub: account %s: checking its usage before a turn: %v", a.ID, err)
		}
	}
	accounts, err := s.Accounts.List(ctx, sess.AgentType)
	if err != nil {
		return
	}
	if cur := findAccount(accounts, sess.AccountID); stale(cur) {
		check(*cur)
	}
	d, ok := s.decideAccount(ctx, sess)
	if !ok || !d.Switch {
		return
	}
	if accounts, err = s.Accounts.List(ctx, sess.AgentType); err != nil {
		return
	}
	if target := findAccount(accounts, d.Target); stale(target) {
		check(*target)
	}
}
```
In `switchAccountForTurn`, first line: `s.refreshStaleForTurn(ctx, sess)` (the existing `decideAccount` then runs on the refreshed book). Add `probe *usageProbe` to `rigOptions` and `countedMock` in the rig and wire `newAccountRig` to leave it nil.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agenthub/ -run 'RefreshAccountUsage|TwoRefreshes|InsideTheFloor|ATurnChecks|ATurnDoesNot|RunningAccountIsRead' -race -count=3 && go test ./internal/agenthub/ ./internal/arch/ -count=1 && go vet ./internal/agenthub/`
Expected: PASS (`TwoRefreshes…` is the one to run several times).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: usage is checked on demand and, for a shared account, before a turn"
```

---

### Task 7: Signing in to an added account, and knowing whether it is

**Files:**
- Create: `internal/agenthub/account_signin.go`
- Modify: `internal/agenthub/account_probe.go` (`AccountAuthProvider`, `AccountInAppSignIn`), `adapter_claude.go` (`SignInCommand` → `signInCommand`), `codex_auth.go` (`startSignIn`, `codexSignIn`, `SignInResult`, `finishSignIn`), `adapter_servermode.go` (refuse)
- Test: `account_signin_test.go` (external, rig), additions to `auth_test.go` (Claude command), `adapter_codex_test.go` (Codex at a home)

- [ ] **Step 1: Write the failing tests.**

Claude (in `auth_test.go`, reusing its fixtures `measuredAuthMethods` and `scriptedAgentRunner`; build the adapter exactly as `TestAuthMethodsProbesOnceAndResolvesSignIn` does):

```go
func TestSignInCommandForPutsTheAccountHomeInTheLoginEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/the/hosts/own")
	a := newScriptedClaude(t) // the helper that test already uses to get an adapter whose initialize answers measuredAuthMethods
	home := filepath.Join(t.TempDir(), "work")
	cmd, err := a.SignInCommandFor(t.Context(), "claude-ai-login", home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cmd.Release)
	var got []string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			got = append(got, kv)
		}
	}
	if len(got) != 1 || got[0] != "CLAUDE_CONFIG_DIR="+home {
		t.Fatalf("CLAUDE_CONFIG_DIR entries = %v, want exactly the account's home (the host's own value must lose)", got)
	}
	if n := len(cmd.Args); n < 4 || cmd.Args[n-4] != "--cli" || cmd.Args[n-3] != "auth" || cmd.Args[n-2] != "login" {
		t.Errorf("args = %v, want the adapter's --cli auth login …", cmd.Args)
	}
	plain, err := a.SignInCommand(t.Context(), "claude-ai-login")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(plain.Release)
	for _, kv := range plain.Env {
		if kv == "CLAUDE_CONFIG_DIR="+home {
			t.Error("the built-in sign-in must not pick up an account's home")
		}
	}
}
```
(if no such constructor helper exists, copy the three lines that test uses to build the adapter and the runner.) Codex (in `adapter_codex_test.go`, scenario `login`, helpers `newCodexFixture`, `waitSignIn`, `fx.runner.started()` as the existing sign-in tests):

```go
func TestCodexSignInAtAnAccountHomeCarriesTheAccountToTheResult(t *testing.T) {
	fx := newCodexFixture(t, "login")
	home := filepath.Join(t.TempDir(), "work")
	flow, err := fx.adapter.StartBrowserSignInAt(t.Context(), OpenSpec{AccountHome: home}, "codex-work")
	if err != nil {
		t.Fatal(err)
	}
	result := waitSignIn(t, fx, flow.LoginID)
	if !result.Success || result.AccountID != "codex-work" {
		t.Fatalf("result = %+v, want a success carrying the account id", result)
	}
	if pol, _ := fx.runner.started(); pol.Env["CODEX_HOME"] != home {
		t.Errorf("CODEX_HOME = %q, want %q", pol.Env["CODEX_HOME"], home)
	}
}

func TestCodexBuiltInSignInResultHasNoAccount(t *testing.T) {
	fx := newCodexFixture(t, "login")
	flow, err := fx.adapter.StartBrowserSignIn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSignIn(t, fx, flow.LoginID); result.AccountID != "" {
		t.Errorf("the built-in sign-in carries account %q, want none (it must keep adding the Accounts row)", result.AccountID)
	}
}

func TestCodexCheckSignInAtAHome(t *testing.T) {
	fx := newCodexFixture(t, "signedout")
	state, err := fx.adapter.CheckSignIn(t.Context(), OpenSpec{AccountHome: filepath.Join(t.TempDir(), "work")})
	if err != nil || state.SignedIn {
		t.Fatalf("state = %+v, %v, want signed out", state, err)
	}
}
```
(if `waitSignIn` returns something other than `SignInResult`, adapt the assertion.) Service level, `account_signin_test.go` (the rig's vendor wrapper gets small fakes for the four optional interfaces):

```go
package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func TestAccountSignInCommandPreparesTheHomeAndAsksTheVendorForItsCommand(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	cmd, err := rig.svc.AccountSignInCommand(uiCtx(), "work", "login-method")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Label != "fake login" || rig.signIn.home != rig.home {
		t.Fatalf("cmd = %+v, the vendor was asked for home %q (want %q)", cmd, rig.signIn.home, rig.home)
	}
}

func TestAccountSignInCommandRefusesTheBuiltInAnAgentAndAnUnsupportedVendor(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.AccountSignInCommand(uiCtx(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "m"); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("built-in: %v, want ErrInvalidAccount (it signs in from Runtimes)", err)
	}
	if _, err := rig.svc.AccountSignInCommand(agentCtx("11111111-1111-1111-1111-111111111111"), "work", "m"); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("agent: %v, want ErrPermissionDenied", err)
	}
	plain := newAccountRig(t, preferences.RefusalNone) // its vendor implements none of the sign-in interfaces
	if _, err := plain.svc.AccountSignInCommand(uiCtx(), "work", "m"); !errors.Is(err, agenthub.ErrAuthMethodUnknown) {
		t.Errorf("vendor without terminal sign-in: %v, want ErrAuthMethodUnknown", err)
	}
}

func TestStartAccountSignInUsesTheAccountsHome(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	flow, err := rig.svc.StartAccountSignIn(uiCtx(), "work", "browser")
	if err != nil || flow.LoginID != "login-1" {
		t.Fatalf("flow = %+v, %v", flow, err)
	}
	if rig.signIn.startedHome != rig.home || rig.signIn.startedAccount != "work" {
		t.Errorf("sign-in started with home %q account %q", rig.signIn.startedHome, rig.signIn.startedAccount)
	}
	if _, err := rig.svc.StartAccountSignIn(uiCtx(), "work", "carrier-pigeon"); !errors.Is(err, agenthub.ErrAuthMethodUnknown) {
		t.Errorf("unknown method: %v, want ErrAuthMethodUnknown", err)
	}
}

func TestRefreshAccountStatusRecordsWhatTheVendorSays(t *testing.T) {
	fake := &signInFake{state: agenthub.SignInState{SignedIn: true}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	acct, err := rig.svc.RefreshAccountStatus(uiCtx(), "work")
	if err != nil || !acct.SignedIn {
		t.Fatalf("account = %+v, %v", acct, err)
	}
	if changed != 0 {
		t.Errorf("nothing changed (work was already signed in): %d events", changed)
	}
	fake.state = agenthub.SignInState{SignedIn: false}
	acct, err = rig.svc.RefreshAccountStatus(uiCtx(), "work")
	if err != nil || acct.SignedIn || changed != 1 {
		t.Fatalf("after the vendor says signed out: %+v, %v, events %d", acct, err, changed)
	}
	if rig.signIn.checkedHome != rig.home {
		t.Errorf("the status was read from %q, want the account's home", rig.signIn.checkedHome)
	}
}

func TestAfterAccountSignInRunsTheStatusCheckWithoutAWindowCaller(t *testing.T) {
	fake := &signInFake{state: agenthub.SignInState{SignedIn: true}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	if err := rig.book.SetSignedIn(context.Background(), "work", false); err != nil {
		t.Fatal(err)
	}
	rig.svc.AfterAccountSignIn("work")
	if got, _ := rig.book.Get(context.Background(), "work"); !got.SignedIn {
		t.Error("a finished sign-in must leave the account marked signed in")
	}
}
```
with this fake added to `account_switch_test.go` (and `rigOptions.signIn *signInFake` wired onto the vendor wrapper, which implements `SignInCommandFor`, `StartBrowserSignInAt`, `StartDeviceSignInAt`, `CheckSignIn` by delegating to it):

```go
type signInFake struct {
	state           agenthub.SignInState
	home            string // home the terminal command was asked for
	startedHome     string
	startedAccount  string
	checkedHome     string
}

func (f *signInFake) commandFor(methodID, home string) (agenthub.SignInCommand, error) {
	if methodID != "login-method" {
		return agenthub.SignInCommand{}, agenthub.ErrAuthMethodUnknown
	}
	f.home = home
	return agenthub.SignInCommand{Path: "/bin/true", Label: "fake login", Release: func() {}}, nil
}
```
(`SignInCommandFor(ctx, methodID, home)` → `commandFor`; `StartBrowserSignInAt(ctx, spec, accountID)` records `spec.AccountHome` and `accountID` and returns `SignInFlow{LoginID: "login-1"}`; `CheckSignIn(ctx, spec)` records `checkedHome` and returns `state`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agenthub/ -run 'SignInCommandFor|CodexSignInAtAn|CodexBuiltInSignIn|CodexCheckSignIn|AccountSignInCommand|StartAccountSignIn|RefreshAccountStatus|AfterAccountSignIn' -count=1`
Expected: FAIL (does not compile: `SignInCommandFor`, `StartBrowserSignInAt`, `AccountID`, `AccountSignInCommand` undefined).

- [ ] **Step 3: Implement.** `account_probe.go` (append):

```go
// AccountAuthProvider is a vendor that signs in through a terminal (Claude): the same
// command as SignInCommand, run with an account's home as its config directory.
type AccountAuthProvider interface {
	SignInCommandFor(ctx context.Context, methodID, home string) (SignInCommand, error)
}

// AccountInAppSignIn is a vendor that signs in inside the app (Codex), at an account's
// home. accountID rides on the result so the window can tell it from the built-in sign-in.
type AccountInAppSignIn interface {
	StartBrowserSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error)
	StartDeviceSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error)
}
```
`adapter_claude.go`: turn `SignInCommand(ctx, methodID)` into a wrapper around `signInCommand(ctx, methodID, "")` and add:

```go
func (a *ClaudeAdapter) SignInCommandFor(ctx context.Context, methodID, home string) (SignInCommand, error) {
	return a.signInCommand(ctx, methodID, home)
}
```
where `signInCommand` is the existing body with one change: `env := map[string]string{}`; copy `l.env` into it; `if home != "" { env["CLAUDE_CONFIG_DIR"] = home }`; `Env: signInEnv(env)` (`signInEnv` already lets a key in the map beat the process environment, case-insensitively). `var _ AccountAuthProvider = (*ClaudeAdapter)(nil)`. `codex_auth.go`: `startSignIn(ctx, spec OpenSpec, accountID, loginType, method string)` passes `spec` to `a.start(...)` instead of `OpenSpec{}`; `codexSignIn` gets `accountID string`; `SignInResult` gets `AccountID string \`json:"accountId,omitempty"\`` and `finishSignIn` fills it from the flow; `StartBrowserSignIn`/`StartDeviceSignIn` call `startSignIn(ctx, OpenSpec{}, "", …)`; add

```go
func (a *CodexAdapter) StartBrowserSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error) {
	return a.startSignIn(ctx, spec, accountID, "chatgpt", "browser")
}

func (a *CodexAdapter) StartDeviceSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error) {
	return a.startSignIn(ctx, spec, accountID, "chatgptDeviceCode", "deviceCode")
}

var _ AccountInAppSignIn = (*CodexAdapter)(nil)
```
(use the same `loginType`/`method` strings the existing `StartBrowserSignIn`/`StartDeviceSignIn` pass; the sign-in holds the loopback callback port, so starting one cancels another in progress — say so in the doc comment). `adapter_servermode.go`: `ServerModeAdapter` implements `SignInCommandFor`, `StartBrowserSignInAt`, `StartDeviceSignInAt`, `CheckSignIn`, `CheckUsage` returning `ErrUnavailableInServerMode` (it already does for the built-in sign-in). `account_signin.go`:

```go
package agenthub

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (s *Service) accountForSignIn(ctx context.Context, id string) (AccountRecord, VendorAdapter, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, nil, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, nil, err
	}
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return AccountRecord{}, nil, ErrVendorUnknown
	}
	return acct, adapter, nil
}

// prepareForSignIn makes the home (and its store link) a login is about to write into.
func (s *Service) prepareForSignIn(acct AccountRecord) error {
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account signs in from Settings → Agents → Runtimes", ErrInvalidAccount)
	}
	if s.AccountSharedHome == nil {
		return ErrAccountsUnavailable
	}
	return PrepareAccountHome(acct.Vendor, acct.HomeDir, s.AccountSharedHome(acct.Vendor))
}

// AccountSignInCommand is the terminal sign-in for an added account (Claude): the vendor's
// own login command with the account's home as its config directory. DevKit never sees
// the login; the terminal tab runs it and the window learns how it ended from its exit.
func (s *Service) AccountSignInCommand(ctx context.Context, accountID, methodID string) (SignInCommand, error) {
	acct, adapter, err := s.accountForSignIn(ctx, accountID)
	if err != nil {
		return SignInCommand{}, err
	}
	provider, ok := adapter.(AccountAuthProvider)
	if !ok {
		return SignInCommand{}, ErrAuthMethodUnknown
	}
	if err := s.prepareForSignIn(acct); err != nil {
		return SignInCommand{}, err
	}
	return provider.SignInCommandFor(ctx, methodID, acct.HomeDir)
}

// StartAccountSignIn is the in-app sign-in for an added account (Codex). method is
// "browser" or "deviceCode". It ends with agenthub:signInCompleted carrying accountId.
func (s *Service) StartAccountSignIn(ctx context.Context, accountID, method string) (SignInFlow, error) {
	acct, adapter, err := s.accountForSignIn(ctx, accountID)
	if err != nil {
		return SignInFlow{}, err
	}
	starter, ok := adapter.(AccountInAppSignIn)
	if !ok || (method != "browser" && method != "deviceCode") {
		return SignInFlow{}, ErrAuthMethodUnknown
	}
	if err := s.prepareForSignIn(acct); err != nil {
		return SignInFlow{}, err
	}
	spec := s.accountProbeSpec(acct)
	if method == "deviceCode" {
		return starter.StartDeviceSignInAt(ctx, spec, acct.ID)
	}
	return starter.StartBrowserSignInAt(ctx, spec, acct.ID)
}

// refreshStatus asks the vendor whether the account's home is signed in and records it.
func (s *Service) refreshStatus(ctx context.Context, acct AccountRecord) (AccountRecord, error) {
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return acct, ErrVendorUnknown
	}
	checker, ok := adapter.(SignInChecker)
	if !ok {
		return acct, ErrAuthMethodUnknown
	}
	state, err := checker.CheckSignIn(ctx, s.accountProbeSpec(acct))
	if err != nil {
		return acct, err
	}
	if state.SignedIn != acct.SignedIn {
		if err := s.Accounts.SetSignedIn(ctx, acct.ID, state.SignedIn); err != nil {
			return acct, err
		}
		s.accountsChanged()
	}
	return s.Accounts.Get(ctx, acct.ID)
}

// RefreshAccountStatus is the "Refresh status" action, window only.
func (s *Service) RefreshAccountStatus(ctx context.Context, id string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, err
	}
	return s.refreshStatus(ctx, acct)
}

// AfterAccountSignIn runs when a sign-in the window started has ended (the terminal
// exited, or the in-app flow completed): it re-reads the account's status. It has no
// caller identity of its own — the bridge and main.go call it after their own checks.
func (s *Service) AfterAccountSignIn(accountID string) {
	if s.Accounts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	acct, err := s.Accounts.Get(ctx, accountID)
	if err != nil {
		return
	}
	if _, err := s.refreshStatus(ctx, acct); err != nil {
		log.Printf("agenthub: account %s: reading its status after a sign-in: %v", accountID, err)
	}
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agenthub/ -run 'SignInCommandFor|CodexSignInAtAn|CodexBuiltInSignIn|CodexCheckSignIn|AccountSignInCommand|StartAccountSignIn|RefreshAccountStatus|AfterAccountSignIn' -race -count=1 && go test ./internal/agenthub/ -count=1 && go vet ./internal/agenthub/`
Expected: PASS; the existing sign-in tests (`TestCodexBrowserSignIn`, `TestAuthMethods…`) pass unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: signing in to an added account, and reading whether it is signed in"
```

---

### Task 8: The transcript carries the account notices as a typed field

Go already writes `session_info_update` rows with `_meta.devkit.accountSwitch` / `accountSwitchFailed` (`{from, to, reason}`), and the frontend drops them. A typed field (like `Failure`) is what the window reads; no new update kind.

**Files:**
- Modify: `internal/agenthub/update.go` (`SessionUpdate`, `wireSessionInfoMeta`, `case UpdateSessionInfo`)
- Test: `internal/agenthub/update_meta_test.go` (same package as that file)

- [ ] **Step 1: Write the failing test** (append; use the package name the file already has — `ParseSessionUpdate` is exported):

```go
func TestAccountSwitchNoticesAreTypedOnTheUpdate(t *testing.T) {
	sw := ParseSessionUpdate([]byte(`{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":{"from":"claude-default","to":"claude-work","reason":"Weekly 95% ≥ 90%"}}}}`))
	if sw.Kind != UpdateSessionInfo || sw.AccountNotice == nil {
		t.Fatalf("update = %+v, want a session_info_update with an account notice", sw)
	}
	if got := *sw.AccountNotice; got.Kind != "switch" || got.From != "claude-default" || got.To != "claude-work" || got.Reason != "Weekly 95% ≥ 90%" {
		t.Errorf("notice = %+v", got)
	}
	failed := ParseSessionUpdate([]byte(`{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitchFailed":{"from":"a","to":"b","reason":"sign-in expired"}}}}`))
	if failed.AccountNotice == nil || failed.AccountNotice.Kind != "failed" {
		t.Errorf("notice = %+v, want kind failed", failed.AccountNotice)
	}
	if plain := ParseSessionUpdate([]byte(`{"sessionUpdate":"session_info_update","title":"x"}`)); plain.AccountNotice != nil {
		t.Errorf("an ordinary info update has no notice: %+v", plain.AccountNotice)
	}
	if junk := ParseSessionUpdate([]byte(`{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":"nope"}}}`)); junk.AccountNotice != nil {
		t.Errorf("a malformed notice is no notice: %+v", junk.AccountNotice)
	}
	long := ParseSessionUpdate([]byte(`{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":{"from":"a","to":"b","reason":"` + strings.Repeat("x", 600) + `"}}}}`))
	if long.AccountNotice == nil || utf8.RuneCountInString(long.AccountNotice.Reason) > 300 {
		t.Errorf("the reason is clipped to 300 runes: %+v", long.AccountNotice)
	}
}
```
(imports `strings`, `unicode/utf8` if the file lacks them.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestAccountSwitchNoticesAreTyped -count=1`
Expected: FAIL (`AccountNotice` undefined).

- [ ] **Step 3: Implement.** `update.go`:

```go
// AccountNotice is a move between vendor accounts, carried on a session_info_update's
// _meta.devkit. Kind is "switch" (a move that stands) or "failed" (one that was tried
// and undone). From and To are account ids; the built-in account's is "<vendor>-default".
type AccountNotice struct {
	Kind   string `json:"kind"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}
```
add `AccountNotice *AccountNotice \`json:"accountNotice,omitempty"\`` to `SessionUpdate`, and:

```go
type accountNoticeWire struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// parseAccountNotice reads _meta.devkit.accountSwitch / accountSwitchFailed. Anything
// malformed is no notice (the row is still stored as an ordinary info update).
func parseAccountNotice(meta json.RawMessage) *AccountNotice {
	if len(meta) == 0 {
		return nil
	}
	var wire struct {
		DevKit struct {
			Switch json.RawMessage `json:"accountSwitch"`
			Failed json.RawMessage `json:"accountSwitchFailed"`
		} `json:"devkit"`
	}
	if json.Unmarshal(meta, &wire) != nil {
		return nil
	}
	read := func(kind string, raw json.RawMessage) *AccountNotice {
		var n accountNoticeWire
		if len(raw) == 0 || json.Unmarshal(raw, &n) != nil {
			return nil
		}
		reason := n.Reason
		if r := []rune(reason); len(r) > 300 {
			reason = string(r[:300])
		}
		return &AccountNotice{Kind: kind, From: n.From, To: n.To, Reason: reason}
	}
	if notice := read("failed", wire.DevKit.Failed); notice != nil {
		return notice
	}
	return read("switch", wire.DevKit.Switch)
}
```
and in `case UpdateSessionInfo:` after the goal parse: `out.AccountNotice = parseAccountNotice(wire.Meta)`. (`wire.Meta` is the raw `_meta` the other session-info parsers read; use the same field they use.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'TestAccountSwitchNoticesAreTyped|TestParse|Update|Transcript' -race -count=1 && go test ./internal/agenthub/ -count=1`
Expected: PASS (the transcript golden test is unchanged: no stored row carries the new field).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: an account move is a typed notice on the session info update"
```

---

### Task 9: The bridge: UI-only methods, grants, published errors

**Files:**
- Create: `internal/bridge/agenthub_accounts.go`
- Modify: `internal/bridge/errors.go`, `internal/app/container.go` (grants), `internal/agenthub/account_manage.go` (`NotifyAccountsChanged`)
- Test: `internal/bridge/agenthub_accounts_test.go`, `internal/bridge/errors_agenthub_test.go` (rows), `internal/app/app_test.go`, `internal/agenthub/account_tools_test.go`

- [ ] **Step 1: Write the failing tests.**

`internal/agenthub/account_tools_test.go` (internal package; uses the helper `mcpToolNames` from `devkit_prompt_test.go`):

```go
package agenthub

import (
	"strings"
	"testing"
)

// Agents may read nothing about vendor accounts and change nothing: no MCP tool
// mentions them (the credentials module's accounts.* tools are a different thing),
// and the prompt does not teach one.
func TestNoToolTouchesAgentAccounts(t *testing.T) {
	for name := range mcpToolNames(t) {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "agentaccount") || strings.Contains(lower, "accountpolicy") || strings.Contains(lower, "accountusage") {
			t.Errorf("tool %s touches the vendor accounts; only the window may", name)
		}
	}
	prompt := strings.ToLower(devkitPrompt(claudeNaming, ""))
	for _, phrase := range []string{"account policy", "switch account", "vendor account"} {
		if strings.Contains(prompt, phrase) {
			t.Errorf("the prompt tells agents about vendor accounts (%q)", phrase)
		}
	}
}
```
`internal/app/app_test.go`:

```go
func TestNewContainerGrantsAgentAccounts(t *testing.T) {
	container := NewContainer()
	for _, grant := range []permissions.Grant{
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_account_policy"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_account_policy"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "account:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "account:new"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "account:claude-work"},
	} {
		if !container.Permissions.Check(permissions.Request{SubjectID: grant.SubjectID, Capability: grant.Capability, Scope: grant.Scope}).Allowed {
			t.Errorf("%s %s on %s was denied", grant.SubjectID, grant.Capability, grant.Scope)
		}
	}
}
```
`internal/bridge/errors_agenthub_test.go`: add rows to the table of `TestPublicErrorMapsAgentHubSentinels` (same `{in, want}` shape the table uses):

```go
	{agenthub.ErrAllAccountsLimited, "agent_usage_limit: Every account of this vendor has reached its limit; the first one is available again at 15:04 on 2 Jan"},
```
built from `fmt.Errorf("%w: every account of this vendor has reached its limit; the first one is available again at 15:04 on 2 Jan", agenthub.ErrAllAccountsLimited)`, plus rows for `ErrAccountNotFound` ("validation_failed: That account no longer exists."), `ErrInvalidAccount` wrapped with `"a name is at most 40 characters"` ("validation_failed: Account: a name is at most 40 characters"), `ErrAccountHome` wrapped with a path (the message must NOT contain the path: "validation_failed: DevKit could not prepare a folder for that account. Nothing was changed."), `ErrAccountLabelTaken` ("validation_failed: You already have an account with that name."), `ErrAccountInUse` ("validation_failed: A session is using that account. Close it first."), `ErrUsageNotCheckable` ("validation_failed: This account's usage cannot be read right now."), `ErrAccountsUnavailable` ("validation_failed: Accounts are not available."). And through `publicError("builtin.settings", …)`: `preferences.ErrAccountPolicyConflict` ("validation_failed: The account switching rules changed. Refresh and apply your change again.") and an `ErrInvalidAccountPolicy`-wrapped sentence ("validation_failed: Account switching rules: claude: the threshold must be between 50 and 100 percent").

`internal/bridge/agenthub_accounts_test.go` (package `bridge`; helper below mirrors `newBridgeWithAgentHub` and wires the book — read that helper and `agenthub_subagentlimits_test.go` first and reuse their imports):

```go
func newBridgeWithAccounts(t *testing.T) *AppService {
	t.Helper()
	svc := newBridgeWithAgentHub(t)
	db, err := storage.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	hub := svc.container.AgentHub
	hub.Accounts = agenthub.NewSQLAccountStore(db)
	hub.AccountsDir = t.TempDir()
	hub.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeMock}
	shared := t.TempDir()
	hub.AccountSharedHome = func(agenthub.AgentType) string { return shared }
	return svc
}

func TestAgentAccountsRoundTripThroughTheGateway(t *testing.T) {
	svc := newBridgeWithAccounts(t)
	ctx := context.Background()
	added, err := svc.AgentAccountsAdd(ctx, "mock", "Work")
	if err != nil || added.ID != "mock-work" || added.Builtin || added.SignedIn {
		t.Fatalf("added = %+v, %v", added, err)
	}
	list, err := svc.AgentAccountsList(ctx, "mock")
	if err != nil || len(list) != 2 || !list[0].Builtin || list[1].ID != "mock-work" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if renamed, err := svc.AgentAccountsRename(ctx, "mock-work", "Personal"); err != nil || renamed.Label != "Personal" {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	if shared, err := svc.AgentAccountsSetShared(ctx, "mock-work", true); err != nil || !shared.Shared {
		t.Fatalf("shared = %+v, %v", shared, err)
	}
	if err := svc.AgentAccountsRemove(ctx, "mock-work"); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.AgentAccountsList(ctx, "mock"); len(list) != 1 {
		t.Errorf("after remove: %+v", list)
	}
}

func TestAgentAccountsAreUIOnlyAndNeverPermissionDeniedForTheWindow(t *testing.T) {
	svc := newBridgeWithAccounts(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	win := context.Background()
	if _, err := svc.AgentAccountsAdd(win, "mock", "Work"); err != nil {
		t.Fatal(err)
	}
	calls := map[string]func(ctx context.Context) error{
		"list":          func(c context.Context) error { _, err := svc.AgentAccountsList(c, "mock"); return err },
		"add":           func(c context.Context) error { _, err := svc.AgentAccountsAdd(c, "mock", "Other"); return err },
		"rename":        func(c context.Context) error { _, err := svc.AgentAccountsRename(c, "mock-work", "X"); return err },
		"shared":        func(c context.Context) error { _, err := svc.AgentAccountsSetShared(c, "mock-work", true); return err },
		"remove":        func(c context.Context) error { return svc.AgentAccountsRemove(c, "mock-work") },
		"refreshStatus": func(c context.Context) error { _, err := svc.AgentAccountsRefreshStatus(c, "mock-work"); return err },
		"refreshUsage":  func(c context.Context) error { _, err := svc.AgentAccountsRefreshUsage(c, "mock-work"); return err },
		"policyGet":     func(c context.Context) error { _, err := svc.AgentAccountPolicyGet(c); return err },
		"policySet": func(c context.Context) error {
			_, err := svc.AgentAccountPolicySet(c, AgentAccountPolicyState{}, false)
			return err
		},
	}
	for name, call := range calls {
		if err := call(agent); err == nil {
			t.Errorf("%s: an agent caller must be refused", name)
		}
		assertNotPermissionDenied(t, "window "+name, call(win)) // the real gateway must have the grants (a missing grant is a permission_denied)
	}
}

func TestAgentAccountPolicyChecksTheRevisionAndOwnsTheRiskTimestamp(t *testing.T) {
	svc := newBridgeWithAccounts(t)
	ctx := context.Background()
	state, err := svc.AgentAccountPolicyGet(ctx)
	if err != nil || state.Revision != 0 || state.RiskAcknowledgedAt != "" {
		t.Fatalf("initial = %+v, %v", state, err)
	}
	enable := AgentAccountPolicyState{Revision: 0, Vendors: map[string]AgentAccountVendorPolicyState{"claude": {Enabled: true}}}
	if _, err := svc.AgentAccountPolicySet(ctx, enable, false); err == nil || !strings.Contains(err.Error(), "terms") {
		t.Fatalf("enabling without the acknowledgement: %v, want the terms refusal", err)
	}
	enable.RiskAcknowledgedAt = "1999-01-01T00:00:00Z" // a caller-supplied timestamp is ignored
	saved, err := svc.AgentAccountPolicySet(ctx, enable, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.RiskAcknowledgedAt == "" || saved.RiskAcknowledgedAt == "1999-01-01T00:00:00Z" {
		t.Fatalf("saved = %+v, want the server's own timestamp", saved)
	}
	if v := saved.Vendors["claude"]; v.ThresholdPercent != 90 || v.UsageMaxAgeSeconds != 300 || v.OnLimitRefusal != "none" {
		t.Errorf("defaults not applied: %+v", v)
	}
	enable.Revision = 0 // stale
	if _, err := svc.AgentAccountPolicySet(ctx, enable, true); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a stale revision: %v, want the conflict refusal", err)
	}
	again, err := svc.AgentAccountPolicySet(ctx, AgentAccountPolicyState{Revision: 1, Vendors: saved.Vendors}, false)
	if err != nil || again.RiskAcknowledgedAt != saved.RiskAcknowledgedAt {
		t.Fatalf("a later save keeps the stored acknowledgement: %+v, %v", again, err)
	}
}

func TestAgentAccountsNeedAnAgentHub(t *testing.T) {
	svc := newBridgeWithAccounts(t)
	svc.container.AgentHub = nil
	if _, err := svc.AgentAccountsList(context.Background(), "mock"); err == nil {
		t.Error("without an agent hub the call must fail, not panic")
	}
}
```
(`assertNotPermissionDenied(t, label, err)` is the helper at `agenthub_test.go:81`; match its real signature. Imports: `path/filepath`, `strings`, `devkit/internal/storage`, `devkit/internal/identity`, `devkit/internal/agenthub`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/bridge/ ./internal/app/ ./internal/agenthub/ -run 'AgentAccount|TestNoToolTouchesAgentAccounts|NewContainerGrantsAgentAccounts|PublicErrorMapsAgentHub' -count=1`
Expected: FAIL (does not compile: `AgentAccountsAdd` etc. undefined).

- [ ] **Step 3: Implement.**

Grants, `internal/app/container.go`, after the `agent_subagent_limits` lines:

```go
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_account_policy"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_account_policy"},
```
and next to the other `builtin.agenthub` grants:

```go
		// Vendor accounts (Settings → Agents → Accounts & limits). UI only: no MCP tool
		// uses these scopes and every bridge method refuses a caller that is not the window.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "account:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "account:*"},
```
`account_manage.go` (append): `func (s *Service) NotifyAccountsChanged() { s.accountsChanged() }` (the bridge calls it after a policy save, so every open Accounts page refreshes).
`internal/bridge/agenthub_accounts.go`:

```go
package bridge

import (
	"context"
	"regexp"
	"strings"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
	"devkit/internal/terminal"
)

// AgentAccountInfo is one vendor account as Settings draws it. Times are Unix seconds
// (0 = none). The account's home folder, label-derived scope and every credential stay
// in Go: only what the page shows crosses the bridge.
type AgentAccountInfo struct {
	ID             string                 `json:"id"`
	Vendor         string                 `json:"vendor"`
	Label          string                 `json:"label"`
	Builtin        bool                   `json:"builtin"`
	SignedIn       bool                   `json:"signedIn"`
	Shared         bool                   `json:"shared"`
	ExhaustedUntil int64                  `json:"exhaustedUntil"`
	CooldownUntil  int64                  `json:"cooldownUntil"`
	Usage          []agenthub.LimitWindow `json:"usage"`
	UsageSource    string                 `json:"usageSource"`
	ObservedAt     int64                  `json:"observedAt"`
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func accountInfo(a agenthub.AccountRecord) AgentAccountInfo {
	usage := a.Usage
	if usage == nil {
		usage = []agenthub.LimitWindow{}
	}
	return AgentAccountInfo{
		ID: a.ID, Vendor: string(a.Vendor), Label: a.Label, Builtin: a.Builtin(), SignedIn: a.SignedIn, Shared: a.Shared,
		ExhaustedUntil: unixOrZero(a.ExhaustedUntil), CooldownUntil: unixOrZero(a.CooldownUntil),
		Usage: usage, UsageSource: a.UsageSource, ObservedAt: unixOrZero(a.ObservedAt),
	}
}

var accountIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// accountScopeID keeps a caller-chosen id out of the scope string and the audit
// metadata unless it is the shape DevKit itself generates.
func accountScopeID(id string) string {
	if accountIDPattern.MatchString(id) {
		return id
	}
	return invalidOpaqueID
}

// agentAccountsCall is the shared shape: gateway call as the agent hub subject, window
// only, the agent hub service. Audit metadata carries ids and booleans, never a label,
// a path, a usage number or an error text.
func (s *AppService) agentAccountsCall(ctx context.Context, capability, scope string, meta map[string]any, fn func(context.Context, *agenthub.Service) (any, error)) (any, error) {
	return s.call(ctx, "builtin.agenthub", capability, scope, meta, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return fn(callCtx, svc)
	})
}

func (s *AppService) oneAccount(ctx context.Context, capability, id string, meta map[string]any, fn func(context.Context, *agenthub.Service, string) (agenthub.AccountRecord, error)) (AgentAccountInfo, error) {
	meta["account"] = accountScopeID(id)
	result, err := s.agentAccountsCall(ctx, capability, "account:"+accountScopeID(id), meta, func(c context.Context, svc *agenthub.Service) (any, error) {
		rec, err := fn(c, svc, id)
		if err != nil {
			return nil, err
		}
		return accountInfo(rec), nil
	})
	if err != nil {
		return AgentAccountInfo{}, err
	}
	return result.(AgentAccountInfo), nil
}

// AgentAccountsList lists a vendor's accounts, its built-in one first. UI only.
func (s *AppService) AgentAccountsList(ctx context.Context, vendor string) ([]AgentAccountInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:read", "account:list", map[string]any{"action": "list_agent_accounts", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		list, err := svc.ListAccounts(c, agenthub.AgentType(v))
		if err != nil {
			return nil, err
		}
		out := make([]AgentAccountInfo, 0, len(list))
		for _, a := range list {
			out = append(out, accountInfo(a))
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentAccountInfo), nil
}

// AgentAccountsAdd adds an account (its folder and store link; nothing is signed in yet).
func (s *AppService) AgentAccountsAdd(ctx context.Context, vendor, label string) (AgentAccountInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:new", map[string]any{"action": "add_agent_account", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		rec, err := svc.AddAccount(c, agenthub.AgentType(v), label)
		if err != nil {
			return nil, err
		}
		return accountInfo(rec), nil
	})
	if err != nil {
		return AgentAccountInfo{}, err
	}
	return result.(AgentAccountInfo), nil
}

// AgentAccountsRename renames an added account; the built-in one has a fixed name.
func (s *AppService) AgentAccountsRename(ctx context.Context, id, label string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", id, map[string]any{"action": "rename_agent_account"},
		func(c context.Context, svc *agenthub.Service, id string) (agenthub.AccountRecord, error) {
			return svc.RenameAccount(c, id, label)
		})
}

// AgentAccountsSetShared says whether other people also use the login.
func (s *AppService) AgentAccountsSetShared(ctx context.Context, id string, shared bool) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", id, map[string]any{"action": "set_agent_account_shared", "shared": shared},
		func(c context.Context, svc *agenthub.Service, id string) (agenthub.AccountRecord, error) {
			return svc.SetAccountShared(c, id, shared)
		})
}

// AgentAccountsRemove removes an added account and its folder. Its sessions go back to the built-in account.
func (s *AppService) AgentAccountsRemove(ctx context.Context, id string) error {
	_, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+accountScopeID(id), map[string]any{"action": "remove_agent_account", "account": accountScopeID(id)}, func(c context.Context, svc *agenthub.Service) (any, error) {
		return nil, svc.RemoveAccount(c, id)
	})
	return err
}

// AgentAccountsRefreshStatus re-reads whether the account is signed in.
func (s *AppService) AgentAccountsRefreshStatus(ctx context.Context, id string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", id, map[string]any{"action": "refresh_agent_account_status"},
		func(c context.Context, svc *agenthub.Service, id string) (agenthub.AccountRecord, error) {
			return svc.RefreshAccountStatus(c, id)
		})
}

// AgentAccountsRefreshUsage asks the vendor what the account has left. It starts a
// short-lived vendor process, so it is rate-limited per account.
func (s *AppService) AgentAccountsRefreshUsage(ctx context.Context, id string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", id, map[string]any{"action": "refresh_agent_account_usage"},
		func(c context.Context, svc *agenthub.Service, id string) (agenthub.AccountRecord, error) {
			return svc.RefreshAccountUsage(c, id)
		})
}

// AgentAccountsStartSignIn starts the vendor's terminal sign-in (Claude) for an added
// account in a PTY and returns the terminal session id. Mirrors AgentHubStartSignIn;
// when the terminal exits the account's status is re-read.
func (s *AppService) AgentAccountsStartSignIn(ctx context.Context, id, methodID string, rows, cols int) (string, error) {
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+accountScopeID(id), map[string]any{"action": "start_agent_account_sign_in", "account": accountScopeID(id)}, func(c context.Context, svc *agenthub.Service) (any, error) {
		cmd, err := svc.AccountSignInCommand(c, id, methodID)
		if err != nil {
			return nil, err
		}
		// Same as AgentHubStartSignIn (internal/bridge/agenthub.go): the working
		// directory, the PTY, the exit goroutine that releases the lease, the terminal
		// registration. Read that method and copy its body for these four steps.
		home := signInWorkDir()
		pty, err := terminal.NewLocalCommandPTY(cmd.Path, cmd.Args, cmd.Env, home, rows, cols)
		if err != nil {
			cmd.Release()
			return nil, err
		}
		go func() {
			_ = pty.Wait()
			cmd.Release()
			svc.AfterAccountSignIn(id)
		}()
		return s.container.Terminal.Register("builtin.agenthub", pty), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// AgentAccountsStartBrowserSignIn / StartDeviceSignIn start the in-app sign-in (Codex)
// at an added account's home. The flow ends with agenthub:signInCompleted, which now
// carries the account id; cancel it with AgentHubCancelSignIn.
func (s *AppService) AgentAccountsStartBrowserSignIn(ctx context.Context, id string) (agenthub.SignInFlow, error) {
	return s.startAccountFlow(ctx, id, "browser")
}

func (s *AppService) AgentAccountsStartDeviceSignIn(ctx context.Context, id string) (agenthub.SignInFlow, error) {
	return s.startAccountFlow(ctx, id, "deviceCode")
}

func (s *AppService) startAccountFlow(ctx context.Context, id, method string) (agenthub.SignInFlow, error) {
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+accountScopeID(id), map[string]any{"action": "start_agent_account_sign_in", "account": accountScopeID(id), "method": method}, func(c context.Context, svc *agenthub.Service) (any, error) {
		return svc.StartAccountSignIn(c, id, method)
	})
	if err != nil {
		return agenthub.SignInFlow{}, err
	}
	return result.(agenthub.SignInFlow), nil
}

// AgentAccountVendorPolicyState and AgentAccountPolicyState are the switching rules as
// Settings edits them (preferences.AgentAccountPolicy, minus the timestamp it owns).
type AgentAccountVendorPolicyState struct {
	Enabled                bool     `json:"enabled"`
	ThresholdPercent       int      `json:"thresholdPercent"`
	WeeklyThresholdPercent int      `json:"weeklyThresholdPercent"`
	UsageMaxAgeSeconds     int      `json:"usageMaxAgeSeconds"`
	Order                  []string `json:"order"`
	OnLimitRefusal         string   `json:"onLimitRefusal"`
}

type AgentAccountPolicyState struct {
	Revision           int64                                    `json:"revision"`
	RiskAcknowledgedAt string                                   `json:"riskAcknowledgedAt"`
	Vendors            map[string]AgentAccountVendorPolicyState `json:"vendors"`
}

func accountPolicyState(p preferences.AgentAccountPolicy) AgentAccountPolicyState {
	out := AgentAccountPolicyState{Revision: p.Revision, RiskAcknowledgedAt: p.RiskAcknowledgedAt, Vendors: make(map[string]AgentAccountVendorPolicyState, len(p.Vendors))}
	for name, v := range p.Vendors {
		order := v.Order
		if order == nil {
			order = []string{}
		}
		out.Vendors[name] = AgentAccountVendorPolicyState{
			Enabled: v.Enabled, ThresholdPercent: v.ThresholdPercent, WeeklyThresholdPercent: v.WeeklyThresholdPercent,
			UsageMaxAgeSeconds: v.UsageMaxAgeSeconds, Order: order, OnLimitRefusal: v.OnLimitRefusal,
		}
	}
	return out
}

// AgentAccountPolicyGet reads the switching rules. UI only.
func (s *AppService) AgentAccountPolicyGet(ctx context.Context) (AgentAccountPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_account_policy", map[string]any{"action": "get_agent_account_policy"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.AgentAccountPolicy()
		if err != nil {
			return nil, err
		}
		return accountPolicyState(value), nil
	})
	if err != nil {
		return AgentAccountPolicyState{}, err
	}
	return result.(AgentAccountPolicyState), nil
}

// AgentAccountPolicySet replaces the rules when state.Revision is the stored one.
// acknowledgeRisk records that the person accepted the terms warning; the timestamp is
// DevKit's own (RiskAcknowledgedAt in state is ignored), and a stored acknowledgement is
// kept. A vendor cannot be enabled without one.
func (s *AppService) AgentAccountPolicySet(ctx context.Context, state AgentAccountPolicyState, acknowledgeRisk bool) (AgentAccountPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_account_policy", map[string]any{"action": "set_agent_account_policy", "vendors": len(state.Vendors), "acknowledge": acknowledgeRisk}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		current, err := s.container.Preferences.AgentAccountPolicy()
		if err != nil {
			return nil, err
		}
		ack := current.RiskAcknowledgedAt
		if acknowledgeRisk && strings.TrimSpace(ack) == "" {
			ack = time.Now().UTC().Format(time.RFC3339)
		}
		next := preferences.AgentAccountPolicy{Revision: state.Revision, RiskAcknowledgedAt: ack, Vendors: map[string]preferences.AccountVendorPolicy{}}
		for name, v := range state.Vendors {
			next.Vendors[name] = preferences.AccountVendorPolicy{
				Enabled: v.Enabled, ThresholdPercent: v.ThresholdPercent, WeeklyThresholdPercent: v.WeeklyThresholdPercent,
				UsageMaxAgeSeconds: v.UsageMaxAgeSeconds, Order: v.Order, OnLimitRefusal: v.OnLimitRefusal,
			}
		}
		saved, err := s.container.Preferences.SetAgentAccountPolicy(next)
		if err != nil {
			return nil, err
		}
		if s.container.AgentHub != nil {
			s.container.AgentHub.InvalidateAccountPolicy() // the next turn reads the new rules
			s.container.AgentHub.NotifyAccountsChanged()
		}
		return accountPolicyState(saved), nil
	})
	if err != nil {
		return AgentAccountPolicyState{}, err
	}
	return result.(AgentAccountPolicyState), nil
}
```
(`signInWorkDir()` stands for whatever `AgentHubStartSignIn` uses as the PTY working directory — read it at `internal/bridge/agenthub.go:54` and mirror its exact calls instead of this helper; `terminal.NewLocalCommandPTY(path, args, env, workDir, rows, cols)` and `s.container.Terminal.Register("builtin.agenthub", pty)` are the signatures that method uses. The `*Service.AccountPolicy` the service reads is `container.Preferences.AgentAccountPolicy`, so a saved policy reaches the next turn once the cache is dropped.)

`errors.go`: in the subject-independent preference block, after `ErrInvalidSubagentLimits`:

```go
	if errors.Is(err, preferences.ErrAccountPolicyConflict) {
		return apperrors.New(codeValidationFailed, "The account switching rules changed. Refresh and apply your change again.", true)
	}
	if errors.Is(err, preferences.ErrInvalidAccountPolicy) {
		// The validator's own sentence names the vendor and the limit; it holds nothing the person did not type.
		return apperrors.New(codeValidationFailed, "Account switching rules: "+strings.TrimPrefix(err.Error(), preferences.ErrInvalidAccountPolicy.Error()+": "), true)
	}
```
and in the `builtin.agenthub` switch, before `ErrProcessFailed` (the home error wraps OS errors with paths: a fixed sentence; the cause stays in the log):

```go
		case errors.Is(err, agenthub.ErrAllAccountsLimited):
			return apperrors.New(codeAgentUsageLimit, capitalised(strings.TrimPrefix(err.Error(), agenthub.ErrAllAccountsLimited.Error()+": ")), true)
		case errors.Is(err, agenthub.ErrAccountNotFound):
			return apperrors.New(codeValidationFailed, "That account no longer exists.", true)
		case errors.Is(err, agenthub.ErrAccountHome):
			return apperrors.New(codeValidationFailed, "DevKit could not prepare a folder for that account. Nothing was changed.", true)
		case errors.Is(err, agenthub.ErrInvalidAccount):
			return apperrors.New(codeValidationFailed, "Account: "+strings.TrimPrefix(err.Error(), agenthub.ErrInvalidAccount.Error()+": "), true)
		case errors.Is(err, agenthub.ErrAccountLabelTaken):
			return apperrors.New(codeValidationFailed, "You already have an account with that name.", true)
		case errors.Is(err, agenthub.ErrAccountInUse):
			return apperrors.New(codeValidationFailed, "A session is using that account. Close it first.", true)
		case errors.Is(err, agenthub.ErrUsageNotCheckable):
			return apperrors.New(codeValidationFailed, "This account's usage cannot be read right now.", true)
		case errors.Is(err, agenthub.ErrAccountsUnavailable):
			return apperrors.New(codeValidationFailed, "Accounts are not available.", true)
```
with `func capitalised(s string) string` (upper-case the first rune). The `ErrAccountHome` case must come BEFORE `ErrInvalidAccount` (both can wrap). `codeAgentUsageLimit` is the code the Codex usage-limit case already uses — reuse the constant the neighbouring case names.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/bridge/ ./internal/app/ ./internal/agenthub/ -run 'AgentAccount|TestNoToolTouchesAgentAccounts|NewContainerGrantsAgentAccounts|PublicErrorMapsAgentHub|TestBoundSurface|TestNoBoundMethod' -race -count=1 && go test ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1 && go vet -tags server ./internal/...`
Expected: PASS, including `TestBoundSurfaceCarriesNoFuncs` and `TestNoBoundMethodAsksTheWindowForATime` (no func or `time.Time` parameter was added).

- [ ] **Step 5: Commit**

```bash
git add internal
git commit -m "bridge: UI-only methods for vendor accounts, their grants and published errors"
```

---

### Task 10: Wire it, say when something changed, regenerate the bindings, update the notes

**Files:**
- Modify: `main.go`, `internal/agenthub/service.go` (`OnAccountSwitched`), `internal/agenthub/account_switch.go` (fire it), `internal/agenthub/account_usage.go` (throttled change event), `AGENTS.md`
- Regenerate: `frontend/bindings/**`
- Test: `internal/agenthub/account_events_test.go` (external, rig)

- [ ] **Step 1: Write the failing test.**

```go
package agenthub_test

import (
	"testing"

	"devkit/internal/preferences"

	"devkit/internal/agenthub"
)

func TestAMoveFiresTheSwitchedHookAndAChangeEvent(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	var switched [][4]string
	changed := 0
	rig.svc.OnAccountSwitched = func(sessionID, from, to, reason string) { switched = append(switched, [4]string{sessionID, from, to, reason}) }
	rig.svc.OnAccountsChanged = func() { changed++ }
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(switched) != 1 || switched[0][0] != sess.ID || switched[0][1] != agenthub.BuiltinAccountID(agenthub.AgentTypeMock) || switched[0][2] != "work" || switched[0][3] == "" {
		t.Fatalf("switched = %v, want one move from the built-in account to work with its reason", switched)
	}
	if changed == 0 {
		t.Error("a move changes what the Accounts page shows: OnAccountsChanged must fire")
	}
}

func TestAnUndoneMoveDoesNotFireTheSwitchedHook(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{refuseHomes: true}) // the added account's home never opens
	rig.fillBuiltin(t, 95)
	fired := 0
	rig.svc.OnAccountSwitched = func(string, string, string, string) { fired++ }
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if fired != 0 {
		t.Errorf("the move was undone: OnAccountSwitched fired %d times", fired)
	}
}

func TestObservedUsageTellsThePageAtMostOnceEveryTwoSeconds(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	for i := 1; i <= 5; i++ {
		rig.svc.ObserveUsageForTest(agenthub.BuiltinAccountID(agenthub.AgentTypeMock), []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: float64(i)}})
	}
	if changed != 1 {
		t.Fatalf("events = %d, want 1 for five quick readings (the page refetches the list on each event)", changed)
	}
}
```
with seams in `export_test.go`: `ObserveUsageForTest(accountID string, w []LimitWindow)` (saves through the same path `observeAccountUsage` uses and fires the throttled event — call the internal function directly) and `rigOptions.refuseHomes` mapped to the vendor hook the failed-reopen tests already use (`vendorHook` refusing opens in an account home; reuse it rather than adding a new mechanism).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'AMoveFires|AnUndoneMove|ObservedUsageTells' -count=1`
Expected: FAIL (`OnAccountSwitched`, `ObserveUsageForTest` undefined).

- [ ] **Step 3: Implement.** `service.go` (after `OnAccountsChanged`): `OnAccountSwitched func(sessionID, from, to, reason string)` with a doc comment ("fires after a session moved to another account and the new process opened; never for an undone move; carries ids and the reason sentence, no credential"), and `accountsEventAt time.Time` (unexported, guarded by `mu`). `account_switch.go`, in `noteAccountSwitch` (the place that writes the success notice and the hold entry): after the notice, `if s.OnAccountSwitched != nil { s.OnAccountSwitched(sess.ID, from, target, reason) }` and `s.accountsChanged()`. `account_manage.go`: add

```go
// accountsChangedThrottled is accountsChanged for a stream of small changes (observed
// usage): at most one event every two seconds.
func (s *Service) accountsChangedThrottled() {
	now := time.Now()
	s.mu.Lock()
	if now.Sub(s.accountsEventAt) < 2*time.Second {
		s.mu.Unlock()
		return
	}
	s.accountsEventAt = now
	s.mu.Unlock()
	s.accountsChanged()
}
```
and call it in `observeAccountUsage` after a successful `SaveUsage`. `main.go` (next to `agentSvc.AccountSharedHome = …`):

```go
	// Added accounts live under <data>/agent-accounts/<vendor>/<id> (hidden from every
	// session except the one that runs as the account: protect.go dataDirSecrets). Only
	// Claude and Codex have them; the window learns of any change from one payload-free
	// event and of a move from a second one that carries only ids and the reason.
	agentSvc.AccountsDir = filepath.Join(dataDir, "agent-accounts")
	agentSvc.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeClaude, agenthub.AgentTypeCodex}
	agentSvc.OnAccountsChanged = func() { emitAppEvent("agentaccounts:changed", map[string]any{}) }
	agentSvc.OnAccountSwitched = func(sessionID, from, to, reason string) {
		emitAppEvent("agentaccounts:switched", map[string]any{"sessionId": sessionID, "from": from, "to": to, "reason": reason})
	}
	accountSignInDone = agentSvc.AfterAccountSignIn
```
and where the Codex adapter options are built, before `codexOpts`: `var accountSignInDone func(accountID string)` and change `OnSignInCompleted` to:

```go
		OnSignInCompleted: func(r agenthub.SignInResult) {
			if r.Success && r.AccountID == "" {
				// The built-in sign-in is what the Accounts list notes ("Codex (ChatGPT)");
				// an added account's state lives in agent_accounts.signed_in instead.
				if err := app.RecordAgentSignIn(container, r.Agent); err != nil {
					log.Printf("agenthub: sign-in: accounts note: %v", err)
				}
			}
			if r.AccountID != "" && accountSignInDone != nil {
				go accountSignInDone(r.AccountID)
			}
			emitAppEvent("agenthub:signInCompleted", map[string]any{
				"agent":     string(r.Agent),
				"loginId":   r.LoginID,
				"success":   r.Success,
				"error":     r.Error,
				"accountId": r.AccountID,
			})
		},
```
(keep the surrounding lines of the existing closure as they are.) Then regenerate the bindings: `wails3 generate bindings -clean=true -ts -i` from the repo root (about 15 s; `/home/ptvu3/go/bin/wails3` v3.0.0-beta.32 matches `go.mod`), confirm `frontend/bindings/devkit/internal/bridge/appservice.ts` now has `AgentAccountsList`, `AgentAccountsAdd`, … and `AgentAccountPolicyGet|Set`, and that `models.ts` has `AgentAccountInfo`, `AgentAccountPolicyState`, `SignInResult.accountId` and `SessionUpdate.accountNotice`. `AGENTS.md`: in the "A vendor can have several accounts…" bullet move what this plan built out of "Not done" and "Before the feature is turned on" (account removal, sign-in into an account home, `UsageProbe`, bridge + grants + `bridge.keptCodes`/`publicError` for the account errors, `refreshIfStale` = `refreshStaleForTurn`, `Put` home validation = `accountHomeFor`, the cooldown split, overage, auth-required → signed out, the policy cache, `observed_at`) and describe the new surface in the file's style: the UI-only methods and their scopes (`account:*`), the two events (`agentaccounts:changed` payload-free, `agentaccounts:switched` with ids and reason), `claudeCLI` (`--cli`, `--verbose`, `get_usage` 0–100 vs `rate_limit_event` 0–1), the single-flight/60 s/10 s/2-at-once guards and the running-session read, the `AccountVendors` set. Leave under "Not done": the frontend (Plan 3), real-vendor e2e, macOS keychain isolation, the progress race at `kickPeers`, `continue` recorded as an ordinary prompt.

- [ ] **Step 4: Run everything**

Run:
```bash
gofmt -l internal main.go
go vet -tags server ./internal/... ./cmd/... .
go test ./internal/agenthub/ ./internal/preferences/ ./internal/storage/ ./internal/bridge/ ./internal/app/ ./internal/codexapp/ ./internal/arch/ ./internal/mcp/ -count=1
go test ./internal/agenthub/ -race -count=1
```
Expected: no `gofmt` output (only the known `internal/crypto/keyring.go` may appear), `vet` clean, every package `ok`. The root package needs `frontend/dist` for `go vet -tags server .`; if it is missing create a stub `frontend/dist/index.html` locally, never commit it, delete it after.

- [ ] **Step 5: Commit**

```bash
git add main.go internal AGENTS.md frontend/bindings
git commit -m "main: wire vendor accounts, tell the window when they change, regenerate the bindings"
```

---

## Self-review (against the spec and the reviews)

**Spec coverage.** Accounts lifecycle and home rules (Task 3), on-demand usage with the measured methods and guards — single flight per account, 60 s / 10 s floors, 2 at once, running-session read, pre-turn check for shared accounts with a 5 s box (Tasks 5, 6), per-account sign-in for both vendors with `signed_in` kept by the vendor's own status (Task 7), the notices typed for the window (Task 8), the UI-only bridge with `account:*` grants, published errors and the audit rules (Task 9), the events and the wiring (Task 10). Plan 1's open items: auth-required → signed out, overage not "reached", observed freshness, policy cache, `Put`/home validation, cooldown ≠ exhausted, skipped accounts named in the reason, removal (Tasks 1–4).
**Not here (Plan 3 / later):** every frontend piece; real-vendor e2e (a different account on the resumed side, macOS keychain naming and `AllowKeychain` for account sessions, a symlinked store inside bwrap, the token-refresh race, whether a team seat's numbers include a teammate's use); the devkit-sourced `continue` prompt; the `kickPeers` progress race; the follow-up whose move failed running on the refused account; Claude's real refusal wording.
**Placeholders.** None intended; two steps say "read `AgentHubStartSignIn` and mirror it" because its exact body was not re-read when this plan was written (Task 9), and the Task 5/7 fakes name helpers (`newScriptedClaude`, `waitSignIn`, the `reply` helper in `runFakeCodex`) that exist in the files named — adapt to their real signatures.
**Type consistency.** `UsageChecker.CheckUsage(ctx, spec) ([]LimitWindow, error)`, `SignInChecker.CheckSignIn(ctx, spec) (SignInState, error)`, `RateLimitReader.ReadRateLimits(ctx)`, `AccountAuthProvider.SignInCommandFor(ctx, methodID, home)`, `AccountInAppSignIn.StartBrowserSignInAt/StartDeviceSignInAt(ctx, spec, accountID)`, `SignInResult.AccountID`, `AccountBook` gains `Delete/SetSignedIn/ReplaceUsage/MarkCooldown`, `AccountRecord.CooldownUntil`, `Service.AccountsDir/AccountVendors/OnAccountsChanged/OnAccountSwitched`, `Service.ListAccounts/AddAccount/RenameAccount/SetAccountShared/RemoveAccount/RefreshAccountUsage/RefreshAccountStatus/AccountSignInCommand/StartAccountSignIn/AfterAccountSignIn/InvalidateAccountPolicy/NotifyAccountsChanged` are used with the same names in the bridge, the tests and `main.go`.
**Risks while executing.** (1) The code was written against code that was read, not compiled. (2) Task 5 changes `RateLimitWindow.UsedPercent` to `float64`; `go vet` finds every use. (3) The rig additions (`probe`, `signIn`, `refuseHomes`, `rigOptions`) extend `newAccountRigWith`; keep `newAccountRig`'s behaviour unchanged for the Plan 1 tests. (4) `TwoRefreshesOfOneAccountAreOneCheck` uses a 50 ms pause to let the second caller reach the flight; if it flakes, replace it with a hook that signals when a caller starts waiting (`waitHook`-style).
