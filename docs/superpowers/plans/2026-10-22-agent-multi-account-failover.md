# Agent multi-account failover — backend core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A session moves to the next eligible vendor account at the start of a turn when its account is over a configured percentage or was refused for a real limit, with the same native conversation, using the vendor's own CLI under a per-account home.

**Architecture:** Per-account home directories (`CODEX_HOME` / `CLAUDE_CONFIG_DIR`) with the conversation store shared by symlink. A pure `PickAccount` decides; `Service.invoke` applies the decision before the runtime is resolved. Usage is observed from updates sessions already receive. DevKit never reads a token.

**Tech Stack:** Go 1.25, SQLite (`modernc.org/sqlite`), existing `internal/agenthub`, `internal/preferences`, `internal/storage`.

**Spec:** `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`

**Scope of this plan (Plan 1 of 2).** Phase 0 spike, then the Go backend: migration, store, policy preference, `PickAccount`, home preparation, jail/env per account, switch in `invoke`, usage observation, real-limit marking and the `onLimitRefusal` modes, wiring in `main.go`. **Plan 2 (written after the spike, because the spike picks the Claude usage-check method and may change the continuity approach):** `UsageProbe` (Codex `account/rateLimits/read`; Claude: the CLI's `get_usage` control request, measured in Phase 0), bridge methods + grants + published error codes, the "Accounts & limits" Settings page, transcript notice drawing, summary-card row, toasts, sign-in with a per-account home, account removal, the `devkit`-sourced "continue" prompt.

**Conventions to follow (from `AGENTS.md`):** Go tests `go test ./internal/agenthub/ ./internal/preferences/ ./internal/storage/ ./internal/arch/ -count=1`; vet `go vet -tags server ./...`; `gofmt -l internal main.go`. Keep `internal/arch` green. Commit after each task.

---

## File structure

| File | Action | Responsibility |
| --- | --- | --- |
| `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md` | modify | amend 3 points; add "Measured" |
| `internal/storage/sqlite.go` | modify | migration 45 |
| `internal/storage/migrations_test.go` | modify | version pin 44→45, new test |
| `internal/agenthub/types.go` | modify | `AgentSession.AccountID` |
| `internal/agenthub/store.go` | modify | column, scan, insert, `UpdateSessionAccount` |
| `internal/agenthub/accounts.go` | create | `AccountRecord`, `AccountBook`, errors, id helpers, window merge |
| `internal/agenthub/accounts_store.go` | create | `SQLAccountStore` |
| `internal/preferences/accountpolicy.go` | create | `AgentAccountPolicy`, normalize, service get/set |
| `internal/preferences/preferences.go`, `sqlrepository.go`, `memory.go` | modify | repository methods |
| `internal/agenthub/accountpick.go` | create | pure `PickAccount` |
| `internal/agenthub/account_home.go` | create | `PrepareAccountHome`, `SharedStorePaths`, `carveOutAll` |
| `internal/agenthub/adapter.go` | modify | `OpenSpec.AccountHome`, `AccountSharedPaths` |
| `internal/agenthub/adapter_codex.go` | modify | per-account home in `jail`/`open`/`start` |
| `internal/agenthub/adapter_claude.go` | modify | `CLAUDE_CONFIG_DIR` + jail in `sandboxFor` |
| `internal/agenthub/claude_limits.go` | create | Claude `unifiedWindows` → percent windows |
| `internal/agenthub/account_switch.go` | create | Service side: apply, switch, usage, limit marking, follow-up |
| `internal/agenthub/service.go`, `rewind.go`, `stop_service.go` | modify | hook points |
| `main.go` | modify | wire the book, policy, shared home |
| `AGENTS.md` | modify | document the backend |

---

### Task 0: Amend the spec for what reading the code showed

**Files:** Modify `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`

- [ ] **Step 1: Homes live beside, not inside, the built-in home.** In "Isolation and continuity", replace `<data>/agent-homes/<vendor>/accounts/<id>` (also in the Model table's `home_dir` row) with `<data>/agent-accounts/<vendor>/<id>`. Add the reason: Codex's built-in home is `<data>/agent-homes/codex`; a home inside it would be readable by every built-in Codex session (that session's deny list is carved to its own home), exposing the other accounts' `auth.json`.
- [ ] **Step 2: Stale readings are a lower bound.** In "Where it runs" replace "`PickAccount` treats a stale reading as unknown, i.e. eligible" with: "`PickAccount` uses the reading as it is. A window's usage only grows until its `resetsAt`, so an old reading above the threshold is still true; an old reading below it proves nothing and the account stays eligible (the real-limit backstop covers it)."
- [ ] **Step 3: Types and store live in `agenthub`.** Replace "Accounts (`internal/agentaccounts`, a leaf; migration 45)" with "Accounts (`internal/agenthub/accounts*.go`, migration 45)". Reason: it follows `SQLStore`, and `agenthub` already imports `preferences`; a separate package would have to import `agenthub.LimitWindow`.
- [ ] **Step 4: Account chosen at the first turn, not at creation.** Replace "Sessions created by an agent choose by the same rule at creation; they do not inherit the parent's account." with "A new session starts on the built-in account and, like every session, is evaluated at the start of each turn — so its first turn chooses; it does not inherit its parent's account."
- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md
git commit -m "docs: agent accounts spec — homes beside the built-in one, stale readings, store in agenthub"
```

---

### Task 1: Phase 0 spike (manual, gates everything below)

**Files:** Modify the spec (add a "## Measured" section at the end). No code.

Needs real `claude` and `codex` CLIs, two accounts of each (a team seat counts), a scratch directory. Work only in `$SPIKE`; never point at your real `~/.claude` or `~/.codex`.

- [ ] **Step 1: Set up**

```bash
export SPIKE=/tmp/claude-1001/-home-ptvu3-Downloads-dev-kit/69f0c101-9cbd-4b25-b5a3-30e052209139/scratchpad/spike
mkdir -p "$SPIKE"/{claude-a,claude-b,codex-a,codex-b,proj}
```

- [ ] **Step 2: Claude — sign in two homes**

```bash
CLAUDE_CONFIG_DIR=$SPIKE/claude-a claude auth login   # no `auth` subcommand in your version? run `claude`, then /login
CLAUDE_CONFIG_DIR=$SPIKE/claude-b claude auth login
CLAUDE_CONFIG_DIR=$SPIKE/claude-a claude auth status
CLAUDE_CONFIG_DIR=$SPIKE/claude-b claude auth status
```
Record: do both stay signed in at once? (macOS: `security dump-keychain | grep "Claude Code-credentials"` — record the service names; do they differ per config dir?)

- [ ] **Step 3: Claude — resume under another account with a shared `projects/`**

```bash
cd "$SPIKE/proj"
CLAUDE_CONFIG_DIR=$SPIKE/claude-a claude -p "Remember the word pineapple. Reply: ok" --output-format json > "$SPIKE/a.json"
SID=$(jq -r .session_id "$SPIKE/a.json")
rm -rf "$SPIKE/claude-b/projects"; ln -s "$SPIKE/claude-a/projects" "$SPIKE/claude-b/projects"
CLAUDE_CONFIG_DIR=$SPIKE/claude-b claude -p "What word did I ask you to remember?" --resume "$SID" --output-format json | jq -r .result
```
Expected: the answer names "pineapple". Record: pass/fail, the message on failure, whether the new turn wrote its lines into the shared `projects/` file, and the cache-miss cost (`usage.cache_read_input_tokens` of that call vs a call on account A).
This is the CLI's proxy for `session/load`. Plan 2's e2e repeats it through `claude-agent-acp`; if the CLI passes and ACP fails, stop and report.

- [ ] **Step 4: Claude — usage-check candidates (record which work, with what numbers)**

```bash
# (3) a one-token probe turn: is there a rate_limit_event, and is it account-wide?
cd "$SPIKE/proj"
CLAUDE_CONFIG_DIR=$SPIKE/claude-a claude -p "ok" --output-format stream-json --verbose | jq -c 'select(.type=="rate_limit_event" or (.type=="system" and (tostring|test("rate"))))'
# (1) status-line payload: does the interactive CLI hand rate limits to the status line command?
cat > "$SPIKE/claude-a/settings.json" <<'EOF'
{"statusLine":{"type":"command","command":"cat > /tmp/claude-1001/-home-ptvu3-Downloads-dev-kit/69f0c101-9cbd-4b25-b5a3-30e052209139/scratchpad/spike/statusline.json"}}
EOF
CLAUDE_CONFIG_DIR=$SPIKE/claude-a claude     # send "hi", exit; then:
jq . "$SPIKE/statusline.json"
# (2) /usage: run `claude` interactively, type /usage, copy the screen text into the spec.
```
Record per candidate: works / not, fields returned, whether the numbers include other people's use (compare with the same account's Usage page in the Claude web app, or with a teammate's concurrent use), latency, usage cost, and whether DevKit would have to read a token (if yes: out).

- [ ] **Step 5: Codex — two homes, shared `sessions/`**

```bash
CODEX_HOME=$SPIKE/codex-a codex login
CODEX_HOME=$SPIKE/codex-b codex login
cd "$SPIKE/proj"
CODEX_HOME=$SPIKE/codex-a codex exec --skip-git-repo-check "Remember the word pineapple. Reply: ok"
ls -la "$SPIKE/codex-a"          # note every state file: sessions/, *.sqlite, session_index*, history*
rm -rf "$SPIKE/codex-b/sessions"; ln -s "$SPIKE/codex-a/sessions" "$SPIKE/codex-b/sessions"
CODEX_HOME=$SPIKE/codex-b codex exec resume --last --skip-git-repo-check "What word did I ask you to remember?"
```
Expected: pineapple. If it fails, symlink the other state files one at a time and record the smallest set that works. Record whether both homes accept different ChatGPT accounts at the same time.

- [ ] **Step 6: Codex — `account/rateLimits/read` and the `auth.json` race**

```bash
codex generate-ts --out "$SPIKE/ts" && grep -rln "rateLimits" "$SPIKE/ts" | head
printf '%s\n' \
 '{"id":1,"method":"initialize","params":{"clientInfo":{"name":"spike","version":"0"}}}' \
 '{"method":"initialized"}' \
 '{"id":2,"method":"account/rateLimits/read"}' \
 | CODEX_HOME=$SPIKE/codex-a timeout 20 codex app-server | tee "$SPIKE/rl.json"
```
(If the server rejects the line shape, add `"jsonrpc":"2.0"` to each.) Record: the response fields, whether per-name buckets are present, latency, and — on a team seat — whether the numbers move when a teammate uses the same login. Race: start a long `codex exec` on home A in the background, run the read above on home A at the same time, and compare `stat -c %Y $SPIKE/codex-a/auth.json` before and after; record any failure of either process.

- [ ] **Step 7: Write the results into the spec** under `## Measured`: one short paragraph per Phase 0 question (1–4), the exact commands that worked, and a decision line: `Shared store: works | needs <files> | fails` and `Claude usage check: <candidate> | none`.

- [ ] **Step 8: Gate.** If Claude or Codex resume fails with every needed file shared, **stop**: tell the person, and revise the spec to seed a fresh native conversation instead (do not continue with Task 2). Otherwise record any extra files to share in Task 6's table `sharedStoreEntries`, then commit.

```bash
git add docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md
git commit -m "docs: agent accounts spec — Phase 0 measurements"
```

---

### Task 2: Migration 45 — `agent_accounts` and `agent_sessions.agent_account_id`

**Files:**
- Modify: `internal/storage/sqlite.go` (const block ~line 30; after `m44` ~line 1407; the runner list)
- Modify: `internal/storage/migrations_test.go:184-185`

- [ ] **Step 1: Write the failing test.** In `internal/storage/migrations_test.go` change the pin and add a test:

```go
	if version != 45 {
		t.Fatalf("migration version = %d, want 45", version)
	}
```
```go
func TestMigration45AddsAgentAccounts(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ table, column string }{
		{TableAgentAccounts, "home_dir"},
		{TableAgentAccounts, "shared"},
		{TableAgentAccounts, "exhausted_until"},
		{TableAgentAccounts, "usage"},
		{TableAgentSessions, "agent_account_id"},
	} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s.%s missing after migration 45", c.table, c.column)
		}
	}
}
```
(`filepath` and `context` are already imported in that file; if not, add them.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/storage/ -run 'TestMigration45|TestMigrate' -count=1`
Expected: FAIL (`undefined: TableAgentAccounts`).

- [ ] **Step 3: Implement.** Add `TableAgentAccounts = "agent_accounts"` next to `TableAgentSessions` in the const block, then after `m44`:

```go
	// m45: the vendor accounts a person signed in to Agent Hub (agenthub.AccountRecord).
	// Local, never synced, no credential: a vendor CLI keeps its own login in home_dir.
	// agent_sessions.agent_account_id is the account a session's process runs as
	// ('' = the built-in account).
	m45 := Migration{
		Version: 45,
		Name:    "agent accounts",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					vendor TEXT NOT NULL,
					label TEXT NOT NULL,
					home_dir TEXT NOT NULL DEFAULT '',
					signed_in INTEGER NOT NULL DEFAULT 0,
					shared INTEGER NOT NULL DEFAULT 0,
					exhausted_until INTEGER NOT NULL DEFAULT 0,
					usage TEXT NOT NULL DEFAULT '',
					usage_source TEXT NOT NULL DEFAULT '',
					observed_at INTEGER NOT NULL DEFAULT 0,
					created_at TEXT NOT NULL
				)`, TableAgentAccounts,
			)); err != nil {
				return err
			}
			return addColumnIfNotExists(ctx, db, TableAgentSessions, "agent_account_id", "TEXT NOT NULL DEFAULT ''")
		},
	}
```
and append `, m45` to the `[]Migration{ … m44}` list.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/storage/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite.go internal/storage/migrations_test.go
git commit -m "storage: migration 45 — agent_accounts and agent_sessions.agent_account_id"
```

---

### Task 3: `AgentSession.AccountID` and `UpdateSessionAccount`

**Files:**
- Modify: `internal/agenthub/types.go` (struct `AgentSession`, after `Stop`)
- Modify: `internal/agenthub/store.go` (`Store` interface, `sessionColumns`, `scanSession`, `InsertSession`, new method)
- Test: `internal/agenthub/store_test.go`

- [ ] **Step 1: Write the failing test** (append to `store_test.go`):

```go
func TestStoreSessionAccountRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	s := sampleSession("sess-acct")
	s.AccountID = "codex-work"
	if err := store.InsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "codex-work" {
		t.Fatalf("AccountID = %q, want codex-work", got.AccountID)
	}
	at := s.UpdatedAt.Add(time.Minute)
	if err := store.UpdateSessionAccount(ctx, s.ID, "codex-home", at); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetSession(ctx, s.ID)
	if got.AccountID != "codex-home" {
		t.Fatalf("after update AccountID = %q, want codex-home", got.AccountID)
	}
	if err := store.UpdateSessionAccount(ctx, "missing", "x", at); !errors.Is(err, agenthub.ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want ErrSessionNotFound", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestStoreSessionAccountRoundTrip -count=1`
Expected: FAIL (`AccountID` undefined).

- [ ] **Step 3: Implement.** `types.go`, after the `Stop` field:

```go
	// AccountID is the vendor account this session's process runs as ("" is the
	// built-in account: the machine's own Claude login, DevKit's own Codex home).
	// Set when a turn moves the session to another account (account_switch.go).
	AccountID string `json:"AccountID,omitempty"`
```
`store.go`: add to the interface (after `UpdateSessionStop`):

```go
	// UpdateSessionAccount points a session at another vendor account.
	UpdateSessionAccount(ctx context.Context, id, accountID string, at time.Time) error
```
Change `sessionColumns` to
`const sessionColumns = \`id, agent_type, project_path, workspace_id, status, parent_session_id, depth, title, acp_session_id, extra_rw_paths, preview, stop, agent_account_id, created_at, updated_at\``
In `scanSession` add `accountID` to the `var … string` line (`var agentType, status, extraRW, stopJSON, accountID string`), scan it after `&stopJSON` (`…, &stopJSON, &accountID, &createdAt, &updatedAt`), and set `s.AccountID = accountID` next to `s.AgentType = …`. In `InsertSession` add the column and value:

```go
		INSERT INTO `+tableAgentSessions+` (
			id, agent_type, project_path, workspace_id, status, parent_session_id, depth, title, acp_session_id, extra_rw_paths, agent_account_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, string(s.AgentType), s.ProjectPath, nullableWorkspaceID(s.WorkspaceID), string(s.Status),
		nullString(s.ParentSessionID), s.Depth, nullString(s.Title), s.ACPSessionID,
		encodePathList(s.ExtraReadWritePaths), s.AccountID,
		formatStoreTime(s.CreatedAt), formatStoreTime(s.UpdatedAt),
```
and after `UpdateSessionACPID`:

```go
// UpdateSessionAccount writes the account alone, for the same reason.
func (st *SQLStore) UpdateSessionAccount(ctx context.Context, id, accountID string, at time.Time) error {
	return st.updateSessionColumn(ctx, `agent_account_id = ?`, id, accountID, at)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'Store' -count=1 && go vet ./internal/agenthub/`
Expected: PASS (any other `Store` implementation the compiler flags must get the same method).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/types.go internal/agenthub/store.go internal/agenthub/store_test.go
git commit -m "agenthub: sessions remember the vendor account they run as"
```

---

### Task 4: Account records and the SQL store

**Files:**
- Create: `internal/agenthub/accounts.go`, `internal/agenthub/accounts_store.go`
- Test: `internal/agenthub/accounts_store_test.go`

- [ ] **Step 1: Write the failing test** `accounts_store_test.go`:

```go
package agenthub_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newAccountBook(t *testing.T) *agenthub.SQLAccountStore {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return agenthub.NewSQLAccountStore(db)
}

func TestAccountBookListsTheBuiltinAccountFirstAndSignedIn(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	if err := book.Put(ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	list, err := book.List(ctx, agenthub.AgentTypeCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != agenthub.BuiltinAccountID(agenthub.AgentTypeCodex) || list[1].ID != "work" {
		t.Fatalf("list = %+v, want built-in then work", list)
	}
	if !list[0].Builtin() || !list[0].SignedIn {
		t.Errorf("built-in = %+v, want builtin and signed in", list[0])
	}
	other, _ := book.List(ctx, agenthub.AgentTypeClaude)
	if len(other) != 1 {
		t.Errorf("claude list = %d accounts, want only its built-in", len(other))
	}
}

func TestAccountBookPutGetKeepsUsageAndMarker(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	rec := agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true, Shared: true}
	if err := book.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_800_000_000, 0)
	if err := book.SaveUsage(ctx, "work", []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 40, ResetsAt: 1_800_100_000}}, "observed", at); err != nil {
		t.Fatal(err)
	}
	if err := book.MarkExhausted(ctx, "work", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec.Label = "Work (renamed)"
	if err := book.Put(ctx, rec); err != nil { // an edit must not wipe usage or the marker
		t.Fatal(err)
	}
	got, err := book.Get(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "Work (renamed)" || !got.Shared {
		t.Errorf("got %+v", got)
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 40 || got.UsageSource != "observed" || !got.ObservedAt.Equal(at) {
		t.Errorf("usage lost by Put: %+v", got)
	}
	if !got.ExhaustedUntil.Equal(at.Add(time.Hour)) {
		t.Errorf("marker lost by Put: %v", got.ExhaustedUntil)
	}
}

func TestAccountBookSaveUsageMergesByLabel(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	_ = book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeClaude, Label: "A", HomeDir: "/h/a", SignedIn: true})
	at := time.Unix(1_800_000_000, 0)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 10}}, "observed", at)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 50}}, "observed", at)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 20}}, "observed", at)
	got, _ := book.Get(ctx, "a")
	if len(got.Usage) != 2 {
		t.Fatalf("usage = %+v, want two windows", got.Usage)
	}
	for _, w := range got.Usage {
		if w.Label == "5-hour" && w.UsedPercent != 20 {
			t.Errorf("5-hour = %v, want the newer 20", w.UsedPercent)
		}
	}
}

func TestAccountBookMarkExhaustedOnlyMovesLater(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	_ = book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeClaude, Label: "A", HomeDir: "/h/a", SignedIn: true})
	late, early := time.Unix(1_800_003_600, 0), time.Unix(1_800_000_600, 0)
	_ = book.MarkExhausted(ctx, "a", late)
	_ = book.MarkExhausted(ctx, "a", early)
	got, _ := book.Get(ctx, "a")
	if !got.ExhaustedUntil.Equal(late) {
		t.Errorf("exhausted_until = %v, want %v (it must never move earlier)", got.ExhaustedUntil, late)
	}
	if err := book.MarkExhausted(ctx, "missing", late); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("missing account error = %v, want ErrAccountNotFound", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestAccountBook -count=1`
Expected: FAIL (`undefined: agenthub.SQLAccountStore`).

- [ ] **Step 3: Implement** `accounts.go`:

```go
package agenthub

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrAccountNotFound is an account id the book does not hold.
var ErrAccountNotFound = errors.New("agenthub.accountNotFound")

// AccountRecord is one vendor account a person can run sessions as. It holds no
// credential: the vendor CLI keeps its login in HomeDir, and the built-in
// account (HomeDir "") is the machine's own Claude login or DevKit's own Codex
// home, exactly what sessions ran as before accounts existed.
type AccountRecord struct {
	ID       string
	Vendor   AgentType
	Label    string
	HomeDir  string
	SignedIn bool
	// Shared says other people also use this login (a team account), so what
	// this app's sessions observed is only a lower bound of its usage.
	Shared bool
	// ExhaustedUntil is when a real limit refusal says the account is usable
	// again; zero (or past) means not marked.
	ExhaustedUntil time.Time
	Usage          []LimitWindow
	// UsageSource is "observed" (from a session's own updates) or "checked"
	// (asked of the vendor). Empty when there is no reading.
	UsageSource string
	ObservedAt  time.Time
}

// Builtin reports whether this is the vendor's built-in account.
func (a AccountRecord) Builtin() bool { return a.HomeDir == "" }

// BuiltinAccountID is the id of a vendor's built-in account.
func BuiltinAccountID(vendor AgentType) string { return string(vendor) + "-default" }

func builtinAccountLabel(vendor AgentType) string {
	if vendor == AgentTypeClaude {
		return "System default"
	}
	return "DevKit default"
}

// AccountBook is where accounts and what is known of their usage are kept. nil
// on Service means the feature is off and every session runs as before.
type AccountBook interface {
	// List returns the vendor's accounts, its built-in one first, creating that
	// one on first use.
	List(ctx context.Context, vendor AgentType) ([]AccountRecord, error)
	Get(ctx context.Context, id string) (AccountRecord, error)
	// Put adds an account or edits its label, home, sign-in state and shared
	// flag. It never touches usage or the exhausted marker.
	Put(ctx context.Context, a AccountRecord) error
	// SaveUsage merges windows into the stored ones by label.
	SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error
	// MarkExhausted moves the marker later, never earlier.
	MarkExhausted(ctx context.Context, id string, until time.Time) error
}

// mergeWindows returns old with each of fresh replacing the window of the same
// label, in old's order, new labels last.
func mergeWindows(old, fresh []LimitWindow) []LimitWindow {
	out := append([]LimitWindow(nil), old...)
	for _, w := range fresh {
		replaced := false
		for i := range out {
			if out[i].Label == w.Label {
				out[i] = w
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
		}
	}
	return out
}

// isWeeklyLabel reports whether a window is one of the weekly ones ("Weekly",
// "Weekly · Opus", "7-day").
func isWeeklyLabel(label string) bool {
	l := strings.ToLower(label)
	return strings.Contains(l, "week") || strings.HasPrefix(l, "7-day")
}
```
`accounts_store.go`:

```go
package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const tableAgentAccounts = "agent_accounts"

// SQLAccountStore implements AccountBook over the agent_accounts table
// (storage migration 45).
type SQLAccountStore struct{ db *sql.DB }

func NewSQLAccountStore(db *sql.DB) *SQLAccountStore { return &SQLAccountStore{db: db} }

var _ AccountBook = (*SQLAccountStore)(nil)

const accountColumns = `id, vendor, label, home_dir, signed_in, shared, exhausted_until, usage, usage_source, observed_at`

func scanAccount(scan func(dest ...any) error) (AccountRecord, error) {
	var a AccountRecord
	var vendor, usage string
	var signedIn, shared int
	var exhausted, observed int64
	if err := scan(&a.ID, &vendor, &a.Label, &a.HomeDir, &signedIn, &shared, &exhausted, &usage, &a.UsageSource, &observed); err != nil {
		return AccountRecord{}, err
	}
	a.Vendor = AgentType(vendor)
	a.SignedIn, a.Shared = signedIn != 0, shared != 0
	if exhausted > 0 {
		a.ExhaustedUntil = time.Unix(exhausted, 0)
	}
	if observed > 0 {
		a.ObservedAt = time.Unix(observed, 0)
	}
	if usage != "" {
		// A reading this build cannot read is no reading, not a failed list.
		_ = json.Unmarshal([]byte(usage), &a.Usage)
	}
	return a, nil
}

func (st *SQLAccountStore) ensureBuiltin(ctx context.Context, vendor AgentType) error {
	_, err := st.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, created_at)
		 VALUES (?, ?, ?, '', 1, ?)`,
		BuiltinAccountID(vendor), string(vendor), builtinAccountLabel(vendor), formatStoreTime(time.Now()))
	return err
}

func (st *SQLAccountStore) List(ctx context.Context, vendor AgentType) ([]AccountRecord, error) {
	if err := st.ensureBuiltin(ctx, vendor); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM `+tableAgentAccounts+` WHERE vendor = ?
		 ORDER BY (home_dir = '') DESC, created_at ASC, id ASC`, string(vendor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountRecord
	for rows.Next() {
		a, err := scanAccount(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (st *SQLAccountStore) Get(ctx context.Context, id string) (AccountRecord, error) {
	row := st.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM `+tableAgentAccounts+` WHERE id = ?`, id)
	a, err := scanAccount(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountRecord{}, ErrAccountNotFound
	}
	return a, err
}

func (st *SQLAccountStore) Put(ctx context.Context, a AccountRecord) error {
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, shared, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET label = excluded.label, home_dir = excluded.home_dir,
		   signed_in = excluded.signed_in, shared = excluded.shared`,
		a.ID, string(a.Vendor), a.Label, a.HomeDir, boolInt(a.SignedIn), boolInt(a.Shared), formatStoreTime(time.Now()))
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (st *SQLAccountStore) SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error {
	cur, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(mergeWindows(cur.Usage, windows))
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET usage = ?, usage_source = ?, observed_at = ? WHERE id = ?`,
		string(raw), source, at.Unix(), id)
	return err
}

func (st *SQLAccountStore) MarkExhausted(ctx context.Context, id string, until time.Time) error {
	if _, err := st.Get(ctx, id); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET exhausted_until = ? WHERE id = ? AND exhausted_until < ?`,
		until.Unix(), id, until.Unix())
	return err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run TestAccountBook -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/accounts.go internal/agenthub/accounts_store.go internal/agenthub/accounts_store_test.go
git commit -m "agenthub: account records and their SQL store"
```

---

### Task 5: The policy preference

**Files:**
- Create: `internal/preferences/accountpolicy.go`
- Modify: `internal/preferences/preferences.go` (Repository interface), `sqlrepository.go` (key + methods), `memory.go` (fields + methods)
- Test: `internal/preferences/accountpolicy_test.go`

- [ ] **Step 1: Write the failing test** (`package preferences`):

```go
package preferences

import (
	"errors"
	"testing"
)

func TestAccountPolicyDefaultsAreFilledIn(t *testing.T) {
	got, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": {}}})
	if err != nil {
		t.Fatal(err)
	}
	v := got.Vendors["claude"]
	if v.ThresholdPercent != 90 || v.UsageMaxAgeSeconds != 300 || v.OnLimitRefusal != RefusalNone {
		t.Errorf("defaults = %+v, want 90 / 300 / none", v)
	}
}

func TestAccountPolicyRefusesWhatCannotBeStored(t *testing.T) {
	cases := map[string]AccountVendorPolicy{
		"threshold too low":  {ThresholdPercent: 10},
		"threshold over 100": {ThresholdPercent: 101},
		"weekly out of range": {ThresholdPercent: 90, WeeklyThresholdPercent: 30},
		"max age too small":  {ThresholdPercent: 90, UsageMaxAgeSeconds: 5},
		"unknown refusal":    {ThresholdPercent: 90, OnLimitRefusal: "always"},
	}
	for name, v := range cases {
		_, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": v}})
		if !errors.Is(err, ErrInvalidAccountPolicy) {
			t.Errorf("%s: error = %v, want ErrInvalidAccountPolicy", name, err)
		}
	}
}

func TestAccountPolicyNeedsTheRiskAcknowledgementToEnable(t *testing.T) {
	_, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": {Enabled: true}}})
	if !errors.Is(err, ErrInvalidAccountPolicy) {
		t.Fatalf("enabling without the acknowledgement: error = %v, want ErrInvalidAccountPolicy", err)
	}
	_, err = NormalizeAccountPolicy(AgentAccountPolicy{RiskAcknowledgedAt: "2026-10-22T10:00:00Z", Vendors: map[string]AccountVendorPolicy{"claude": {Enabled: true}}})
	if err != nil {
		t.Fatalf("enabling with it: %v", err)
	}
}

func TestAccountPolicyOrderIsTrimmedAndDeduplicated(t *testing.T) {
	got, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{
		"codex": {Order: []string{" work ", "work", "", "home"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o := got.Vendors["codex"].Order; len(o) != 2 || o[0] != "work" || o[1] != "home" {
		t.Errorf("order = %v, want [work home]", o)
	}
}

func TestAccountPolicySetUsesRevisionCompareAndSwap(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	first, err := svc.SetAgentAccountPolicy(AgentAccountPolicy{Revision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 {
		t.Fatalf("revision = %d, want 1", first.Revision)
	}
	if _, err := svc.SetAgentAccountPolicy(AgentAccountPolicy{Revision: 0}); !errors.Is(err, ErrAccountPolicyConflict) {
		t.Fatalf("stale revision: error = %v, want ErrAccountPolicyConflict", err)
	}
	got, err := svc.AgentAccountPolicy()
	if err != nil || got.Revision != 1 {
		t.Fatalf("read back = %+v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/preferences/ -run AccountPolicy -count=1`
Expected: FAIL (`undefined: NormalizeAccountPolicy`).

- [ ] **Step 3: Implement** `accountpolicy.go`:

```go
package preferences

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The person's rules for switching between several accounts of one vendor
// (Settings → Agents → Accounts & limits). Local, UI-owned: an agent cannot
// read or change it. It carries no credential and grants nothing — it only
// decides which of the person's own accounts a session runs as.

const (
	// RefusalNone leaves a turn the vendor refused for a real limit alone; the
	// next prompt starts on the next account.
	RefusalNone = "none"
	// RefusalResendIfNoProgress sends the same prompt once on the next account
	// when the refused turn had produced no output and no tool call.
	RefusalResendIfNoProgress = "resendIfNoProgress"
	// RefusalContinue sends one fixed "continue" message on the next account.
	RefusalContinue = "continue"

	DefaultAccountThresholdPercent = 90
	DefaultAccountUsageMaxAge      = 300 // seconds
	maxAccountOrder                = 32
)

var (
	ErrInvalidAccountPolicy  = errors.New("preferences: invalid account policy")
	ErrAccountPolicyConflict = errors.New("account policy revision conflict")
)

// AccountVendorPolicy is the policy for one vendor, keyed by its agent type.
type AccountVendorPolicy struct {
	Enabled bool `json:"enabled"`
	// ThresholdPercent: an account at or over this in any window is left (50–100).
	ThresholdPercent int `json:"thresholdPercent"`
	// WeeklyThresholdPercent replaces ThresholdPercent for weekly windows; 0 = same.
	WeeklyThresholdPercent int `json:"weeklyThresholdPercent,omitempty"`
	// UsageMaxAgeSeconds is how old a shared account's reading may be before a
	// turn start asks the vendor again (60–3600).
	UsageMaxAgeSeconds int `json:"usageMaxAgeSeconds"`
	// Order is the person's preference, first used first. Built-in ids may be listed.
	Order          []string `json:"order,omitempty"`
	OnLimitRefusal string   `json:"onLimitRefusal"`
}

// AgentAccountPolicy is the whole preference, versioned like AgentModelProfiles.
type AgentAccountPolicy struct {
	Revision int64 `json:"revision"`
	// RiskAcknowledgedAt is when the person accepted the terms warning; a vendor
	// cannot be enabled without it.
	RiskAcknowledgedAt string                         `json:"riskAcknowledgedAt,omitempty"`
	Vendors            map[string]AccountVendorPolicy `json:"vendors,omitempty"`
}

func (p AgentAccountPolicy) clone() AgentAccountPolicy {
	out := p
	if p.Vendors != nil {
		out.Vendors = make(map[string]AccountVendorPolicy, len(p.Vendors))
		for k, v := range p.Vendors {
			v.Order = append([]string(nil), v.Order...)
			out.Vendors[k] = v
		}
	}
	return out
}

// NormalizeAccountPolicy fills defaults and refuses what cannot be stored.
func NormalizeAccountPolicy(in AgentAccountPolicy) (AgentAccountPolicy, error) {
	out := in.clone()
	for name, v := range out.Vendors {
		if strings.TrimSpace(name) == "" || len(name) > 32 {
			return AgentAccountPolicy{}, fmt.Errorf("%w: a vendor name must be 1–32 characters", ErrInvalidAccountPolicy)
		}
		if v.ThresholdPercent == 0 {
			v.ThresholdPercent = DefaultAccountThresholdPercent
		}
		if v.UsageMaxAgeSeconds == 0 {
			v.UsageMaxAgeSeconds = DefaultAccountUsageMaxAge
		}
		if v.OnLimitRefusal == "" {
			v.OnLimitRefusal = RefusalNone
		}
		switch {
		case v.ThresholdPercent < 50 || v.ThresholdPercent > 100:
			return AgentAccountPolicy{}, fmt.Errorf("%w: the threshold must be between 50 and 100 percent", ErrInvalidAccountPolicy)
		case v.WeeklyThresholdPercent != 0 && (v.WeeklyThresholdPercent < 50 || v.WeeklyThresholdPercent > 100):
			return AgentAccountPolicy{}, fmt.Errorf("%w: the weekly threshold must be between 50 and 100 percent", ErrInvalidAccountPolicy)
		case v.UsageMaxAgeSeconds < 60 || v.UsageMaxAgeSeconds > 3600:
			return AgentAccountPolicy{}, fmt.Errorf("%w: how old a reading may be must be between 60 and 3600 seconds", ErrInvalidAccountPolicy)
		case v.OnLimitRefusal != RefusalNone && v.OnLimitRefusal != RefusalResendIfNoProgress && v.OnLimitRefusal != RefusalContinue:
			return AgentAccountPolicy{}, fmt.Errorf("%w: unknown choice for a turn refused at its limit", ErrInvalidAccountPolicy)
		case v.Enabled && strings.TrimSpace(out.RiskAcknowledgedAt) == "":
			return AgentAccountPolicy{}, fmt.Errorf("%w: accept the terms warning before turning switching on", ErrInvalidAccountPolicy)
		}
		seen := map[string]bool{}
		order := make([]string, 0, len(v.Order))
		for _, id := range v.Order {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			order = append(order, id)
		}
		if len(order) > maxAccountOrder {
			return AgentAccountPolicy{}, fmt.Errorf("%w: at most %d accounts in the order", ErrInvalidAccountPolicy, maxAccountOrder)
		}
		v.Order = order
		out.Vendors[name] = v
	}
	return out, nil
}

var accountPolicyMu sync.Mutex

// AgentAccountPolicy reads the policy. Unset is the zero value: nothing enabled.
func (s *Service) AgentAccountPolicy() (AgentAccountPolicy, error) {
	value, _, err := s.repo.GetAgentAccountPolicy()
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	return value.clone(), nil
}

// SetAgentAccountPolicy replaces the policy when the caller's revision is the
// stored one, and returns what was stored.
func (s *Service) SetAgentAccountPolicy(value AgentAccountPolicy) (AgentAccountPolicy, error) {
	accountPolicyMu.Lock()
	defer accountPolicyMu.Unlock()
	current, _, err := s.repo.GetAgentAccountPolicy()
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	if current.Revision != value.Revision {
		return AgentAccountPolicy{}, ErrAccountPolicyConflict
	}
	next, err := NormalizeAccountPolicy(value)
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	next.Revision = current.Revision + 1
	if err := s.repo.SetAgentAccountPolicy(next); err != nil {
		return AgentAccountPolicy{}, err
	}
	return next.clone(), nil
}
```
`preferences.go` Repository interface, after `SetAgentSubagentLimits`:

```go
	GetAgentAccountPolicy() (AgentAccountPolicy, bool, error)
	SetAgentAccountPolicy(policy AgentAccountPolicy) error
```
`sqlrepository.go`: add `agentAccountPolicyKey = "agent_account_policy"` to the const block and

```go
// GetAgentAccountPolicy reads a value it cannot parse as unset: nothing is
// enabled, which is how every session ran before accounts existed.
func (r *SQLRepository) GetAgentAccountPolicy() (AgentAccountPolicy, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", agentAccountPolicyKey).Scan(&value)
	if err == sql.ErrNoRows {
		return AgentAccountPolicy{}, false, nil
	}
	if err != nil {
		return AgentAccountPolicy{}, false, err
	}
	var policy AgentAccountPolicy
	if jsonErr := json.Unmarshal([]byte(value), &policy); jsonErr != nil {
		return AgentAccountPolicy{}, false, nil
	}
	return policy, true, nil
}

func (r *SQLRepository) SetAgentAccountPolicy(policy AgentAccountPolicy) error {
	body, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, agentAccountPolicyKey, string(body))
	return err
}
```
`memory.go`: add fields `accountPolicy AgentAccountPolicy` / `hasAccountPolicy bool` to the struct and

```go
func (r *MemoryRepository) GetAgentAccountPolicy() (AgentAccountPolicy, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.accountPolicy.clone(), r.hasAccountPolicy, nil
}

func (r *MemoryRepository) SetAgentAccountPolicy(policy AgentAccountPolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accountPolicy = policy.clone()
	r.hasAccountPolicy = true
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/preferences/ -count=1 && go build ./... 2>&1 | head`
Expected: PASS; if the build flags another `Repository` implementer, give it the two methods.

- [ ] **Step 5: Commit**

```bash
git add internal/preferences
git commit -m "preferences: the policy for switching between a vendor's accounts"
```

---

### Task 6: `PickAccount` — the pure decision

**Files:**
- Create: `internal/agenthub/accountpick.go`
- Test: `internal/agenthub/accountpick_test.go`

- [ ] **Step 1: Write the failing test** (`package agenthub_test`):

```go
package agenthub_test

import (
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

var pickNow = time.Unix(1_800_000_000, 0)

func acct(id string, usage ...agenthub.LimitWindow) agenthub.AccountRecord {
	home := "/h/" + id
	if id == "claude-default" {
		home = ""
	}
	return agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeClaude, Label: id, HomeDir: home, SignedIn: true, Usage: usage}
}

func win(label string, used float64) agenthub.LimitWindow {
	return agenthub.LimitWindow{Label: label, UsedPercent: used, ResetsAt: pickNow.Add(6 * time.Hour).Unix()}
}

func policy() preferences.AccountVendorPolicy {
	return preferences.AccountVendorPolicy{Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: preferences.RefusalNone}
}

func TestPickAccount(t *testing.T) {
	exhausted := func(a agenthub.AccountRecord) agenthub.AccountRecord {
		a.ExhaustedUntil = pickNow.Add(time.Hour)
		return a
	}
	signedOut := func(a agenthub.AccountRecord) agenthub.AccountRecord { a.SignedIn = false; return a }
	weekly := policy()
	weekly.WeeklyThresholdPercent = 70

	cases := []struct {
		name       string
		policy     preferences.AccountVendorPolicy
		accounts   []agenthub.AccountRecord
		current    string
		switchedAt time.Time
		previous   string
		want       string // expected Target
		wantSwitch bool
		allLimited bool
	}{
		{"disabled stays", preferences.AccountVendorPolicy{}, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"one account stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99))}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"under threshold stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 89)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"at threshold moves", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 90)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"any window counts", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 10), win("Weekly", 95)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"weekly threshold applies to weekly only", weekly, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 80), win("Weekly", 75)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"weekly threshold leaves the 5-hour window alone", weekly, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 80), win("Weekly", 60)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"elapsed reset counts as reset", policy(), []agenthub.AccountRecord{acct("claude-default", agenthub.LimitWindow{Label: "5-hour", UsedPercent: 99, ResetsAt: pickNow.Add(-time.Minute).Unix()}), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"order decides the target", func() preferences.AccountVendorPolicy { p := policy(); p.Order = []string{"c", "b"}; return p }(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b"), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"unknown usage is eligible", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"target over threshold is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b", win("5-hour", 95)), acct("c", win("5-hour", 10))}, "claude-default", time.Time{}, "", "c", true, false},
		{"signed-out target is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), signedOut(acct("b")), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"exhausted current moves", policy(), []agenthub.AccountRecord{exhausted(acct("claude-default")), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"exhausted target is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), exhausted(acct("b")), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"empty current means the built-in", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "", time.Time{}, "", "b", true, false},
		{"recent switch holds", policy(), []agenthub.AccountRecord{acct("claude-default"), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Minute), "claude-default", "b", false, false},
		{"recent switch does not hold against a real refusal", policy(), []agenthub.AccountRecord{acct("claude-default"), exhausted(acct("b"))}, "b", pickNow.Add(-time.Minute), "claude-default", "claude-default", true, false},
		{"returning needs a margin", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 85)), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Hour), "claude-default", "b", false, false},
		{"returning with a margin is fine", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 70)), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Hour), "claude-default", "claude-default", true, false},
		{"nobody to go to stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b", win("5-hour", 97))}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"every account exhausted", policy(), []agenthub.AccountRecord{exhausted(acct("claude-default")), exhausted(acct("b"))}, "claude-default", time.Time{}, "", "claude-default", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := agenthub.PickAccount(agenthub.PickInput{
				Policy: tc.policy, Accounts: tc.accounts, Current: tc.current,
				Now: pickNow, SwitchedAt: tc.switchedAt, Previous: tc.previous,
			})
			if got.Target != tc.want || got.Switch != tc.wantSwitch || got.AllLimited != tc.allLimited {
				t.Fatalf("PickAccount = %+v, want target %q switch %v allLimited %v", got, tc.want, tc.wantSwitch, tc.allLimited)
			}
			if tc.wantSwitch && got.Reason == "" {
				t.Errorf("a switch must carry its reason")
			}
		})
	}
}

func TestPickAccountAllLimitedNamesTheEarliestReturn(t *testing.T) {
	a, b := acct("claude-default"), acct("b")
	a.ExhaustedUntil = pickNow.Add(3 * time.Hour)
	b.ExhaustedUntil = pickNow.Add(time.Hour)
	got := agenthub.PickAccount(agenthub.PickInput{Policy: policy(), Accounts: []agenthub.AccountRecord{a, b}, Current: "claude-default", Now: pickNow})
	if !got.AllLimited || !got.RetryAt.Equal(pickNow.Add(time.Hour)) {
		t.Fatalf("got %+v, want AllLimited with RetryAt in one hour", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestPickAccount -count=1`
Expected: FAIL (`undefined: agenthub.PickAccount`).

- [ ] **Step 3: Implement** `accountpick.go`:

```go
package agenthub

import (
	"fmt"
	"sort"
	"time"

	"devkit/internal/preferences"
)

const (
	// accountFlipHold is how long a session stays on the account it just moved
	// to, whatever the numbers say (a real refusal still moves it).
	accountFlipHold = 5 * time.Minute
	// accountReturnGap is how far under the threshold an account must be before
	// a session goes back to the one it left.
	accountReturnGap = 10.0
	// accountProbeCooldown is how long a refusal that names no reset keeps an
	// account out, before the next turn may try it again.
	accountProbeCooldown = 15 * time.Minute
)

// PickInput is everything PickAccount decides from. Accounts are all of one
// vendor's; Current is the account the session is bound to ("" is the built-in).
type PickInput struct {
	Policy     preferences.AccountVendorPolicy
	Accounts   []AccountRecord
	Current    string
	Now        time.Time
	SwitchedAt time.Time // when this session last switched; zero = never
	Previous   string    // the account it left then
}

// PickDecision says which account the next turn runs as.
type PickDecision struct {
	Target string
	Switch bool
	// Reason is why the current account was left ("Weekly 92% ≥ 90%"), or why
	// nothing could be chosen.
	Reason string
	// AllLimited: every signed-in account is marked exhausted. RetryAt is when
	// the first comes back.
	AllLimited bool
	RetryAt    time.Time
}

// PickAccount is pure. A reading is used as it is: a window's usage only grows
// until it resets, so an old reading over the threshold is still true, and an old
// one under it proves nothing and leaves the account eligible.
func PickAccount(in PickInput) PickDecision {
	stay := PickDecision{Target: in.Current}
	if !in.Policy.Enabled || len(in.Accounts) < 2 {
		return stay
	}
	ordered := orderedAccounts(in.Policy.Order, in.Accounts)
	cur := findAccount(ordered, in.Current)
	if cur == nil {
		return stay
	}
	stay.Target = cur.ID
	ok, why := accountFine(*cur, in.Policy, in.Now, 0)
	if ok {
		return stay
	}
	refused := cur.ExhaustedUntil.After(in.Now)
	if !refused && !in.SwitchedAt.IsZero() && in.Now.Sub(in.SwitchedAt) < accountFlipHold {
		return stay
	}
	for _, a := range ordered {
		if a.ID == cur.ID || !a.SignedIn {
			continue
		}
		margin := 0.0
		if a.ID == in.Previous {
			margin = accountReturnGap
		}
		if fine, _ := accountFine(a, in.Policy, in.Now, margin); fine {
			return PickDecision{Target: a.ID, Switch: true, Reason: why}
		}
	}
	if retry, all := earliestReturn(ordered, in.Now); all {
		return PickDecision{Target: cur.ID, AllLimited: true, RetryAt: retry, Reason: why}
	}
	stay.Reason = why
	return stay
}

// accountFine reports whether an account may run a turn: not marked exhausted
// and every window not yet reset under its threshold (lowered by margin).
func accountFine(a AccountRecord, p preferences.AccountVendorPolicy, now time.Time, margin float64) (bool, string) {
	if a.ExhaustedUntil.After(now) {
		return false, "usage limit reached"
	}
	threshold := float64(p.ThresholdPercent)
	if threshold <= 0 {
		threshold = preferences.DefaultAccountThresholdPercent
	}
	for _, w := range a.Usage {
		if w.ResetsAt != 0 && w.ResetsAt <= now.Unix() {
			continue
		}
		limit := threshold
		if p.WeeklyThresholdPercent > 0 && isWeeklyLabel(w.Label) {
			limit = float64(p.WeeklyThresholdPercent)
		}
		if w.UsedPercent >= limit-margin {
			return false, fmt.Sprintf("%s %.0f%% ≥ %.0f%%", w.Label, w.UsedPercent, limit)
		}
	}
	return true, ""
}

func orderedAccounts(order []string, accounts []AccountRecord) []AccountRecord {
	rank := make(map[string]int, len(order))
	for i, id := range order {
		rank[id] = i
	}
	out := append([]AccountRecord(nil), accounts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].ID]
		rj, jok := rank[out[j].ID]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return false
	})
	return out
}

// findAccount resolves id; "" is the vendor's built-in account.
func findAccount(accounts []AccountRecord, id string) *AccountRecord {
	for i := range accounts {
		if (id == "" && accounts[i].Builtin()) || accounts[i].ID == id {
			return &accounts[i]
		}
	}
	return nil
}

// earliestReturn reports whether every signed-in account is marked exhausted
// and, if so, when the first comes back.
func earliestReturn(accounts []AccountRecord, now time.Time) (time.Time, bool) {
	var first time.Time
	signedIn := 0
	for _, a := range accounts {
		if !a.SignedIn {
			continue
		}
		signedIn++
		if !a.ExhaustedUntil.After(now) {
			return time.Time{}, false
		}
		if first.IsZero() || a.ExhaustedUntil.Before(first) {
			first = a.ExhaustedUntil
		}
	}
	return first, signedIn > 0
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run TestPickAccount -count=1`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/accountpick.go internal/agenthub/accountpick_test.go
git commit -m "agenthub: PickAccount decides which account the next turn runs as"
```

---

### Task 7: Account homes — preparation, shared store, multi-root carve-out

**Files:**
- Create: `internal/agenthub/account_home.go`
- Test: `internal/agenthub/account_home_test.go` (`package agenthub`)

- [ ] **Step 1: Write the failing test**

```go
package agenthub

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPrepareAccountHomeLinksTheSharedStore(t *testing.T) {
	shared := t.TempDir()
	home := filepath.Join(t.TempDir(), "acct")
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "sessions")
	target, err := os.Readlink(link)
	if err != nil || target != filepath.Join(shared, "sessions") {
		t.Fatalf("sessions link = %q, %v; want %q", target, err, filepath.Join(shared, "sessions"))
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatalf("preparing twice must be a no-op: %v", err)
	}
	if info, _ := os.Stat(home); info.Mode().Perm() != 0o700 {
		t.Errorf("home mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestPrepareAccountHomeNeverReplacesRealData(t *testing.T) {
	shared := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err == nil {
		t.Fatal("a real sessions directory must be refused, not replaced")
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); err != nil {
		t.Errorf("the existing directory was touched: %v", err)
	}
}

func TestPrepareAccountHomeRefusesRelativePaths(t *testing.T) {
	if err := PrepareAccountHome(AgentTypeClaude, "relative/home", t.TempDir()); err == nil {
		t.Fatal("a relative home must be refused")
	}
}

func TestSharedStorePaths(t *testing.T) {
	got := SharedStorePaths(AgentTypeClaude, "/x/.claude")
	if !slices.Equal(got, []string{"/x/.claude/projects"}) {
		t.Errorf("claude shared paths = %v", got)
	}
	if got := SharedStorePaths(AgentTypeMock, "/x"); len(got) != 0 {
		t.Errorf("a vendor with no shared store has none, got %v", got)
	}
}

func TestCarveOutAllKeepsEveryRootReachableAndHidesTheRest(t *testing.T) {
	data := t.TempDir()
	mk := func(p ...string) string {
		path := filepath.Join(append([]string{data}, p...)...)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mk("agent-homes", "codex", "sessions")
	mk("agent-homes", "codex", "auth")
	keepHome := mk("agent-accounts", "codex", "work")
	mk("agent-accounts", "codex", "other")
	keepShared := filepath.Join(data, "agent-homes", "codex", "sessions")
	deny := []string{filepath.Join(data, "agent-homes"), filepath.Join(data, "agent-accounts"), filepath.Join(data, "devkit.db")}

	got := carveOutAll(deny, keepHome, keepShared)

	for _, hidden := range []string{
		filepath.Join(data, "agent-homes", "codex", "auth"),
		filepath.Join(data, "agent-accounts", "codex", "other"),
		filepath.Join(data, "devkit.db"),
	} {
		if !slices.Contains(got, hidden) {
			t.Errorf("%s must stay hidden; deny = %v", hidden, got)
		}
	}
	for _, kept := range []string{keepHome, keepShared, filepath.Join(data, "agent-homes"), filepath.Join(data, "agent-accounts")} {
		if slices.Contains(got, kept) {
			t.Errorf("%s must not be denied (it is, or holds, a kept root); deny = %v", kept, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'PrepareAccountHome|SharedStorePaths|CarveOutAll' -count=1`
Expected: FAIL (`undefined: PrepareAccountHome`).

- [ ] **Step 3: Implement** `account_home.go`:

```go
package agenthub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrAccountHome is a home DevKit cannot prepare without risking data.
var ErrAccountHome = errors.New("agenthub.accountHome")

// sharedStoreEntries are the entries of a vendor's home that hold its
// conversations. An added account links them to the built-in account's, so a
// session moved to another account resumes the same native conversation while
// the sign-in (auth.json, credentials, .claude.json) stays per account.
// Measured in Phase 0 (spec "Measured"); add to this table only what that
// measurement says a resume needs.
var sharedStoreEntries = map[AgentType][]string{
	AgentTypeClaude: {"projects"},
	AgentTypeCodex:  {"sessions"},
}

// SharedStorePaths are the directories of sharedFrom (the built-in account's
// home) an account's links point at: the jail grants them read-write.
func SharedStorePaths(vendor AgentType, sharedFrom string) []string {
	var out []string
	for _, name := range sharedStoreEntries[vendor] {
		out = append(out, filepath.Join(sharedFrom, name))
	}
	return out
}

// PrepareAccountHome creates an account's home (0700) and links its
// conversation store to the shared one. It never removes or replaces anything:
// a real directory where a link belongs is refused.
func PrepareAccountHome(vendor AgentType, home, sharedFrom string) error {
	if home == "" || !filepath.IsAbs(home) || !filepath.IsAbs(sharedFrom) {
		return fmt.Errorf("%w: homes must be absolute paths", ErrAccountHome)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("%w: create %s: %v", ErrAccountHome, home, err)
	}
	for _, name := range sharedStoreEntries[vendor] {
		target := filepath.Join(sharedFrom, name)
		link := filepath.Join(home, name)
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("%w: create %s: %v", ErrAccountHome, target, err)
		}
		info, err := os.Lstat(link)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			if current, _ := os.Readlink(link); current == target {
				continue
			}
			return fmt.Errorf("%w: %s already links somewhere else", ErrAccountHome, link)
		case err == nil:
			return fmt.Errorf("%w: %s exists and is not a link; DevKit will not replace it", ErrAccountHome, link)
		case !os.IsNotExist(err):
			return fmt.Errorf("%w: %v", ErrAccountHome, err)
		}
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("%w: link %s: %v", ErrAccountHome, link, err)
		}
	}
	return nil
}

// carveOutAll is carveOut for several paths at once: every deny path that holds
// one of keeps is replaced by its entries, recursively, except the way down to
// each kept path. Calling carveOut once per root cannot do this — the second
// call would hide the first root's siblings' contents again.
//
// Entries are listed when a session opens; an entry created later beside a kept
// path is not hidden from that session (as with carveOut). An unlistable
// directory stays hidden whole, so the kept path cannot be reached and the spawn
// fails closed.
func carveOutAll(deny []string, keeps ...string) []string {
	var out []string
	for _, path := range deny {
		out = append(out, carveKeeps(path, keeps)...)
	}
	return out
}

func carveKeeps(dir string, keeps []string) []string {
	var inside []string
	for _, keep := range keeps {
		if keep == dir {
			return nil
		}
		if pathIsInside(keep, dir) {
			inside = append(inside, keep)
		}
	}
	if len(inside) == 0 {
		return []string{dir}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{dir}
	}
	var out []string
	for _, entry := range entries {
		out = append(out, carveKeeps(filepath.Join(dir, entry.Name()), inside)...)
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'PrepareAccountHome|SharedStorePaths|CarveOutAll' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/account_home.go internal/agenthub/account_home_test.go
git commit -m "agenthub: account homes with a shared conversation store, and a multi-root carve-out"
```

---

### Task 8: A session's jail and environment follow its account

**Files:**
- Modify: `internal/agenthub/adapter.go` (`OpenSpec`, after `SubagentLimits`)
- Modify: `internal/agenthub/adapter_codex.go` (`jail` ~line 222, `open` ~line 547, `start` ~line 403)
- Modify: `internal/agenthub/adapter_claude.go` (`sandboxFor` ~line 166)
- Test: `internal/agenthub/account_spec_test.go` (`package agenthub`)

- [ ] **Step 1: Write the failing test**

```go
package agenthub

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCodexJailUsesTheAccountHome(t *testing.T) {
	builtin := t.TempDir()
	account := filepath.Join(t.TempDir(), "work")
	shared := filepath.Join(builtin, "sessions")
	if err := PrepareAccountHome(AgentTypeCodex, account, builtin); err != nil {
		t.Fatal(err)
	}
	a := NewCodexAdapter(CodexOptions{Home: builtin})
	spec := OpenSpec{AccountHome: account, AccountSharedPaths: []string{shared}}
	pol, _, err := a.jail(t.TempDir(), launch{}, spec, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Env["CODEX_HOME"] != account {
		t.Errorf("CODEX_HOME = %q, want the account home %q", pol.Env["CODEX_HOME"], account)
	}
	if !slices.Contains(pol.ReadWritePaths, account) || !slices.Contains(pol.ReadWritePaths, shared) {
		t.Errorf("read-write = %v, want the account home and the shared store", pol.ReadWritePaths)
	}
	if slices.Contains(pol.ReadWritePaths, builtin) {
		t.Errorf("read-write = %v must not grant the built-in home itself", pol.ReadWritePaths)
	}
}

func TestCodexJailWithoutAccountIsUnchanged(t *testing.T) {
	builtin := t.TempDir()
	a := NewCodexAdapter(CodexOptions{Home: builtin})
	pol, _, err := a.jail(t.TempDir(), launch{}, OpenSpec{}, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Env["CODEX_HOME"] != builtin || !slices.Contains(pol.ReadWritePaths, builtin) {
		t.Errorf("built-in jail changed: CODEX_HOME=%q rw=%v", pol.Env["CODEX_HOME"], pol.ReadWritePaths)
	}
}

func TestClaudeSandboxUsesTheAccountConfigDir(t *testing.T) {
	account := filepath.Join(t.TempDir(), "work")
	shared := filepath.Join(t.TempDir(), "projects")
	spec := OpenSpec{AccountHome: account, AccountSharedPaths: []string{shared}}
	pol, _ := sandboxFor(t.TempDir(), launch{path: "/bin/true"}, spec)
	if pol.Env["CLAUDE_CONFIG_DIR"] != account {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", pol.Env["CLAUDE_CONFIG_DIR"], account)
	}
	if !slices.Contains(pol.ReadWritePaths, account) || !slices.Contains(pol.ReadWritePaths, shared) {
		t.Errorf("read-write = %v, want the account home and the shared store", pol.ReadWritePaths)
	}
}

func TestClaudeSandboxWithoutAccountIsUnchanged(t *testing.T) {
	pol, _ := sandboxFor(t.TempDir(), launch{path: "/bin/true"}, OpenSpec{})
	if _, set := pol.Env["CLAUDE_CONFIG_DIR"]; set {
		t.Errorf("CLAUDE_CONFIG_DIR must not be set for the built-in account")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestCodexJail|TestClaudeSandbox' -count=1`
Expected: FAIL (`unknown field AccountHome`).

- [ ] **Step 3: Implement.** `adapter.go`, in `OpenSpec` after `SubagentLimits`:

```go
	// AccountHome is the home of the vendor account this session runs as
	// (CODEX_HOME for Codex, CLAUDE_CONFIG_DIR for Claude); empty is the
	// built-in account, exactly as sessions ran before accounts existed.
	// AccountSharedPaths are the conversation-store directories that home links
	// to (SharedStorePaths); the jail grants them read-write.
	// Set by Service.applyAccount from the session's own row, never from a tool.
	AccountHome        string
	AccountSharedPaths []string
```
`adapter_codex.go` `jail`:

```go
	home := a.opts.Home
	if spec.AccountHome != "" {
		home = spec.AccountHome
	}
```
Replace `denyRead := carveOut(spec.DenyReadPaths, home)` with

```go
	// DevKit's data directory is hidden from every session; this vendor's own
	// home inside it is carved back out for this vendor's sessions only. An
	// added account's home and the conversation store it links to are carved
	// out together, and no other account's home is.
	denyRead := carveOutAll(spec.DenyReadPaths, append([]string{home}, spec.AccountSharedPaths...)...)
```
(drop the two old comment lines above it) and change the `ReadWritePaths` line to

```go
		ReadWritePaths:         uniqueAbsPaths(append(append([]string{home}, spec.AccountSharedPaths...), spec.ExtraReadWritePaths...)...),
```
`open`: replace the API-key read

```go
	key, err := a.apiKey(ctx)
	if err != nil {
```
with

```go
	// An added account is a ChatGPT sign-in kept in its own home; the Accounts
	// API key (one, global) belongs to the built-in account only.
	var key string
	if spec.AccountHome == "" {
		key, err = a.apiKey(ctx)
	}
	if err != nil {
```
(add `var err error` handling if `err` is already declared above in that function — it is: `workDir, err := …`, so no new declaration is needed.) `start`: change the last argument of `captureEnvironment(AgentTypeCodex, spec, pol.Env, cmd.Path, a.opts.Home)` to `pol.Env["CODEX_HOME"]`.
`adapter_claude.go` `sandboxFor`: after `completeToolPath(env, spec)` insert

```go
	readWrite := append(claudeCredentialPaths(env["HOME"]), spec.ExtraReadWritePaths...)
	denyRead := append([]string(nil), spec.DenyReadPaths...)
	if spec.AccountHome != "" {
		// An added account is a config directory of its own. The machine's own
		// ~/.claude and ~/.claude.json hold the built-in account's login, so they
		// are not granted here (a shared $HOME still shows them, as it always did).
		env["CLAUDE_CONFIG_DIR"] = spec.AccountHome
		kept := append([]string{spec.AccountHome}, spec.AccountSharedPaths...)
		readWrite = append(append([]string(nil), kept...), spec.ExtraReadWritePaths...)
		denyRead = carveOutAll(denyRead, kept...)
	}
```
and in the `sandbox.Policy` literal use `ReadWritePaths: uniqueAbsPaths(readWrite...)` and `DenyReadPaths: denyRead`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -count=1 -run 'TestCodex|TestClaude|Account|Jail|Sandbox'`
Expected: PASS, and the existing Codex/Claude adapter tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/adapter.go internal/agenthub/adapter_codex.go internal/agenthub/adapter_claude.go internal/agenthub/account_spec_test.go
git commit -m "agenthub: Codex and Claude sessions run in their account's home"
```

---

### Task 9: Service — hold the accounts, apply a session's account at every open

**Files:**
- Create: `internal/agenthub/account_switch.go` (first part)
- Modify: `internal/agenthub/service.go` (fields near `SubagentLimits`; `NewService` literal; `resumeRuntime` after `applySandboxExtras`), `internal/agenthub/rewind.go:470`
- Test: `internal/agenthub/account_apply_test.go` (`package agenthub_test`)

- [ ] **Step 1: Write the failing test**

```go
package agenthub_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

type accountRig struct {
	svc     *agenthub.Service
	mock    *agenthub.MockAdapter
	book    *agenthub.SQLAccountStore
	home    string // the "work" account's home
	mu      sync.Mutex
	emitted []json.RawMessage
}

func (r *accountRig) updates() []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]json.RawMessage(nil), r.emitted...)
}

// newAccountRig: a mock-vendor service with its built-in account and one added
// account "work", switching enabled at 90%.
func newAccountRig(t *testing.T, refusal string) *accountRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	rig := &accountRig{mock: agenthub.NewMockAdapter(), book: agenthub.NewSQLAccountStore(db), home: t.TempDir()}
	reg := agenthub.NewRegistry()
	reg.Register(rig.mock)
	rig.svc = agenthub.NewService(reg, agenthub.NewSQLStore(db), func(_ string, u json.RawMessage) {
		rig.mu.Lock()
		rig.emitted = append(rig.emitted, u)
		rig.mu.Unlock()
	})
	rig.svc.Accounts = rig.book
	rig.svc.AccountSharedHome = func(agenthub.AgentType) string { return t.TempDir() }
	rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(agenthub.AgentTypeMock): {
				Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: refusal,
				Order: []string{agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "work"},
			},
		}}, nil
	}
	ctx := context.Background()
	if err := rig.book.Put(ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeMock, Label: "Work", HomeDir: rig.home, SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	return rig
}

// fillBuiltin reports the built-in account at pct in its weekly window.
func (r *accountRig) fillBuiltin(t *testing.T, pct float64) {
	t.Helper()
	w := agenthub.LimitWindow{Label: "Weekly", UsedPercent: pct, ResetsAt: time.Now().Add(24 * time.Hour).Unix()}
	if _, err := r.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := r.book.SaveUsage(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), []agenthub.LimitWindow{w}, "observed", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestResumedSessionOpensInItsAccountsHome(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if rig.mock.LastSpec().AccountHome != "" {
		t.Fatalf("a new session runs as the built-in account, got home %q", rig.mock.LastSpec().AccountHome)
	}
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("OpenSpec.AccountHome = %q, want the account's home %q", got, rig.home)
	}
}
```
`BindSessionAccountForTest` is a tiny exported test seam (below); keep it in `export_test.go`-style file `account_export_test.go` in package `agenthub` (`export_test.go` exists in the package; add to it):

```go
// BindSessionAccountForTest writes a session's account without going through a turn.
func (s *Service) BindSessionAccountForTest(ctx context.Context, id, accountID string) error {
	return s.store.UpdateSessionAccount(ctx, id, accountID, time.Now().UTC())
}
```
(add `"time"` to that file's imports if absent). Note: `ResumeSession(..., reload=true)` needs `SessionLoader`; the mock adapter implements it (existing reload tests use it). If the mock refuses a never-prompted reload, send one prompt first with `rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"})`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestResumedSessionOpensInItsAccountsHome -count=1`
Expected: FAIL (`svc.Accounts undefined`).

- [ ] **Step 3: Implement.** `service.go`, in the `Service` struct after `SubagentLimits`:

```go
	// Accounts holds the vendor accounts a session can run as and what is known
	// of their usage. nil means the feature is off: every session runs as the
	// built-in account, exactly as before. AccountPolicy reads the person's
	// switching rules, AccountSharedHome the built-in account's home for a vendor
	// (where the conversation store an added account links to lives). All set in
	// main.go; read on every turn and every open, so an edit applies at once.
	Accounts          AccountBook
	AccountPolicy     func() (preferences.AgentAccountPolicy, error)
	AccountSharedHome func(vendor AgentType) string

	// accountSwitches remembers, per session, when it last moved and from where
	// (anti flip-flop; not persisted). sessionAccount caches the account id a
	// session's runtime runs as, for usage updates that carry only a session id.
	// turnProgress marks a turn that produced output or a tool call, for
	// onLimitRefusal. usageSeen is the last usage written per account, so an
	// unchanged update writes nothing. All guarded by mu.
	accountSwitches map[string]accountSwitch
	sessionAccount  map[string]string
	turnProgress    map[string]bool
	usageSeen       map[string]string
```
and in the `NewService` literal add `accountSwitches: make(map[string]accountSwitch), sessionAccount: make(map[string]string), turnProgress: make(map[string]bool), usageSeen: make(map[string]string),`. In `resumeRuntime` after `s.applySandboxExtras(&spec)` add `s.applyAccount(ctx, &spec, sess)`; in `rewind.go` after its `s.applySandboxExtras(&spec)` add `s.applyAccount(ctx, &spec, r.session)`.
`account_switch.go`:

```go
package agenthub

import (
	"context"
	"log"
)

// accountSwitch is when a session last moved to another account, and from which.
type accountSwitch struct {
	at   time.Time
	from string
}

// accountIDOf is the account a session runs as: its own, else the vendor's built-in.
func accountIDOf(sess AgentSession) string {
	if sess.AccountID != "" {
		return sess.AccountID
	}
	return BuiltinAccountID(sess.AgentType)
}

// applyAccount points an OpenSpec at the session's account home. The built-in
// account changes nothing. An account that is gone falls back to the built-in
// one (logged): a session must still open.
func (s *Service) applyAccount(ctx context.Context, spec *OpenSpec, sess AgentSession) {
	id := accountIDOf(sess)
	s.mu.Lock()
	s.sessionAccount[sess.ID] = id
	s.mu.Unlock()
	if s.Accounts == nil || sess.AccountID == "" {
		return
	}
	acct, err := s.Accounts.Get(ctx, sess.AccountID)
	if err != nil {
		log.Printf("agenthub: session %s: account %s: %v; opening as the built-in account", sess.ID, sess.AccountID, err)
		return
	}
	if acct.Builtin() {
		return
	}
	spec.AccountHome = acct.HomeDir
	if s.AccountSharedHome != nil {
		spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
	}
}
```
(imports: `context`, `log`, `time`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'TestResumedSessionOpensInItsAccountsHome' -count=1 && go build ./... && go vet ./internal/agenthub/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: a resumed session opens in its account's home"
```

---

### Task 10: Switch accounts at the start of a turn

**Files:**
- Modify: `internal/agenthub/account_switch.go` (append), `internal/agenthub/errors.go` (new error), `internal/agenthub/service.go` (`invoke`: before `rt, err := s.runtimeFor(ctx, sess)`)
- Test: `internal/agenthub/account_turn_test.go` (`package agenthub_test`, uses `newAccountRig` from Task 9)

- [ ] **Step 1: Write the failing test**

```go
package agenthub_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func startSession(t *testing.T, rig *accountRig) agenthub.AgentSession {
	t.Helper()
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func rowAccount(t *testing.T, rig *accountRig, id string) string {
	t.Helper()
	sess, err := rig.svc.GetStatus(uiCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.AccountID
}

func TestTurnMovesToTheNextAccountOverTheThreshold(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	sess := startSession(t, rig)

	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Fatalf("session account = %q, want work", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("the turn ran in %q, want the account home %q", got, rig.home)
	}
	var noticed bool
	for _, u := range rig.updates() {
		if strings.Contains(string(u), "accountSwitch") && strings.Contains(string(u), "Weekly 95% ≥ 90%") {
			noticed = true
		}
	}
	if !noticed {
		t.Error("the move must leave a notice carrying its reason")
	}
}

func TestTurnStaysUnderTheThreshold(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 40)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in (empty)", got)
	}
}

func TestTurnDoesNothingWithTheFeatureOff(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 99)
	rig.svc.Accounts = nil
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in with the feature off", got)
	}
}

func TestTurnIsRefusedWhenEveryAccountIsExhausted(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	ctx := context.Background()
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	for _, id := range []string{agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "work"} {
		if err := rig.book.MarkExhausted(ctx, id, until); err != nil {
			t.Fatal(err)
		}
	}
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want ErrAllAccountsLimited", err)
	}
	if status := mustStatus(t, rig, sess.ID); status != agenthub.SessionStatusIdle {
		t.Errorf("status after the refusal = %s, want idle (nothing ran)", status)
	}
}

func mustStatus(t *testing.T, rig *accountRig, id string) agenthub.SessionStatus {
	t.Helper()
	sess, err := rig.svc.GetStatus(uiCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Status
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestTurnMoves|TestTurnStays|TestTurnDoesNothing|TestTurnIsRefused' -count=1`
Expected: FAIL (`undefined: agenthub.ErrAllAccountsLimited`).

- [ ] **Step 3: Implement.** `errors.go`, next to `ErrAgentUsageLimit`:

```go
// ErrAllAccountsLimited: switching is on and every signed-in account of the
// vendor is marked as having reached its limit. The turn is not started; the
// sentence names when the first account returns.
var ErrAllAccountsLimited = errors.New("agenthub.allAccountsLimited")
```
Append to `account_switch.go` (add imports `encoding/json`, `errors`, `fmt`, `devkit/internal/preferences`):

```go
// accountPolicyFor reads the person's policy for a vendor; ok is false when the
// feature is off, unconfigured or unreadable (logged), so the turn runs as before.
func (s *Service) accountPolicyFor(vendor AgentType) (preferences.AccountVendorPolicy, bool) {
	if s.Accounts == nil || s.AccountPolicy == nil {
		return preferences.AccountVendorPolicy{}, false
	}
	policy, err := s.AccountPolicy()
	if err != nil {
		log.Printf("agenthub: account policy: %v", err)
		return preferences.AccountVendorPolicy{}, false
	}
	vp, ok := policy.Vendors[string(vendor)]
	return vp, ok && vp.Enabled
}

// decideAccount asks PickAccount about a session as it is now.
func (s *Service) decideAccount(ctx context.Context, sess AgentSession) (PickDecision, preferences.AccountVendorPolicy, bool) {
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return PickDecision{}, vp, false
	}
	accounts, err := s.Accounts.List(ctx, sess.AgentType)
	if err != nil {
		log.Printf("agenthub: accounts of %s: %v", sess.AgentType, err)
		return PickDecision{}, vp, false
	}
	s.mu.Lock()
	prev := s.accountSwitches[sess.ID]
	s.mu.Unlock()
	return PickAccount(PickInput{
		Policy: vp, Accounts: accounts, Current: sess.AccountID,
		Now: time.Now(), SwitchedAt: prev.at, Previous: prev.from,
	}), vp, true
}

// switchAccountForTurn runs at the start of a turn, before the runtime is
// resolved. It moves the session to the account PickAccount names (closing the
// idle process; the next resolve reopens it in the new home), or refuses the
// turn when every account is at its limit. A failure to move leaves the session
// where it is: the turn runs, or fails, as it did before accounts.
func (s *Service) switchAccountForTurn(ctx context.Context, sess *AgentSession) error {
	d, _, ok := s.decideAccount(ctx, *sess)
	if !ok {
		return nil
	}
	if d.AllLimited {
		return fmt.Errorf("%w: every account is at its limit; the first returns at %s",
			ErrAllAccountsLimited, d.RetryAt.Local().Format("15:04 Mon 2 Jan"))
	}
	if !d.Switch {
		return nil
	}
	from := accountIDOf(*sess)
	if err := s.moveSession(ctx, sess, d.Target); err != nil {
		log.Printf("agenthub: session %s: could not move to account %s: %v", sess.ID, d.Target, err)
		return nil
	}
	s.mu.Lock()
	s.accountSwitches[sess.ID] = accountSwitch{at: time.Now(), from: from}
	s.mu.Unlock()
	s.handleUpdate(context.WithoutCancel(ctx), sess.ID, accountSwitchUpdate(from, d.Target, d.Reason))
	return nil
}

// moveSession closes the session's idle runtime and binds it to target. The
// caller holds the session's claim (invoking), so nothing resumes it meanwhile.
func (s *Service) moveSession(ctx context.Context, sess *AgentSession, target string) error {
	s.mu.Lock()
	rt := s.runtimes[sess.ID]
	closeEndpoint := s.endpoints[sess.ID]
	delete(s.runtimes, sess.ID)
	delete(s.endpoints, sess.ID)
	s.forgetWorkspaceContextLocked(sess.ID)
	s.mu.Unlock()
	var closeErr error
	if rt != nil {
		closeErr = rt.Close(ctx)
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}
	if err := s.recorder.Flush(ctx, sess.ID); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sess.ID, err)
	}
	if err := s.store.UpdateSessionAccount(ctx, sess.ID, target, time.Now().UTC()); err != nil {
		return err
	}
	sess.AccountID = target
	s.mu.Lock()
	s.sessionAccount[sess.ID] = target
	s.mu.Unlock()
	return closeErr
}

// accountSwitchUpdate is the transcript notice of a move. Its shape is a
// session_info_update carrying DevKit's own key, like the other DevKit extras
// (`_devkit`); the window learns to draw it in the accounts UI plan.
func accountSwitchUpdate(from, to, reason string) json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateSessionInfo),
		"_meta": map[string]any{
			"devkit": map[string]any{"accountSwitch": map[string]any{"from": from, "to": to, "reason": reason}},
		},
	})
}
```
`service.go` `invoke`, immediately before `rt, err := s.runtimeFor(ctx, sess)` (after the `cp := checkpoints.Begin(...)` block):

```go
	// Accounts: the start of a turn is the only moment a session changes
	// account (never inside a running turn).
	if err := s.switchAccountForTurn(ctx, &sess); err != nil {
		cp.Finish(context.WithoutCancel(ctx), TurnResult{Cancelled: true})
		release()
		return InvokeResult{}, err
	}
```
(`marshalUpdate` is the existing helper used by `sessionFailureUpdate`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'TestTurn|TestResumed' -count=1 && go test ./internal/agenthub/ -count=1`
Expected: PASS, the rest of the package still green. If `TestTurnIsRefusedWhenEveryAccountIsExhausted` leaves the session `running`, make sure the refusal path above runs before the session is marked running (it does: `release()` only, no status write).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: a turn moves to the next account when its own is at the threshold"
```

---

### Task 11: Observe usage from the updates sessions already receive

**Files:**
- Modify: `internal/agenthub/account_switch.go` (append), `internal/agenthub/service.go` (`handleUpdate`)
- Test: `internal/agenthub/account_usage_internal_test.go` (`package agenthub`)

- [ ] **Step 1: Write the failing test**

```go
package agenthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"devkit/internal/identity"
	"devkit/internal/storage"
)

func TestUsageUpdatesAreMergedIntoTheSessionsAccount(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	book := NewSQLAccountStore(db)
	svc := NewService(reg, NewSQLStore(db), nil)
	svc.Accounts = book
	ctx := identity.WithCaller(context.Background(), identity.UI())
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.List(ctx, AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	svc.sessionAccount[sess.ID] = BuiltinAccountID(AgentTypeMock)
	svc.mu.Unlock()

	update := json.RawMessage(`{"sessionUpdate":"usage_update","used":10,"size":100,
		"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":92}}}`)
	svc.handleUpdate(ctx, sess.ID, update)

	got, err := book.Get(ctx, BuiltinAccountID(AgentTypeMock))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Usage) != 1 || got.Usage[0].Label != "5-hour" || got.Usage[0].UsedPercent != 92 || got.UsageSource != "observed" {
		t.Fatalf("account usage = %+v (source %q), want one 5-hour window at 92 observed", got.Usage, got.UsageSource)
	}
}

func TestAnUnchangedUsageUpdateWritesNothingMore(t *testing.T) {
	svc := &Service{usageSeen: map[string]string{}}
	w := []LimitWindow{{Label: "5-hour", UsedPercent: 50, ResetsAt: 1900000000}}
	if !svc.usageChanged("a", w) {
		t.Fatal("the first reading is a change")
	}
	if svc.usageChanged("a", w) {
		t.Error("the same reading again must not be a change")
	}
	w[0].UsedPercent = 51
	if !svc.usageChanged("a", w) {
		t.Error("a different percentage is a change")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestUsageUpdates|TestAnUnchangedUsage' -count=1`
Expected: FAIL (`svc.usageChanged undefined`).

- [ ] **Step 3: Implement.** Append to `account_switch.go`:

```go
// observeAccountUsage merges the usage windows an update carries into the
// account the session runs as. It is only what this app's sessions saw: on a
// shared account, a lower bound. Best effort — a failed write is logged, never
// returned, because the update must still reach the transcript.
func (s *Service) observeAccountUsage(ctx context.Context, sessionID string, parsed SessionUpdate) {
	if s.Accounts == nil || parsed.Kind != UpdateUsage || parsed.Usage == nil || len(parsed.Usage.Limits) == 0 {
		return
	}
	s.mu.Lock()
	id := s.sessionAccount[sessionID]
	s.mu.Unlock()
	if id == "" {
		sess, err := s.store.GetSession(ctx, sessionID)
		if err != nil {
			return
		}
		id = accountIDOf(sess)
		s.mu.Lock()
		s.sessionAccount[sessionID] = id
		s.mu.Unlock()
	}
	if !s.usageChanged(id, parsed.Usage.Limits) {
		return
	}
	if err := s.Accounts.SaveUsage(ctx, id, parsed.Usage.Limits, "observed", time.Now()); err != nil {
		log.Printf("agenthub: account %s: saving usage: %v", id, err)
	}
}

// usageChanged reports whether windows differ from what was last written for
// the account, and remembers them.
func (s *Service) usageChanged(accountID string, windows []LimitWindow) bool {
	raw, _ := json.Marshal(windows)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usageSeen[accountID] == string(raw) {
		return false
	}
	s.usageSeen[accountID] = string(raw)
	return true
}
```
In `handleUpdate` after the `UpdateProviderRetry` block add:

```go
	if parsed.Kind == UpdateUsage {
		s.observeAccountUsage(ctx, sessionID, parsed)
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -run 'TestUsageUpdates|TestAnUnchangedUsage' -count=1`
Expected: PASS. (If the first test's `_meta` key is not picked up, check that `SessionUpdate.Usage.Limits` is filled by `limitsOfRateLimit` for the wire shape in `update.go:655-660`; the JSON above is that shape.)

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: sessions' usage updates are kept on their account"
```

---

### Task 11b: Claude's usage windows come from `unifiedWindows`, as percentages

Phase 0 measured that the real `rate_limit_event` carries both windows in `rate_limit_info.unifiedWindows.{five_hour,seven_day}` as **0–1 fractions** (0.69 while `/usage` said 69%) and no top-level `utilization` while `status` is `allowed`; `limitsOfRateLimit` reads only the top-level value as a percentage. Without this task Claude accounts would never report a window to Task 11, and `PickAccount` (thresholds in percent) would never trigger for them.

**Files:**
- Create: `internal/agenthub/claude_limits.go`
- Modify: `internal/agenthub/update.go` (the `case UpdateUsage:` branch, right after `usage.Limits = limitsOfRateLimit(usage.RateLimit)`)
- Test: `internal/agenthub/claude_limits_test.go` (`package agenthub`)

- [ ] **Step 1: Write the failing test** (the first update is the event captured in Phase 0):

```go
package agenthub

import "testing"

func TestClaudeUnifiedWindowsBecomePercentWindows(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{
		"status":"allowed","resetsAt":1792653600,"rateLimitType":"five_hour","overageStatus":"rejected",
		"overageDisabledReason":"org_level_disabled_until","isUsingOverage":false,
		"unifiedWindows":{"five_hour":{"utilization":0.69,"resetsAt":1792653600},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v, want the two unified windows", got.Usage)
	}
	want := []LimitWindow{
		{Label: "5-hour", UsedPercent: 69, ResetsAt: 1792653600, Status: "ok"},
		{Label: "Weekly", UsedPercent: 15, ResetsAt: 1793080800, Status: "ok"},
	}
	for i, w := range want {
		if got.Usage.Limits[i] != w {
			t.Errorf("window %d = %+v, want %+v", i, got.Usage.Limits[i], w)
		}
	}
}

func TestClaudeUnifiedWindowStatusFollowsThePercent(t *testing.T) {
	cases := map[string]string{"0.79": "ok", "0.8": "warning", "0.93": "warning", "1": "reached"}
	for fraction, status := range cases {
		update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"allowed",
			"unifiedWindows":{"five_hour":{"utilization":` + fraction + `,"resetsAt":1792653600}}}}}`)
		got := ParseSessionUpdate(update)
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Status != status {
			t.Errorf("utilization %s: limits = %+v, want status %q", fraction, got.Usage, status)
		}
	}
}

func TestClaudeRateLimitWithoutUnifiedWindowsIsReadAsBefore(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].UsedPercent != 82 {
		t.Fatalf("limits = %+v, want the old single window at 82", got.Usage)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestClaudeUnified|TestClaudeRateLimitWithout' -count=1`
Expected: FAIL (`limits = …, want the two unified windows`).

- [ ] **Step 3: Implement** `claude_limits.go`:

```go
package agenthub

import (
	"encoding/json"
	"math"
	"sort"
)

// unifiedWindowOrder is the order Claude's windows are listed in; any other key
// follows, alphabetically.
var unifiedWindowOrder = []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"}

// unifiedLimitWindows reads the windows a Claude rate_limit_event carries in
// rate_limit_info.unifiedWindows. Measured against the real CLI: utilization
// there is a 0–1 fraction (0.69 while /usage said 69%), so it is shown as a
// percentage rounded to 0.01. The key is not in the SDK's declared type, so an
// absent or unreadable one is no windows, never an error.
func unifiedLimitWindows(meta json.RawMessage) []LimitWindow {
	if len(meta) == 0 {
		return nil
	}
	var parsed struct {
		RateLimit *struct {
			Unified map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"_claude/rateLimit"`
	}
	if json.Unmarshal(meta, &parsed) != nil || parsed.RateLimit == nil || len(parsed.RateLimit.Unified) == 0 {
		return nil
	}
	keys := make([]string, 0, len(parsed.RateLimit.Unified))
	for _, k := range unifiedWindowOrder {
		if _, ok := parsed.RateLimit.Unified[k]; ok {
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range parsed.RateLimit.Unified {
		known := false
		for _, o := range unifiedWindowOrder {
			known = known || o == k
		}
		if !known {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	out := make([]LimitWindow, 0, len(keys))
	for _, k := range keys {
		w := parsed.RateLimit.Unified[k]
		pct := math.Round(w.Utilization*10000) / 100
		status := "ok"
		switch {
		case pct >= 100:
			status = "reached"
		case pct >= 80:
			status = "warning"
		}
		out = append(out, LimitWindow{Label: claudeLimitLabel(k), UsedPercent: pct, ResetsAt: w.ResetsAt, Status: status})
	}
	return out
}
```
and in `update.go`, `case UpdateUsage:` after `usage.Limits = limitsOfRateLimit(usage.RateLimit)` add:

```go
		if unified := unifiedLimitWindows(wire.Meta); len(unified) > 0 {
			usage.Limits = unified
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -count=1`
Expected: PASS, including the existing `update_meta_test.go` and `codex_insights_test.go` cases (they carry no `unifiedWindows`).

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/claude_limits.go internal/agenthub/claude_limits_test.go internal/agenthub/update.go
git commit -m "agenthub: Claude's usage windows are read from unifiedWindows, as percentages"
```

---

### Task 12: A real limit marks the account, and `onLimitRefusal` follows

**Files:**
- Modify: `internal/agenthub/account_switch.go` (append), `internal/agenthub/stop_service.go` (`recordStop`), `internal/agenthub/service.go` (`Invoke`, `invoke` start, `handleUpdate`)
- Test: `internal/agenthub/account_refusal_test.go` (`package agenthub_test`, uses the rig)

- [ ] **Step 1: Write the failing test**

```go
package agenthub_test

import (
	"errors"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

const limitPrompt = "please FAIL_USAGE_LIMIT now" // the mock adapter's usage-limit marker

func TestRefusalMarksTheAccountAndNoneLeavesThePromptAlone(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)

	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("with `none` the refused turn must not move the session; account = %q", got)
	}
	built, _ := rig.book.Get(ctxBackground(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	if !built.ExhaustedUntil.After(nowish()) {
		t.Fatalf("the refused account must be marked exhausted, got %v", built.ExhaustedUntil)
	}
	// The next prompt the person sends starts on the next account.
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("next turn account = %q, want work", got)
	}
}

func TestResendIfNoProgressSendsTheSamePromptOnceOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	// The mock refuses the same prompt on the second account too: the chain ends there.
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit from the resend", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("the resend must have run on work; account = %q", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("the resend ran in %q, want %q", got, rig.home)
	}
}

func TestContinueSendsAFixedMessageOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt}); err != nil {
		t.Fatalf("the continue message succeeds on the next account, got %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want work", got)
	}
}

func TestNoFollowUpWhenThereIsNowhereToGo(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	// work is exhausted too, so nothing can take the continue message.
	_, _ = rig.book.List(ctxBackground(), agenthub.AgentTypeMock)
	if err := rig.book.MarkExhausted(ctxBackground(), "work", nowish().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the original usage limit (no follow-up, not a new error)", err)
	}
}
```
with the small helpers appended to `account_turn_test.go`:

```go
func ctxBackground() context.Context { return context.Background() }
func nowish() time.Time              { return time.Now() }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestRefusal|TestResendIf|TestContinueSends|TestNoFollowUp' -count=1`
Expected: FAIL (the marker is never set).

- [ ] **Step 3: Implement.** Append to `account_switch.go`:

```go
// accountProbeCooldown is how long a refusal that names no reset keeps an
// account out, before the next turn may try it again (a half-open probe).
// Defined here, next to its use (Task 6 no longer carries it: it was unused there).
const accountProbeCooldown = 15 * time.Minute

// continueAfterLimit is what `continue` sends on the next account.
const continueAfterLimit = "The previous account reached its usage limit. Continue where you left off."

// markAccountLimited records, after a turn the vendor refused for a real limit,
// when the account is usable again: the reached window's reset, else the stop's
// retry time, else a short cooldown after which the next turn may probe it.
func (s *Service) markAccountLimited(ctx context.Context, sess AgentSession, stop SessionStop) {
	if s.Accounts == nil || (stop.Kind != StopUsageLimit && stop.Kind != StopRateLimited) {
		return
	}
	if _, err := s.Accounts.List(ctx, sess.AgentType); err != nil { // creates the built-in row
		return
	}
	id := accountIDOf(sess)
	acct, err := s.Accounts.Get(ctx, id)
	if err != nil {
		return
	}
	now := time.Now()
	until := now.Add(accountProbeCooldown)
	if reset := reachedReset(acct.Usage, now); !reset.IsZero() {
		until = reset
	} else if stop.RetryAfter != nil && stop.RetryAfter.After(now) {
		until = *stop.RetryAfter
	}
	if err := s.Accounts.MarkExhausted(ctx, id, until); err != nil {
		log.Printf("agenthub: account %s: marking its limit: %v", id, err)
	}
}

// reachedReset is the latest future reset among windows the vendor says are reached.
func reachedReset(windows []LimitWindow, now time.Time) time.Time {
	var out time.Time
	for _, w := range windows {
		if w.Status != "reached" || w.ResetsAt <= now.Unix() {
			continue
		}
		if t := time.Unix(w.ResetsAt, 0); t.After(out) {
			out = t
		}
	}
	return out
}

// noteTurnProgress marks the session's running turn as having produced output
// or a tool call, which is what makes a refusal after it unsafe to resend.
func (s *Service) noteTurnProgress(sessionID string, kind UpdateKind) {
	switch kind {
	case UpdateAgentMessageChunk, UpdateAgentThoughtChunk, UpdateToolCall:
		s.mu.Lock()
		s.turnProgress[sessionID] = true
		s.mu.Unlock()
	}
}

func (s *Service) turnHadProgress(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnProgress[sessionID]
}

func (s *Service) resetTurnProgress(sessionID string) {
	s.mu.Lock()
	delete(s.turnProgress, sessionID)
	s.mu.Unlock()
}

// refusalFollowUp is what to send after a turn was refused for a real limit, per
// the person's onLimitRefusal. ok is false for `none`, for a turn that made
// progress when the choice is resendIfNoProgress, and when there is no other
// account to run it on (a follow-up on the refused account would only fail again).
func (s *Service) refusalFollowUp(ctx context.Context, in InvokeInput) (InvokeInput, bool) {
	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeInput{}, false
	}
	d, vp, ok := s.decideAccount(ctx, sess)
	if !ok || !d.Switch {
		return InvokeInput{}, false
	}
	switch vp.OnLimitRefusal {
	case preferences.RefusalResendIfNoProgress:
		if s.turnHadProgress(in.SessionID) {
			return InvokeInput{}, false
		}
		return in, true
	case preferences.RefusalContinue:
		return InvokeInput{SessionID: in.SessionID, CallerSessionID: in.CallerSessionID, Prompt: continueAfterLimit}, true
	}
	return InvokeInput{}, false
}
```
`stop_service.go` `recordStop`: after `stop := StopFromError(...)` and before the store write, add `s.markAccountLimited(ctx, sess, stop)`.
`service.go`: in `handleUpdate` after the usage block add `s.noteTurnProgress(sessionID, parsed.Kind)`. In `invoke`, right after the `s.invoking[in.SessionID] = struct{}{}` claim is released-or-set (just after `release := func() {…}` is defined) add `s.resetTurnProgress(in.SessionID)`. Replace `Invoke` with:

```go
func (s *Service) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	result, err := s.invoke(ctx, in, nil)
	if err != nil && errors.Is(err, ErrAgentUsageLimit) {
		// The vendor refused the turn for a real limit and the account is marked.
		// Whether the prompt goes again on the next account is the person's setting.
		// One follow-up at most: it calls invoke, not Invoke.
		if next, ok := s.refusalFollowUp(ctx, in); ok {
			return s.invoke(ctx, next, nil)
		}
	}
	return result, err
}
```
(`errors` is already imported in `service.go`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agenthub/ -count=1`
Expected: PASS for the four new tests and the whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub
git commit -m "agenthub: a real limit marks the account; onLimitRefusal decides what follows"
```

---

### Task 13: Wire it in `main.go`

**Files:** Modify `main.go` near line 596 (`agentSvc.SubagentLimits = …`).

- [ ] **Step 1: Check how the data directory and DB are named there.** `dataDir` is defined at `main.go:112` and `db` at `main.go:116`; the Codex home is `filepath.Join(dataDir, "agent-homes", "codex")` (`main.go:355`).

- [ ] **Step 2: Add the wiring** after `agentSvc.SubagentLimits = container.Preferences.AgentSubagentLimits`:

```go
	// Accounts: several vendor accounts per vendor, switched at the start of a
	// turn (spec 2026-10-22-agent-multi-account-failover). Off until the person
	// enables a vendor in Settings; the built-in accounts are what sessions
	// always ran as. The conversation store an added account links to is the
	// built-in account's: ~/.claude for Claude, DevKit's own Codex home.
	agentSvc.Accounts = agenthub.NewSQLAccountStore(db)
	agentSvc.AccountPolicy = container.Preferences.AgentAccountPolicy
	agentSvc.AccountSharedHome = func(vendor agenthub.AgentType) string {
		switch vendor {
		case agenthub.AgentTypeClaude:
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, ".claude")
			}
		case agenthub.AgentTypeCodex:
			return filepath.Join(dataDir, "agent-homes", "codex")
		}
		return ""
	}
```
(`os` and `filepath` are already imported in `main.go`.)

- [ ] **Step 3: Build and vet**

Run: `go build ./... && go vet -tags server ./internal/... 2>&1 | head`
Expected: no output from vet. (`go build ./...` of the root package needs `frontend/dist`; if it is missing, run `go build ./internal/... ./cmd/...` and `gofmt -l main.go` instead, and say so.)

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "main: wire the vendor account book and its policy into Agent Hub"
```

---

### Task 14: Document it, and run the full checks

**Files:** Modify `AGENTS.md` (Agent Hub section, after the "A parent briefs a child…" bullet).

- [ ] **Step 1: Add this bullet**

```markdown
- **A vendor can have several accounts, and a session moves between them at the start of a turn** (spec
  `docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md`; `internal/agenthub/accounts*.go`,
  `accountpick.go`, `account_home.go`, `account_switch.go`; migration 45). **Backend only so far** — no bridge method, no
  Settings page, no UI notice (the accounts UI plan); `Service.Accounts` is set in `main.go`, nothing enables a vendor yet
  except writing `agent_account_policy`. An account is a row (no credential): the vendor CLI keeps its login in the
  account's own home, `CODEX_HOME` / `CLAUDE_CONFIG_DIR` under `<data>/agent-accounts/<vendor>/<id>` (beside the built-in
  Codex home, never inside it: a built-in session's deny list is carved to its own home only). The built-in account
  (`<vendor>-default`, empty home) is what sessions always ran as. The conversation store (`projects/`, `sessions/`) is a
  symlink to the built-in account's (`PrepareAccountHome`, never replaces a real directory), so a moved session resumes the
  same native conversation; `OpenSpec.AccountHome` / `AccountSharedPaths` come from the session row in `applyAccount`, never
  from a tool, and the jail carves out exactly those roots (`carveOutAll`).
  - **`PickAccount` is pure**: stay unless the current account is marked exhausted or any window not yet reset is at or over
    the threshold (`thresholdPercent`, weekly windows `weeklyThresholdPercent`); the target is the first account in the
    person's order that is signed in, not exhausted and under the threshold; 5 minutes' hold after a move (a real refusal
    overrides), a 10-point margin to go back; every account exhausted → `ErrAllAccountsLimited` (the turn does not start).
    A reading is used as it is — usage only grows until a reset, so an old reading over the threshold is still true.
  - **It runs in `Service.invoke` before the runtime is resolved** (`switchAccountForTurn`): close the idle process, write
    `agent_account_id`, record a `session_info_update` carrying `_meta.devkit.accountSwitch`, reopen in the new home. Never
    inside a running turn. A move that fails leaves the session where it was.
  - **Usage is observed, not polled**: `UpdateUsage` windows are merged into the session's account by label
    (`observeAccountUsage`; Claude's real event carries both windows in `unifiedWindows` as 0–1 fractions, read by `claude_limits.go`). On a shared team login that is a lower bound. A real
    refusal (`usage_limit`, `rate_limited`) marks the account exhausted until the reached window's reset, else the stop's
    retry time, else 15 minutes (`markAccountLimited`, only ever later). `onLimitRefusal` (`none` default,
    `resendIfNoProgress`, `continue`) is read in `Invoke`; one follow-up at most, only when another account can take it.
  - **Not done (the accounts UI plan)**: `UsageProbe` (Codex `account/rateLimits/read`; Claude per the spec's Phase 0),
    bridge methods and grants, `bridge.keptCodes` for `agenthub.allAccountsLimited`, the Settings page, drawing the notice,
    the `devkit`-sourced "continue" prompt (today it is an ordinary prompt), removing an account, sign-in into an account
    home. **Not verified against real vendors** except as the spec's "Measured" section says.
```

- [ ] **Step 2: Run the full checks**

```bash
gofmt -l internal main.go
go vet -tags server ./internal/... ./cmd/...
go test ./internal/agenthub/ ./internal/preferences/ ./internal/storage/ ./internal/arch/ ./internal/bridge/ ./internal/mcp/ -count=1
```
Expected: `gofmt` prints nothing except the known `internal/crypto/keyring.go` (run `gofmt -w` on any file of this plan it lists — the table-style test literals are the likely ones); vet clean; all packages `ok`.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: AGENTS.md — vendor accounts backend"
```

---

## Self-review (against the spec)

**Coverage.** Model + migration → Tasks 2–4. Policy (all fields) → 5. Choosing an account, anti flip-flop, all-limited → 6, 10. Usage observed → 11. Real limit marking + `onLimitRefusal` (3 modes, single follow-up, no follow-up with nowhere to go) → 12. Isolation (homes, shared store, jail carve-out, API key only for built-in Codex) → 7, 8. Where it runs (before runtime, never mid-turn, failure leaves old account) → 10. Phase 0 + gate → 1. Spec corrections found while reading code → 0. Restart persistence: `agent_account_id`, `exhausted_until`, usage are in SQLite; hold timers are in memory (as the spec says).
**Deliberately not here (Plan 2):** `UsageProbe` and the `shared`-account re-check before a turn (`refreshIfStale`, `usageMaxAgeSeconds` is stored and validated but not yet read), bridge/grants/error codes, Settings page, notice drawing, summary-card row, toasts, sign-in into an account home, removing an account, `devkit`-sourced continue prompt (today `continue` sends an ordinary user prompt — a known simplification), `no-shared-history` marking after a failed resume, `TestNoToolTouchesAgentAccounts`/`TestNewContainerGrantsAgentAccounts` (nothing to test until a bridge method exists), the Phase 0 e2e under `cmd/pdfe2e`.
**Placeholders.** None; every code step has code. Task 1 is commands because it measures real vendors.
**Type consistency.** `AccountRecord`, `AccountBook` (`List/Get/Put/SaveUsage/MarkExhausted`), `BuiltinAccountID`, `PickInput/PickDecision/PickAccount`, `PrepareAccountHome/SharedStorePaths/carveOutAll`, `OpenSpec.AccountHome/AccountSharedPaths`, `Service.Accounts/AccountPolicy/AccountSharedHome`, `accountSwitch`, `accountIDOf`, `applyAccount`, `decideAccount`, `switchAccountForTurn`, `moveSession`, `observeAccountUsage/usageChanged`, `markAccountLimited`, `refusalFollowUp`, `ErrAllAccountsLimited`, `preferences.AgentAccountPolicy/AccountVendorPolicy/Refusal*` are used with the same names and signatures in every task. The mock vendor stands in for Claude/Codex in service tests (policy and rows are keyed by vendor string; nothing in `Service` names a vendor).
**Risks to watch while executing.** (1) The code was written against code read, not compiled: expect small fixes at each Step 2/4 (e.g. unused imports, `err` shadowing in `CodexAdapter.open`). (2) `TestResumedSessionOpensInItsAccountsHome` relies on the mock accepting `ResumeSession(reload=true)`; the plan says what to do if it does not. (3) `carveOutAll` lists directories when a session opens, like `carveOut` — an entry created later beside a kept path is visible to that session.
