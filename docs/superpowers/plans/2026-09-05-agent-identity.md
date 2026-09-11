# Agent Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** DevKit knows *which agent* is calling on the MCP path, and every policy that only needed that fact becomes correct: per-caller pending prompts, `allow_always` scoped to `(caller, tool, profile)`, attributed and persisted audit, descendant-only orchestration, depth enforced from identity, `cwd` bounded by the parent.

**Architecture:** Identity is a *socket*, not a token. Each Agent Hub session gets its own unix socket under `<UserConfigDir>/DevKit/agents/`; only that session's sandbox may connect to it (`Policy.AllowUnixSockets`), so the accepting side can stamp `Caller{agent, sessionID}` with no handshake. The shared `mcp.sock` stays for the user's own terminals (`user-terminal`). A per-identity `*sdk.Server` with one receiving middleware puts the caller into `ctx`; the 56 tool handlers are untouched. `capabilities.Gateway` reads the caller from `ctx` and stamps audit; `agenthub.Service` and `interaction.Hub` read it to enforce policy.

**Tech Stack:** Go 1.2x, `github.com/modelcontextprotocol/go-sdk` v1.7.0 (`AddReceivingMiddleware`, `NewInMemoryTransports`), SQLite via `internal/storage` migrations, Seatbelt (`sandbox-exec`) / bubblewrap, ACP v1 (`session/new.mcpServers`, stdio variant: `name, command, args, env` — no `type` field, `env` required), React + zustand + Wails3 bindings.

**Spec:** the four base-doc pages updated 2026-09-05 — `base-doc/content/projects/dev-kit/agent-hub.mdx` § "Agent identity & ranh giới giữa các session (Design)", `mcp-agent.mdx` § "Caller identity — socket riêng cho từng Agent Hub session (Design)", `sandbox.mdx` § "`AllowUnixSockets` (Design)", `security-controls.mdx` § Remaining security risks. Read them first; this plan implements them and nothing else.

**Measured facts this plan relies on (do not re-derive):**
- Seatbelt matches **resolved** paths: `(allow network-outbound (literal "/tmp/x.sock"))` never matches because the kernel sees `/private/tmp/x.sock`. With the resolved path, the single `network-outbound` literal is sufficient — no `file-read*` on the socket, no metadata grant on parent dirs (verified live, both `NetworkHost` and `NetworkNone`).
- A nested `sandbox-exec` inside the DevKit jail fails (`sandbox_apply: Operation not permitted`); vendor sandboxes must stay off. Not touched here.
- ACP `McpServerStdio` requires `name, command, args, env`; `env` must be present (`[]`, not `null`).

**Conventions:** every Go step ends with `gofmt -l <pkg>` printing nothing and `go vet ./<pkg>/` passing. Commit after each task with the message given. Repo root for all commands: `dev-kit-app/`.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/identity/identity.go` (new, leaf) | `Caller{Kind, SessionID}`, ctx helpers, audit label |
| `internal/audit/events.go` | `Event` gains `Caller`, `At` |
| `internal/audit/tee.go` (new) | fan-out sink |
| `internal/audit/sqlsink.go` (new) | batched SQLite sink, backpressure, fallback, prune |
| `internal/storage/sqlite.go` | `TableAuditEvents`, migration `m20` |
| `internal/capabilities/gateway.go` | stamps `Caller`/`At` from ctx |
| `internal/bridge/gateway.go` | defaults unattributed calls to `ui` |
| `internal/interaction/hub.go`, `errors.go` | pending map, `Caller` on `Pending`, `allowAlways` key `(caller, tool, profile)`, `ForgetCaller`, `PendingList`, `PendingByID` |
| `internal/mcp/gate.go` | passes `ProfileID` into `ConfirmRequest` |
| `internal/agenthub/permission.go` | pending map keyed by id, one per session |
| `internal/acp/types_v1.go` | `McpServerStdio`, `EnvVariable` |
| `internal/sandbox/policy.go`, `seatbelt_profile.go`, `bwrap_args.go` | `AllowUnixSockets` |
| `internal/agenthub/adapter.go`, `adapter_mock.go`, `adapter_claude.go` | `Open(ctx, OpenSpec)` |
| `internal/agenthub/service.go`, `errors.go` | id-first creation, `MCPEndpoints`, `OnSessionClosed`, descendant / cwd / depth rules |
| `internal/mcp/server.go` | per-identity `*sdk.Server` + middleware; `register*` take `ms` |
| `internal/mcp/session_host.go` (new) | per-session sockets, sweep |
| `cmd/devkit-mcp/main.go` | `--socket` flag |
| `internal/bridge/errors.go`, `interaction.go`, `agenthub.go` | new sentinels, pending lists |
| `internal/app/container.go` | one new grant |
| `main.go` | wiring |
| `frontend/src/bridge/interaction.ts`, `agenthub.ts`, `stores/agentSessions.ts`, `workbench/InteractionHost.tsx`, `modules/agenthub/AgentHubView.tsx`, `workbench/AgentSessionsSubscriber.tsx` | queues + caller display |

---

### Task 1: `internal/identity` leaf package

**Files:**
- Create: `internal/identity/identity.go`
- Create: `internal/identity/identity_test.go`

- [x] **Step 1: Write the failing test**

```go
package identity

import (
	"context"
	"testing"
)

func TestCallerRoundTrip(t *testing.T) {
	ctx := WithCaller(context.Background(), Agent("11111111-2222-3333-4444-555555555555"))
	c, ok := FromContext(ctx)
	if !ok || !c.IsAgent() || c.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	if got := c.String(); got != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("String() = %q", got)
	}
}

func TestLabels(t *testing.T) {
	if got := Label(context.Background()); got != Unknown {
		t.Fatalf("empty ctx label = %q, want %q", got, Unknown)
	}
	if got := UserTerminal().String(); got != "user-terminal" {
		t.Fatalf("user-terminal label = %q", got)
	}
	if got := UI().String(); got != "ui" {
		t.Fatalf("ui label = %q", got)
	}
	if UI().IsAgent() || UserTerminal().IsAgent() {
		t.Fatal("only Kind agent with a session id is an agent")
	}
	if Agent("").IsAgent() {
		t.Fatal("agent without session id must not count as an agent")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/identity/ -run 'TestCallerRoundTrip|TestLabels' -v`
Expected: FAIL — `undefined: WithCaller` (package has no non-test file yet).

- [x] **Step 3: Write the implementation**

```go
// Package identity names who is calling DevKit. It is a leaf: it must not
// import any devkit/internal package, because capabilities, interaction,
// agenthub and mcp all depend on it.
package identity

import "context"

// Kind is the class of caller.
type Kind string

const (
	// KindAgent is a vendor agent spawned by Agent Hub; SessionID is its hub session.
	KindAgent Kind = "agent"
	// KindUserTerminal is a CLI the user ran themselves, connected through the shared mcp.sock.
	KindUserTerminal Kind = "user-terminal"
	// KindUI is the desktop frontend calling through the bridge.
	KindUI Kind = "ui"
)

// Unknown is the audit label for a context that carries no Caller.
const Unknown = "unknown"

// Caller identifies the origin of a request.
type Caller struct {
	Kind      Kind
	SessionID string // hub session UUID; set only for KindAgent
}

// Agent returns the caller for an Agent Hub session.
func Agent(sessionID string) Caller { return Caller{Kind: KindAgent, SessionID: sessionID} }

// UserTerminal returns the caller for the user's own terminal CLIs.
func UserTerminal() Caller { return Caller{Kind: KindUserTerminal} }

// UI returns the caller for the desktop frontend.
func UI() Caller { return Caller{Kind: KindUI} }

// IsAgent reports whether c is a hub agent (Kind agent with a session id).
func (c Caller) IsAgent() bool { return c.Kind == KindAgent && c.SessionID != "" }

// String is the stable audit / allow_always label: "agent:<id>", "user-terminal", "ui".
func (c Caller) String() string {
	switch {
	case c.Kind == KindAgent:
		return "agent:" + c.SessionID
	case c.Kind == "":
		return Unknown
	default:
		return string(c.Kind)
	}
}

type ctxKey struct{}

// WithCaller returns ctx carrying c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the caller stamped on ctx, if any.
func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(ctxKey{}).(Caller)
	return c, ok && c.Kind != ""
}

// Label returns the caller's String(), or Unknown when ctx carries none.
func Label(ctx context.Context) string {
	if c, ok := FromContext(ctx); ok {
		return c.String()
	}
	return Unknown
}
```

- [x] **Step 4: Run tests**

Run: `go test ./internal/identity/ -v && go test ./internal/arch/`
Expected: both `ok`. If `internal/arch` has a leaf-package allowlist test that now lists packages, `identity` imports only `context` and passes.

- [x] **Step 5: Commit**

```bash
git add internal/identity
git commit -m "feat(identity): leaf Caller type and context helpers"
```

---

### Task 2: Audit events carry `Caller` and `At`; gateway stamps them

**Files:**
- Modify: `internal/audit/events.go`
- Modify: `internal/capabilities/gateway.go`
- Create: `internal/capabilities/gateway_identity_test.go`

- [x] **Step 1: Write the failing test**

```go
package capabilities

import (
	"context"
	"testing"
	"time"

	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/permissions"
)

func identityTestGateway() (*Gateway, *audit.MemorySink) {
	sink := audit.NewMemorySink()
	policy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.test", Capability: "test:read", Scope: "thing"},
	})
	return NewGateway(policy, sink), sink
}

func TestGatewayStampsCallerAndTime(t *testing.T) {
	gw, sink := identityTestGateway()
	ctx := identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555"))
	before := time.Now().UTC().Add(-time.Second)
	if _, err := gw.Call(ctx, CallRequest{
		SubjectID: "builtin.test", Capability: "test:read", Scope: "thing",
		Handler: func(context.Context) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Caller != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("Caller = %q", events[0].Caller)
	}
	if events[0].At.Before(before) || events[0].At.IsZero() {
		t.Fatalf("At = %v, want a recent UTC time", events[0].At)
	}
}

func TestGatewayUnattributedCallIsAllowedAndLabelledUnknown(t *testing.T) {
	gw, sink := identityTestGateway()
	if _, err := gw.Call(context.Background(), CallRequest{
		SubjectID: "builtin.test", Capability: "test:read", Scope: "thing",
		Handler: func(context.Context) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatalf("unattributed internal calls must still pass policy: %v", err)
	}
	if got := sink.Events()[0].Caller; got != identity.Unknown {
		t.Fatalf("Caller = %q, want %q", got, identity.Unknown)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capabilities/ -run TestGateway -v`
Expected: FAIL — `events[0].Caller undefined`.

- [x] **Step 3: Add the fields to `audit.Event`**

In `internal/audit/events.go` change the import block and struct:

```go
import (
	"sync"
	"time"
)

type Event struct {
	// Caller is identity.Caller.String() of the requester ("agent:<id>",
	// "user-terminal", "ui") or identity.Unknown. Stamped by the gateway.
	Caller     string
	At         time.Time
	SubjectID  string
	Capability string
	Scope      string
	Outcome    Outcome
	Metadata   map[string]any
}
```

- [x] **Step 4: Stamp them in the gateway**

In `internal/capabilities/gateway.go` add imports `"time"` and `"devkit/internal/identity"`, and replace the `g.sink.Record(...)` call in `Call` with:

```go
	g.sink.Record(audit.Event{
		Caller:     identity.Label(ctx),
		At:         time.Now().UTC(),
		SubjectID:  request.SubjectID,
		Capability: request.Capability,
		Scope:      request.Scope,
		Outcome:    outcome,
		Metadata:   audit.RedactMap(request.Metadata),
	})
```

- [x] **Step 5: Run tests**

Run: `go test ./internal/capabilities/ ./internal/audit/ ./internal/bridge/ -count=1`
Expected: all `ok` (existing tests compare individual fields, not whole structs; if one does a struct equality on `audit.Event`, add `Caller: identity.Unknown` and ignore `At` by zeroing it in the test).

- [x] **Step 6: Commit**

```bash
git add internal/audit/events.go internal/capabilities
git commit -m "feat(audit): events carry caller and timestamp"
```

---

### Task 3: Bridge defaults unattributed calls to `ui`

**Files:**
- Modify: `internal/bridge/gateway.go`
- Create: `internal/bridge/caller_context_test.go`

- [x] **Step 1: Write the failing test**

```go
package bridge

import (
	"context"
	"testing"

	"devkit/internal/identity"
)

func TestCallerContextDefaultsToUI(t *testing.T) {
	c, ok := identity.FromContext(callerContext(context.Background()))
	if !ok || c.Kind != identity.KindUI {
		t.Fatalf("got %+v ok=%v, want ui", c, ok)
	}
}

func TestCallerContextKeepsExistingCaller(t *testing.T) {
	in := identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555"))
	c, _ := identity.FromContext(callerContext(in))
	if !c.IsAgent() {
		t.Fatalf("MCP-stamped agent caller must survive the bridge: %+v", c)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/ -run TestCallerContext -v`
Expected: FAIL — `undefined: callerContext`.

- [x] **Step 3: Implement**

In `internal/bridge/gateway.go` add import `"devkit/internal/identity"`, change the first line of `call` to wrap ctx, and add the helper:

```go
func (s *AppService) call(
	ctx context.Context,
	subject string,
	capability string,
	scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	ctx = callerContext(ctx)
	result, err := s.container.Capabilities.Call(ctx, capabilities.CallRequest{
```

```go
// callerContext defaults an unattributed call to the UI: bridge methods are
// invoked by the desktop frontend, while MCP tool handlers arrive with an
// agent / user-terminal caller already stamped by internal/mcp and keep it.
func callerContext(ctx context.Context) context.Context {
	if _, ok := identity.FromContext(ctx); ok {
		return ctx
	}
	return identity.WithCaller(ctx, identity.UI())
}
```

- [x] **Step 4: Run tests**

Run: `go test ./internal/bridge/ -count=1`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/bridge/gateway.go internal/bridge/caller_context_test.go
git commit -m "feat(bridge): stamp ui caller on unattributed calls"
```

---

### Task 4: Persistent audit — migration, `SQLSink`, `Tee`

**Files:**
- Modify: `internal/storage/sqlite.go` (const near line 30; migration after `m19`; runner list)
- Create: `internal/audit/tee.go`
- Create: `internal/audit/sqlsink.go`
- Create: `internal/audit/sqlsink_test.go`

- [x] **Step 1: Migration `m20`**

Add to the table-name const block in `internal/storage/sqlite.go`:

```go
	TableAuditEvents        = "audit_events"
```

After the `m19 := Migration{...}` literal add:

```go
	m20 := Migration{
		Version: 20,
		Name:    "create audit_events",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					at TEXT NOT NULL,
					caller TEXT NOT NULL,
					subject TEXT NOT NULL,
					capability TEXT NOT NULL,
					scope TEXT NOT NULL,
					outcome TEXT NOT NULL,
					metadata TEXT NOT NULL
				)`, TableAuditEvents,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_audit_events_at ON %s (at)`,
				TableAuditEvents,
			))
			return err
		},
	}
```

and append `m20` to the slice passed to `NewMigrationRunner` (`..., m18, m19, m20}`).

Run: `go test ./internal/storage/ -count=1`
Expected: `ok` (migration tests apply the full chain).

- [x] **Step 2: Write the failing sink tests**

```go
package audit_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/audit"
	"devkit/internal/storage"
)

func openAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + storage.TableAuditEvents).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSQLSinkWritesEveryEvent(t *testing.T) {
	db := openAuditDB(t)
	sink := audit.NewSQLSink(db, storage.TableAuditEvents, nil)
	const n = 5000 // several times the queue size: backpressure path must not drop
	for i := 0; i < n; i++ {
		sink.Record(audit.Event{Caller: "agent:x", SubjectID: "builtin.test", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed,
			Metadata: map[string]any{"password": "secret", "i": i}})
	}
	sink.Close()
	if got := countRows(t, db); got != n {
		t.Fatalf("rows = %d, want %d (events must never be dropped)", got, n)
	}
	var meta string
	if err := db.QueryRow(`SELECT metadata FROM ` + storage.TableAuditEvents + ` LIMIT 1`).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if meta == "" || containsSecret(meta) {
		t.Fatalf("metadata must be redacted JSON, got %q", meta)
	}
}

func containsSecret(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if s[i:i+6] == "secret" {
			return true
		}
	}
	return false
}

func TestSQLSinkFallsBackWhenWriteFails(t *testing.T) {
	db := openAuditDB(t)
	mem := audit.NewMemorySink()
	sink := audit.NewSQLSink(db, "no_such_table", mem)
	sink.Record(audit.Event{Caller: "ui", SubjectID: "builtin.test", Capability: "c", Scope: "s", Outcome: audit.OutcomeDenied})
	sink.Close()
	if got := len(mem.Events()); got != 1 {
		t.Fatalf("fallback events = %d, want 1", got)
	}
}

func TestSQLSinkPruneRemovesOldRows(t *testing.T) {
	db := openAuditDB(t)
	sink := audit.NewSQLSink(db, storage.TableAuditEvents, nil)
	sink.Record(audit.Event{At: time.Now().UTC().Add(-40 * 24 * time.Hour), Caller: "ui", SubjectID: "old", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	sink.Record(audit.Event{Caller: "ui", SubjectID: "new", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	sink.Close()
	if err := sink.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db); got != 1 {
		t.Fatalf("rows after prune = %d, want 1", got)
	}
}

func TestTeeFansOut(t *testing.T) {
	a, b := audit.NewMemorySink(), audit.NewMemorySink()
	audit.NewTee(a, b).Record(audit.Event{Caller: "ui", SubjectID: "x", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	if len(a.Events()) != 1 || len(b.Events()) != 1 {
		t.Fatalf("tee did not reach both sinks: %d/%d", len(a.Events()), len(b.Events()))
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/audit/ -run 'TestSQLSink|TestTee' -v`
Expected: FAIL — `undefined: audit.NewSQLSink`.

- [x] **Step 4: `tee.go`**

```go
package audit

type teeSink struct{ sinks []Sink }

// NewTee returns a Sink that records every event to each of sinks, in order.
func NewTee(sinks ...Sink) Sink { return teeSink{sinks: sinks} }

func (t teeSink) Record(event Event) {
	for _, s := range t.sinks {
		s.Record(event)
	}
}
```

- [x] **Step 5: `sqlsink.go`**

```go
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	sqlSinkQueue = 1024
	sqlSinkBatch = 256
	// DefaultRetention is how long audit rows are kept before Prune removes them.
	DefaultRetention = 30 * 24 * time.Hour
)

// SQLSink persists events to a SQLite table. Record never blocks on SQLite
// while the queue has room; when the queue is full it writes synchronously
// (backpressure) rather than dropping — an audit trail that loses events is
// not an audit trail. A failed SQLite write is a different case: those
// events go to the fallback sink (normally the in-memory one) and are logged.
type SQLSink struct {
	db        *sql.DB
	table     string
	fallback  Sink
	retention time.Duration
	queue     chan Event
	done      chan struct{}
	closeOnce sync.Once
}

// NewSQLSink starts the writer goroutine. table is the fully-qualified table
// name (storage.TableAuditEvents); fallback may be nil.
func NewSQLSink(db *sql.DB, table string, fallback Sink) *SQLSink {
	s := &SQLSink{
		db:        db,
		table:     table,
		fallback:  fallback,
		retention: DefaultRetention,
		queue:     make(chan Event, sqlSinkQueue),
		done:      make(chan struct{}),
	}
	go s.run()
	return s
}

// Record implements Sink.
func (s *SQLSink) Record(event Event) {
	event.Metadata = RedactMap(event.Metadata)
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	select {
	case s.queue <- event:
	default:
		s.write([]Event{event}) // queue full: backpressure, never drop
	}
}

func (s *SQLSink) run() {
	defer close(s.done)
	for event, ok := <-s.queue; ok; event, ok = <-s.queue {
		batch := []Event{event}
	drain:
		for len(batch) < sqlSinkBatch {
			select {
			case next, more := <-s.queue:
				if !more {
					break drain
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}
		s.write(batch)
	}
}

func (s *SQLSink) write(batch []Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(batch, err)
		return
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO `+s.table+
		` (at, caller, subject, capability, scope, outcome, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		s.fail(batch, err)
		return
	}
	defer stmt.Close()
	for _, e := range batch {
		meta, _ := json.Marshal(e.Metadata) // already redacted; unmarshalable values become null
		if _, err := stmt.ExecContext(ctx,
			e.At.UTC().Format(time.RFC3339Nano), e.Caller, e.SubjectID, e.Capability, e.Scope, string(e.Outcome), string(meta),
		); err != nil {
			_ = tx.Rollback()
			s.fail(batch, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.fail(batch, err)
	}
}

func (s *SQLSink) fail(batch []Event, err error) {
	log.Printf("audit: sqlite write failed, keeping %d event(s) in memory: %v", len(batch), err)
	if s.fallback == nil {
		return
	}
	for _, e := range batch {
		s.fallback.Record(e)
	}
}

// Prune deletes rows older than the retention window.
func (s *SQLSink) Prune(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-s.retention).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.table+` WHERE at < ?`, cutoff)
	return err
}

// PruneLoop runs Prune every interval until ctx is done.
func (s *SQLSink) PruneLoop(ctx context.Context, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Prune(ctx); err != nil {
					log.Printf("audit: prune: %v", err)
				}
			}
		}
	}()
}

// Close flushes the queue and stops the writer. Record must not be called after Close.
func (s *SQLSink) Close() {
	s.closeOnce.Do(func() { close(s.queue) })
	<-s.done
}
```

- [x] **Step 6: Run tests**

Run: `go test ./internal/audit/ -count=1 -v -run 'TestSQLSink|TestTee'`
Expected: all PASS; `TestSQLSinkWritesEveryEvent` proves 5000 in → 5000 rows with a 1024 queue.

- [x] **Step 7: Commit**

```bash
git add internal/storage/sqlite.go internal/audit
git commit -m "feat(audit): persistent SQLite sink with backpressure, fallback and prune"
```

---

### Task 5: `interaction.Hub` — pending map, caller, scoped `allow_always`

**Files:**
- Modify: `internal/interaction/hub.go`
- Modify: `internal/interaction/errors.go`
- Modify: `internal/mcp/gate.go`
- Modify: `internal/bridge/interaction.go`
- Create: `internal/interaction/hub_identity_test.go`

- [x] **Step 1: Write the failing tests**

```go
package interaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/identity"
)

func agentCtx(id string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(id))
}

func waitForPendingCount(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.PendingList()) == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("pending = %d, want %d", len(h.PendingList()), want)
}

func TestTwoCallersPendAtOnce(t *testing.T) {
	h := NewHub(NoopPrompter{})
	done := make(chan error, 2)
	for _, id := range []string{"aaaaaaaa-0000-0000-0000-000000000001", "aaaaaaaa-0000-0000-0000-000000000002"} {
		go func(id string) {
			done <- h.Confirm(agentCtx(id), ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1", Kind: KindDatabase})
		}(id)
	}
	waitForPendingCount(t, h, 2)
	list := h.PendingList()
	if list[0].Caller == "" || list[0].Caller == list[1].Caller {
		t.Fatalf("pending must carry distinct callers: %+v", list)
	}
	for _, p := range list {
		if err := h.Complete(p.ID, "allow"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("confirm %d: %v", i, err)
		}
	}
}

func TestSameCallerCannotPendTwice(t *testing.T) {
	h := NewHub(NoopPrompter{})
	ctx := agentCtx("aaaaaaaa-0000-0000-0000-000000000001")
	go func() { _ = h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1"}) }()
	waitForPendingCount(t, h, 1)
	err := h.Confirm(ctx, ConfirmRequest{Env: "dev", Tool: "ssh.runCommand", ProfileID: "p2"})
	if !errors.Is(err, ErrCallerBusy) {
		t.Fatalf("got %v, want ErrCallerBusy", err)
	}
	h.Complete(h.PendingList()[0].ID, "deny")
}

func TestAllowAlwaysIsScopedToCallerToolProfile(t *testing.T) {
	h := NewHub(NoopPrompter{})
	a := agentCtx("aaaaaaaa-0000-0000-0000-000000000001")
	b := agentCtx("aaaaaaaa-0000-0000-0000-000000000002")
	req := ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p1", Kind: KindDatabase}

	done := make(chan error, 1)
	go func() { done <- h.Confirm(a, req) }()
	waitForPendingCount(t, h, 1)
	if err := h.Complete(h.PendingList()[0].ID, "allow_always"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// Same caller, tool, profile: short-circuits.
	if err := h.Confirm(a, req); err != nil {
		t.Fatalf("allow_always for A should short-circuit: %v", err)
	}
	// Same caller, different profile: prompts again.
	go func() { done <- h.Confirm(a, ConfirmRequest{Env: "dev", Tool: "db.query", ProfileID: "p2"}) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	<-done
	// Other caller, same tool and profile: prompts again — never inherits A's grant.
	go func() { done <- h.Confirm(b, req) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	if err := <-done; !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("B must have been prompted and denied, got %v", err)
	}

	// Forgetting A removes its grant.
	h.ForgetCaller(identity.Agent("aaaaaaaa-0000-0000-0000-000000000001").String())
	go func() { done <- h.Confirm(a, req) }()
	waitForPendingCount(t, h, 1)
	h.Complete(h.PendingList()[0].ID, "deny")
	if err := <-done; !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("after ForgetCaller A must be prompted again, got %v", err)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/interaction/ -run 'TestTwoCallers|TestSameCaller|TestAllowAlwaysIsScoped' -v`
Expected: FAIL — `h.PendingList undefined`, `ErrCallerBusy undefined`.

- [x] **Step 3: Add the sentinel**

In `internal/interaction/errors.go` add:

```go
	ErrCallerBusy           = errors.New("caller already has a pending interaction")
```

- [x] **Step 4: Rewrite the hub's pending state**

Replace the corresponding parts of `internal/interaction/hub.go` (imports gain `"sort"` and `"devkit/internal/identity"`):

```go
type ConfirmRequest struct {
	Env     string
	Tool    string
	Summary string
	Kind    Kind
	// ProfileID scopes allow_always: a grant on one profile never covers another.
	ProfileID string
	// EnvBlocks is true when RequiresConfirmation(env) (incl. fail-closed).
	// When true, hub ignores allow_always cache and does not offer allow_always.
	EnvBlocks bool
}

type Pending struct {
	ID                 string    `json:"id"`
	Type               Type      `json:"type"`
	Kind               Kind      `json:"kind,omitempty"`
	Env                string    `json:"env,omitempty"`
	Tool               string    `json:"tool,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	AllowAlwaysOffered bool      `json:"allowAlwaysOffered"`
	Caller             string    `json:"caller,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

// allowKey scopes an allow_always grant: it never crosses callers, tools or profiles.
type allowKey struct{ caller, tool, profile string }

type Hub struct {
	Timeout  time.Duration
	Headless bool
	prompter Prompter

	mu          sync.Mutex
	pending     map[string]*pendingWait // by Pending.ID; at most one per caller
	allowAlways map[allowKey]struct{}   // cleared per caller by ForgetCaller, fully on restart
}

func NewHub(p Prompter) *Hub {
	return &Hub{
		Timeout:     DefaultTimeout,
		prompter:    p,
		pending:     map[string]*pendingWait{},
		allowAlways: map[allowKey]struct{}{},
	}
}

// PendingList returns every pending interaction, oldest first.
func (h *Hub) PendingList() []Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Pending, 0, len(h.pending))
	for _, w := range h.pending {
		out = append(out, w.Pending)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest pending interaction, or nil. Kept for callers
// that only ever showed one; new code should use PendingList.
func (h *Hub) Pending() *Pending {
	list := h.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

// PendingByID returns one pending interaction, or nil.
func (h *Hub) PendingByID(id string) *Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.pending[id]
	if !ok {
		return nil
	}
	cp := w.Pending
	return &cp
}

func (h *Hub) Complete(id, value string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.pending) == 0 {
		return ErrNoPendingInteraction
	}
	w, ok := h.pending[id]
	if !ok || w.finished {
		return ErrInvalidInteractionID
	}
	select {
	case w.result <- value:
		w.finished = true
		return nil
	default:
		return ErrNoPendingInteraction
	}
}

// ForgetCaller drops every allow_always grant held by caller (a closed agent session).
func (h *Hub) ForgetCaller(caller string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key := range h.allowAlways {
		if key.caller == caller {
			delete(h.allowAlways, key)
		}
	}
}

func (h *Hub) invalidatePending(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.pending[id]; ok {
		w.finished = true
		delete(h.pending, id)
	}
}
```

Replace `Confirm` so the cache is keyed per caller:

```go
func (h *Hub) Confirm(ctx context.Context, req ConfirmRequest) error {
	if h.Headless || h.prompter == nil {
		return ErrConfirmationRequired
	}
	key := allowKey{caller: identity.Label(ctx), tool: req.Tool, profile: req.ProfileID}
	// Env block always wins: never short-circuit on allow_always.
	if !req.EnvBlocks {
		h.mu.Lock()
		_, ok := h.allowAlways[key]
		h.mu.Unlock()
		if ok {
			return nil
		}
	}
	_, err := h.wait(ctx, Pending{
		Type: TypeConfirm, Kind: req.Kind, Env: req.Env, Tool: req.Tool, Summary: req.Summary,
		AllowAlwaysOffered: !req.EnvBlocks,
	}, func(v string) (string, error) {
		switch v {
		case "allow":
			return v, nil
		case "allow_always":
			if req.EnvBlocks {
				// UI should not send this when EnvBlocks; treat as single allow.
				return "allow", nil
			}
			h.mu.Lock()
			h.allowAlways[key] = struct{}{}
			h.mu.Unlock()
			return v, nil
		default:
			return "", ErrConfirmationDenied
		}
	})
	return err
}
```

Replace `wait` so it registers into the map and refuses a second pending for the same caller:

```go
func (h *Hub) wait(ctx context.Context, p Pending, mapResult func(string) (string, error)) (string, error) {
	id, err := vaultitem.NewID()
	if err != nil {
		return "", err
	}
	p.ID = id
	p.Caller = identity.Label(ctx)
	p.CreatedAt = time.Now().UTC()
	resCh := make(chan string, 1)

	h.mu.Lock()
	for _, w := range h.pending {
		if w.Caller == p.Caller {
			h.mu.Unlock()
			return "", ErrCallerBusy
		}
	}
	h.pending[id] = &pendingWait{Pending: p, result: resCh}
	h.mu.Unlock()

	defer h.invalidatePending(id)

	if err := h.prompter.Prompt(p); err != nil {
		return "", err
	}

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", ErrInteractionTimeout
	case v := <-resCh:
		return mapResult(v)
	}
}
```

`SelectProfile` is unchanged (it goes through `wait`).

- [x] **Step 5: Pass `ProfileID` from the gate**

In `internal/mcp/gate.go`, `GateActiveMCP`, add `ProfileID: id,` to the `interaction.ConfirmRequest{...}` literal.

- [x] **Step 6: Bridge exposes the list and denies by id**

In `internal/bridge/interaction.go` add after `InteractionPending`:

```go
// InteractionPendingList returns every pending interaction, oldest first,
// each tagged with its caller. InteractionHost renders this as a queue.
func (s *AppService) InteractionPendingList() ([]interaction.Pending, error) {
	result, err := s.call(context.Background(), "builtin.interaction", "interaction:respond", "pending", map[string]any{
		"action": "pending_list",
	}, func(context.Context) (any, error) {
		hub, err := s.interactionHub()
		if err != nil {
			return nil, err
		}
		return hub.PendingList(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]interaction.Pending), nil
}
```

and in `InteractionDeny` replace the `p := hub.Pending()` / `p.ID != id` block with:

```go
		p := hub.PendingByID(id)
		if p == nil {
			return nil, interaction.ErrInvalidInteractionID
		}
```

- [x] **Step 7: Run tests**

Run: `go test ./internal/interaction/ ./internal/mcp/ ./internal/bridge/ -count=1`
Expected: all `ok`. Existing `hub_test.go` still passes: `Pending()` returns the oldest, and single-caller flows are unchanged.

- [x] **Step 8: Commit**

```bash
git add internal/interaction internal/mcp/gate.go internal/bridge/interaction.go
git commit -m "feat(interaction): per-caller pending queue and scoped allow_always"
```

---

### Task 6: `PermissionWaiter` — one pending per session, many sessions

**Files:**
- Modify: `internal/agenthub/permission.go`
- Create: `internal/agenthub/permission_queue_test.go`

- [x] **Step 1: Write the failing test**

```go
package agenthub

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPermissionWaiterQueuesTwoSessions(t *testing.T) {
	w := NewPermissionWaiter(nil)
	params := json.RawMessage(`{"toolCall":{}}`)
	results := make(chan error, 2)
	for _, sid := range []string{"s1", "s2"} {
		go func(sid string) {
			_, err := w.Wait(context.Background(), sid, params)
			results <- err
		}(sid)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(w.PendingList()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want 2 — second session must queue, not be denied", len(w.PendingList()))
		}
		time.Sleep(2 * time.Millisecond)
	}
	for _, p := range w.PendingList() {
		if err := w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agenthub/ -run TestPermissionWaiterQueuesTwoSessions -v`
Expected: FAIL — `w.PendingList undefined`.

- [x] **Step 3: Implement**

In `internal/agenthub/permission.go` (imports gain `"sort"`):

```go
// PendingPermission is a permission prompt awaiting UI resolution.
type PendingPermission struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"createdAt"`
}

type PermissionWaiter struct {
	notify func(PendingPermission)

	mu      sync.Mutex
	pending map[string]*permissionWait // by ID; at most one per SessionID
}

func NewPermissionWaiter(notify func(PendingPermission)) *PermissionWaiter {
	return &PermissionWaiter{notify: notify, pending: map[string]*permissionWait{}}
}
```

Replace the registration block inside `Wait`:

```go
	p := PendingPermission{
		ID:        id,
		SessionID: sessionID,
		Params:    append(json.RawMessage(nil), params...),
		CreatedAt: time.Now().UTC(),
	}

	w.mu.Lock()
	for _, other := range w.pending {
		if other.SessionID == sessionID {
			// A session prompts synchronously; a second prompt from the same
			// session is a protocol violation, not a queue case.
			w.mu.Unlock()
			return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
		}
	}
	w.pending[id] = &permissionWait{PendingPermission: p, result: resCh}
	w.mu.Unlock()

	defer w.invalidate(id)
```

(remove the old `defer func() { w.pending = nil }()`). Replace `Complete`, `Pending`, add `PendingList`, and rewrite `invalidate`:

```go
// Complete resolves a pending permission prompt with an ACP outcome JSON body.
func (w *PermissionWaiter) Complete(id string, outcome json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return errors.New("agenthub: no pending permission")
	}
	p, ok := w.pending[id]
	if !ok || p.finished {
		return errors.New("agenthub: invalid permission id")
	}
	select {
	case p.result <- append(json.RawMessage(nil), outcome...):
		p.finished = true
		return nil
	default:
		return errors.New("agenthub: no pending permission")
	}
}

// PendingList returns every pending prompt, oldest first.
func (w *PermissionWaiter) PendingList() []PendingPermission {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]PendingPermission, 0, len(w.pending))
	for _, p := range w.pending {
		cp := p.PendingPermission
		cp.Params = append(json.RawMessage(nil), cp.Params...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest pending prompt, or nil.
func (w *PermissionWaiter) Pending() *PendingPermission {
	list := w.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

func (w *PermissionWaiter) invalidate(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p, ok := w.pending[id]; ok {
		p.finished = true
		delete(w.pending, id)
	}
}
```

- [x] **Step 4: Run tests**

Run: `go test ./internal/agenthub/ -count=1 -run 'Permission'`
Expected: `ok` — existing `permission_test.go` flows are single-session.

- [x] **Step 5: Commit**

```bash
git add internal/agenthub/permission.go internal/agenthub/permission_queue_test.go
git commit -m "feat(agenthub): permission waiter queues prompts per session"
```

---

### Task 7: ACP `McpServerStdio` type

**Files:**
- Modify: `internal/acp/types_v1.go`
- Modify: `internal/acp/types_v1_test.go` (append)

- [x] **Step 1: Write the failing test** (append to `types_v1_test.go`)

```go
func TestMcpServerStdioWire(t *testing.T) {
	raw, err := json.Marshal(McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/a.sock"}, Env: []EnvVariable{}})
	if err != nil {
		t.Fatal(err)
	}
	// Schema v1 McpServerStdio: required name, command, args, env; no "type" field.
	want := `{"name":"devkit","command":"/opt/devkit-mcp","args":["--socket","/tmp/a.sock"],"env":[]}`
	if string(raw) != want {
		t.Fatalf("wire = %s\nwant %s", raw, want)
	}
}
```

Check the file's imports include `"encoding/json"` (add if missing).

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/acp/ -run TestMcpServerStdioWire -v`
Expected: FAIL — `undefined: McpServerStdio`.

- [x] **Step 3: Implement** (append to `types_v1.go`)

```go
// EnvVariable is one environment variable passed to an MCP server (schema: EnvVariable).
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McpServerStdio is the stdio variant of McpServer for session/new.mcpServers
// (schema: McpServerStdio). The stdio variant carries no "type" discriminator,
// and every field is required — build Env as an empty slice, never nil, or
// it serialises as null and the agent rejects the entry.
type McpServerStdio struct {
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env"`
}
```

- [x] **Step 4: Run tests and commit**

Run: `go test ./internal/acp/ -count=1`
Expected: `ok`.

```bash
git add internal/acp
git commit -m "feat(acp): typed stdio MCP server for session/new"
```

---

### Task 8: `sandbox.Policy.AllowUnixSockets`

**Files:**
- Modify: `internal/sandbox/policy.go`
- Modify: `internal/sandbox/seatbelt_profile.go`
- Modify: `internal/sandbox/bwrap_args.go`
- Modify: `internal/sandbox/seatbelt_profile_test.go` (append)
- Modify: `internal/sandbox/backend_seatbelt_test.go` (append)
- Create: `internal/sandbox/bwrap_args_sockets_test.go`

- [x] **Step 1: Profile test (append to `seatbelt_profile_test.go`)**

```go
func TestSeatbeltProfileAllowUnixSocketsIsResolvedAndAfterNetworkDeny(t *testing.T) {
	// /tmp resolves to /private/tmp on macOS; Seatbelt matches the resolved
	// path, so the emitted literal must be the resolved one.
	dir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	resolved, _ := filepath.EvalSymlinks(sock)

	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone, AllowUnixSockets: []string{sock}}.withDefaults())
	want := `(allow network-outbound (literal "` + resolved + `"))`
	if !strings.Contains(prof, want) {
		t.Fatalf("expected %s in profile:\n%s", want, prof)
	}
	if strings.Index(prof, want) < strings.Index(prof, "(deny network-outbound)") {
		t.Fatalf("socket allow must come after the NetworkNone deny (later rule wins):\n%s", prof)
	}
}
```

Add `"net"`, `"os"`, `"path/filepath"` to that test file's imports.

- [x] **Step 2: Live test (append to `backend_seatbelt_test.go`)**

```go
// TestSeatbeltAllowUnixSocketsConnects: the identity-bound MCP socket must be
// reachable even under NetworkNone, and a sibling socket must not be.
func TestSeatbeltAllowUnixSocketsConnects(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}
	sockDir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	serve := func(name, marker string) string {
		path := filepath.Join(sockDir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(marker)) })}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
		return path
	}
	allowed := serve("a.sock", "ALLOWED_SOCKET")
	sibling := serve("b.sock", "SIBLING_SOCKET")

	curl := func(sock string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		proc, err := backend.Start(ctx, Policy{WorkDir: t.TempDir(), Network: NetworkNone, AllowUnixSockets: []string{allowed}},
			Cmd{Path: "/usr/bin/curl", Args: []string{"-s", "-m", "5", "--unix-socket", sock, "http://localhost/"}})
		if err != nil {
			t.Fatal(err)
		}
		_ = proc.Wait()
		out, _ := proc.Output()
		return string(out)
	}
	if got := curl(allowed); !strings.Contains(got, "ALLOWED_SOCKET") {
		t.Fatalf("allowed socket unreachable from inside the jail: %q", got)
	}
	if got := curl(sibling); strings.Contains(got, "SIBLING_SOCKET") {
		t.Fatalf("sibling socket must stay unreachable: %q", got)
	}
}
```

- [x] **Step 3: Run both to verify they fail**

Run: `go test ./internal/sandbox/ -run 'AllowUnixSockets' -v`
Expected: FAIL — `unknown field AllowUnixSockets in struct literal`.

- [x] **Step 4: Policy**

In `internal/sandbox/policy.go` add the field:

```go
type Policy struct {
	WorkDir                 string
	ReadOnlyPaths           []string
	ReadWritePaths          []string // extra RW beyond WorkDir
	DenyReadPaths           []string // added on top of defaults
	// AllowUnixSockets are unix socket paths the process may connect to —
	// the session's identity-bound DevKit MCP endpoint. Everything else stays
	// closed (NetworkHost is IP-only).
	AllowUnixSockets        []string
	Env                     map[string]string
	Network                 NetworkMode // default none
	PrivateHome             *bool       // nil defaults to true
	DangerousAllowSensitive bool        // default false; test-only escape
}
```

and in `Validate`, before `validateBindPaths`:

```go
	for _, sock := range p.AllowUnixSockets {
		if !filepath.IsAbs(sock) {
			return ErrInvalidPolicy
		}
	}
```

and in `validateBindPaths` extend the loop's slice to include sockets:

```go
	for _, path := range append(append(append([]string{}, p.ReadWritePaths...), p.ReadOnlyPaths...), p.AllowUnixSockets...) {
```

- [x] **Step 5: Seatbelt**

In `seatbelt_profile.go`, after the `switch p.Network { ... }` block and before `denyPaths := p.EffectiveDenyRead()`:

```go
	// The session's own MCP socket. Independent of NetworkMode (local IPC,
	// not network) and emitted after the NetworkNone deny because the later
	// rule wins. Seatbelt matches resolved paths — /tmp/x.sock is
	// /private/tmp/x.sock to the kernel — so resolve before emitting; with
	// the resolved literal the connect right alone suffices (verified live).
	for _, sock := range p.AllowUnixSockets {
		b.WriteString("(allow network-outbound (literal \"" + escapeSBPL(resolveDenyPath(sock)) + "\"))\n")
	}
```

- [x] **Step 6: bubblewrap**

In `bwrap_args.go`, after the `if p.EffectivePrivateHome() && home != "" { ... }` block (the socket lives under the config dir in `$HOME`, which the tmpfs just hid):

```go
	for _, sock := range p.AllowUnixSockets {
		if pathExists(sock) {
			args = append(args, "--bind", sock, sock)
		}
	}
```

Create `internal/sandbox/bwrap_args_sockets_test.go`:

```go
//go:build linux

package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestBwrapBindsAllowedUnixSocketAfterTmpfsHome(t *testing.T) {
	home := t.TempDir()
	sock := filepath.Join(home, ".config", "DevKit", "agents", "s.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	args := buildBwrapArgs(Policy{WorkDir: t.TempDir(), Env: map[string]string{"HOME": home}, AllowUnixSockets: []string{sock}}, Cmd{Path: "/bin/true"})
	tmpfsAt, bindAt := -1, -1
	for i := range args {
		if args[i] == "--tmpfs" && i+1 < len(args) && args[i+1] == home {
			tmpfsAt = i
		}
		if args[i] == "--bind" && i+2 < len(args) && args[i+1] == sock && args[i+2] == sock {
			bindAt = i
		}
	}
	if tmpfsAt < 0 || bindAt < 0 || bindAt < tmpfsAt {
		t.Fatalf("socket bind must follow the home tmpfs: tmpfs@%d bind@%d in %v", tmpfsAt, bindAt, args)
	}
}
```

- [x] **Step 7: Run tests**

Run: `go test ./internal/sandbox/ -count=1 -v -run 'AllowUnixSockets|Seatbelt|Policy' && go vet ./internal/sandbox/`
Expected: all PASS (linux test skipped by build tag on macOS).

- [x] **Step 8: Commit**

```bash
git add internal/sandbox
git commit -m "feat(sandbox): AllowUnixSockets for identity-bound MCP endpoints"
```

---

### Task 9: `VendorAdapter.Open(ctx, OpenSpec)`

**Files:**
- Modify: `internal/agenthub/adapter.go`
- Modify: `internal/agenthub/adapter_mock.go`
- Modify: `internal/agenthub/adapter_claude.go`
- Modify: `internal/agenthub/adapter_claude_test.go`, `adapter_claude_integration_test.go`, `adapter_mock_test.go` (call sites)

- [x] **Step 1: Write the failing test** (append to `adapter_claude_test.go`)

```go
func TestClaudeSandboxPolicyCarriesSpec(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(t.TempDir(), "devkit-mcp")
	if err := os.WriteFile(proxy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	sock := filepath.Join(t.TempDir(), "s.sock")
	a := NewClaudeAdapter(ClaudeOptions{Command: bin})
	pol, _, err := a.buildSandbox(OpenSpec{Cwd: proj, AllowUnixSockets: []string{sock}, ReadOnlyPaths: []string{proxy}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pol.AllowUnixSockets) != 1 || pol.AllowUnixSockets[0] != sock {
		t.Fatalf("AllowUnixSockets = %v", pol.AllowUnixSockets)
	}
	found := false
	for _, p := range pol.ReadOnlyPaths {
		if p == proxy {
			found = true
		}
	}
	if !found {
		t.Fatalf("proxy must be readable inside the jail, ReadOnlyPaths = %v", pol.ReadOnlyPaths)
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `go test ./internal/agenthub/ -run TestClaudeSandboxPolicyCarriesSpec -v`
Expected: FAIL — `undefined: OpenSpec`.

- [x] **Step 3: Interface**

In `internal/agenthub/adapter.go` add import `"devkit/internal/acp"` and:

```go
// OpenSpec is everything an adapter needs to spawn one session.
type OpenSpec struct {
	Cwd string
	// AllowUnixSockets is the session's identity-bound MCP socket (see sandbox.Policy).
	AllowUnixSockets []string
	// McpServers is advertised to the agent via ACP session/new.mcpServers.
	McpServers []acp.McpServerStdio
	// ReadOnlyPaths are extra RO binds — the devkit-mcp proxy binary.
	ReadOnlyPaths []string
}

// VendorAdapter wraps a concrete agent vendor (mock, claude, …).
// Only adapters parse vendor-specific _meta shapes.
type VendorAdapter interface {
	Type() AgentType
	Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error)
	NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error)
	NormalizeToolSummary(meta json.RawMessage, update json.RawMessage) (filesChanged []string, summary string, err error)
	Extensions() []string
}
```

- [x] **Step 4: Mock adapter records the spec**

In `adapter_mock.go`:

```go
// MockAdapter is an in-process VendorAdapter (no sandbox, no real CLI).
type MockAdapter struct {
	mu       sync.Mutex
	lastSpec OpenSpec
}

func (a *MockAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.lastSpec = spec
	a.mu.Unlock()
	id := fmt.Sprintf("mock-acp-%d", mockSessionSeq.Add(1))
	return &mockRuntime{acpSessionID: id}, nil
}

// LastSpec returns the spec of the most recent Open (tests).
func (a *MockAdapter) LastSpec() OpenSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastSpec
}
```

- [x] **Step 5: Claude adapter**

In `adapter_claude.go` change `buildSandbox` and `Open`:

```go
// buildSandbox resolves the agent binary and returns the jail Policy + Cmd.
// Exported for unit tests via same-package access.
func (a *ClaudeAdapter) buildSandbox(spec OpenSpec) (sandbox.Policy, sandbox.Cmd, error) {
	workDir, err := filepath.Abs(spec.Cwd)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		return sandbox.Policy{}, sandbox.Cmd{}, fmt.Errorf("%w: project path", ErrProcessFailed)
	}

	path, args, roPaths, err := a.resolveCommand()
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	roPaths = uniqueAbsPaths(append(roPaths, spec.ReadOnlyPaths...)...)

	env := claudeEnvAllowlist()
	pol := sandbox.Policy{
		WorkDir:          workDir,
		Network:          sandbox.NetworkHost,
		Env:              env,
		ReadOnlyPaths:    roPaths,
		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
	}
	cmd := sandbox.Cmd{
		Path: path,
		Args: args,
	}
	return pol, cmd, nil
}
```

```go
func (a *ClaudeAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pol, cmd, err := a.buildSandbox(spec)
	if err != nil {
		return nil, err
	}
```

and the `session/new` call:

```go
	mcpServers := make([]any, 0, len(spec.McpServers))
	for _, srv := range spec.McpServers {
		if srv.Env == nil {
			srv.Env = []acp.EnvVariable{} // schema requires env; nil would serialise as null
		}
		mcpServers = append(mcpServers, srv)
	}
	var newRes acp.SessionNewResult
	if err := conn.Call(ctx, "session/new", acp.SessionNewRequest{
		Cwd:        pol.WorkDir,
		McpServers: mcpServers,
	}, &newRes); err != nil {
```

- [x] **Step 6: Update call sites**

In `adapter_claude_test.go`: `a.buildSandbox(proj)` → `a.buildSandbox(OpenSpec{Cwd: proj})`; `a.buildSandbox(t.TempDir())` → `a.buildSandbox(OpenSpec{Cwd: t.TempDir()})`; `a.Open(context.Background(), t.TempDir())` → `a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})`.
In `adapter_claude_integration_test.go`: `a.Open(context.Background(), proj)` → `a.Open(context.Background(), agenthub.OpenSpec{Cwd: proj})`.
In `adapter_mock_test.go`: every `Open(ctx, <path>)` → `Open(ctx, agenthub.OpenSpec{Cwd: <path>})` (or `OpenSpec{...}` if the test is in package `agenthub`).
`service.go` still calls `adapter.Open(ctx, in.Cwd)` — it is rewritten in Task 10; to keep this task compiling, change that one line now to `adapter.Open(ctx, OpenSpec{Cwd: in.Cwd})`.

- [x] **Step 7: Run tests and commit**

Run: `go build ./... 2>&1 | grep -v "build/ios" ; go test ./internal/agenthub/ -count=1`
Expected: `ok` (the pre-existing `devkit/build/ios` "function main is undeclared" is unrelated and ignored).

```bash
git add internal/agenthub
git commit -m "feat(agenthub): adapters open sessions from an OpenSpec"
```

---

### Task 10: `agenthub.Service` — identity rules and endpoints

**Files:**
- Modify: `internal/agenthub/errors.go`
- Modify: `internal/agenthub/service.go`
- Create: `internal/agenthub/service_identity_test.go`

- [x] **Step 1: Sentinels**

Append to the `var (...)` block in `errors.go`:

```go
	ErrNotDescendant      = errors.New("agenthub.notDescendant")
	ErrCwdOutsideParent   = errors.New("agenthub.cwdOutsideParent")
	ErrInvalidCwd         = errors.New("agenthub.invalidCwd")
	ErrCallerMismatch     = errors.New("agenthub.callerMismatch")
	ErrProxyNotFound      = errors.New("agenthub.proxyNotFound")
```

- [x] **Step 2: Write the failing tests**

```go
package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

type fakeEndpoints struct{ opened, closed []string }

func (f *fakeEndpoints) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	f.opened = append(f.opened, sessionID)
	return agenthub.MCPEndpoint{
		SocketPath: "/tmp/" + sessionID[:8] + ".sock",
		Server:     acp.McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/x.sock"}, Env: []acp.EnvVariable{}},
		Close:      func() { f.closed = append(f.closed, sessionID) },
	}, nil
}

func newIdentityService(t *testing.T) (*agenthub.Service, *agenthub.MockAdapter, *fakeEndpoints) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	mock := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(mock)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	eps := &fakeEndpoints{}
	svc.MCPEndpoints = eps
	return svc, mock, eps
}

func agentCtx(id string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(id))
}

func uiCtx() context.Context { return identity.WithCaller(context.Background(), identity.UI()) }

func projectDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateSessionOpensEndpointAndPassesSpec(t *testing.T) {
	svc, mock, eps := newIdentityService(t)
	sess, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(eps.opened) != 1 || eps.opened[0] != sess.ID {
		t.Fatalf("endpoint opened for %v, want [%s]", eps.opened, sess.ID)
	}
	spec := mock.LastSpec()
	if len(spec.McpServers) != 1 || spec.McpServers[0].Name != "devkit" {
		t.Fatalf("spec.McpServers = %+v", spec.McpServers)
	}
	if len(spec.AllowUnixSockets) != 1 || len(spec.ReadOnlyPaths) != 1 || spec.ReadOnlyPaths[0] != "/opt/devkit-mcp" {
		t.Fatalf("spec = %+v", spec)
	}
	if err := svc.CloseSession(uiCtx(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if len(eps.closed) != 1 {
		t.Fatalf("endpoint must close with the session, closed = %v", eps.closed)
	}
}

func TestAgentCallerCreatesChildUnderItself(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	root := projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentSessionID == nil || *child.ParentSessionID != parent.ID || child.Depth != 1 {
		t.Fatalf("child = %+v", child)
	}
	if child.ProjectPath != root {
		t.Fatalf("empty cwd must inherit the parent's project, got %q", child.ProjectPath)
	}
	// Lying about the parent is rejected, not silently corrected.
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, ParentSessionID: child.ID})
	if !errors.Is(err, agenthub.ErrCallerMismatch) {
		t.Fatalf("got %v, want ErrCallerMismatch", err)
	}
}

func TestDepthIsEnforcedFromIdentity(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	cur, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < agenthub.MaxDepth; i++ {
		next, err := svc.CreateSession(agentCtx(cur.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
		if err != nil {
			t.Fatalf("depth %d: %v", i+1, err)
		}
		cur = next
	}
	_, err = svc.CreateSession(agentCtx(cur.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if !errors.Is(err, agenthub.ErrDepthLimitExceeded) {
		t.Fatalf("got %v, want ErrDepthLimitExceeded (no callerSessionId needed to enforce)", err)
	}
}

func TestAgentCwdMustStayInsideParent(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	root := projectDir(t)
	inside := filepath.Join(root, "svc-a")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: inside}); err != nil {
		t.Fatalf("subpath of parent must be allowed: %v", err)
	}
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if !errors.Is(err, agenthub.ErrCwdOutsideParent) {
		t.Fatalf("got %v, want ErrCwdOutsideParent", err)
	}
}

func TestRootAndHomeAreNeverProjects(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	home, _ := os.UserHomeDir()
	for _, cwd := range []string{"/", home} {
		_, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
		if !errors.Is(err, agenthub.ErrInvalidCwd) {
			t.Fatalf("cwd %q: got %v, want ErrInvalidCwd", cwd, err)
		}
	}
}

func TestAgentOnlyReachesItsSubtree(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	a, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	b, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	aChild, _ := svc.CreateSession(agentCtx(a.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})

	if _, err := svc.GetStatus(agentCtx(a.ID), aChild.ID); err != nil {
		t.Fatalf("own child must be visible: %v", err)
	}
	if _, err := svc.GetStatus(agentCtx(a.ID), b.ID); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("sibling tree: got %v, want ErrNotDescendant", err)
	}
	if err := svc.Cancel(agentCtx(a.ID), b.ID); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("cancel sibling tree: got %v, want ErrNotDescendant", err)
	}
	if _, err := svc.Invoke(agentCtx(a.ID), agenthub.InvokeInput{SessionID: b.ID, Prompt: "hi"}); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("invoke sibling tree: got %v, want ErrNotDescendant", err)
	}
	list, err := svc.ListSessions(agentCtx(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("agent list = %d sessions, want 2 (self + child)", len(list))
	}
	if all, _ := svc.ListSessions(uiCtx()); len(all) != 3 {
		t.Fatalf("ui list = %d, want 3", len(all))
	}
}

func TestOnSessionClosedHookFires(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	var closed []string
	svc.OnSessionClosed = func(id string) { closed = append(closed, id) }
	s, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err := svc.CloseSession(uiCtx(), s.ID); err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0] != s.ID {
		t.Fatalf("hook got %v", closed)
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/agenthub/ -run 'TestCreateSessionOpensEndpoint|TestAgentCaller|TestDepthIsEnforced|TestAgentCwd|TestRootAndHome|TestAgentOnlyReaches|TestOnSessionClosed' -v`
Expected: FAIL — `undefined: agenthub.MCPEndpoint`.

- [x] **Step 4: Service types and fields**

In `service.go` add imports `"os"`, `"path/filepath"`, `"strings"`, `"devkit/internal/acp"`, `"devkit/internal/identity"`, and add after `InvokeResult`:

```go
// MCPEndpoint is the identity-bound MCP socket handed to one vendor process.
type MCPEndpoint struct {
	SocketPath string
	Server     acp.McpServerStdio
	Close      func()
}

// MCPEndpoints opens one MCPEndpoint per session. nil means sessions get no
// DevKit MCP server (tests only; production wires mcp.SessionHost).
type MCPEndpoints interface {
	Open(sessionID string) (MCPEndpoint, error)
}
```

Extend `Service`:

```go
type Service struct {
	reg               *Registry
	store             Store
	runtimes          map[string]SessionRuntime
	endpoints         map[string]func() // per-session MCP socket close
	invoking          map[string]struct{}
	mu                sync.Mutex
	emit              func(sessionID string, update json.RawMessage)
	ResolvePermission PermissionResolver // nil = fail closed (no auto-approve)
	PermissionTimeout time.Duration      // 0 = interaction.DefaultTimeout (60s)
	// MCPEndpoints opens the per-session MCP socket before the vendor spawns.
	MCPEndpoints MCPEndpoints
	// OnSessionClosed fires after a session is closed (interaction.Hub forgets its allow_always).
	OnSessionClosed func(sessionID string)
}
```

and initialise `endpoints: make(map[string]func())` in `NewService`.

- [x] **Step 5: `CreateSession`**

Replace the whole function:

```go
// CreateSession opens a vendor runtime and persists an idle hub session.
//
// Order matters: the session id is generated first because the MCP socket is
// named after it and the sandbox must know that path before the vendor spawns.
func (s *Service) CreateSession(ctx context.Context, in CreateInput) (AgentSession, error) {
	adapter, ok := s.reg.Get(in.AgentType)
	if !ok {
		return AgentSession{}, ErrVendorUnknown
	}

	caller, _ := identity.FromContext(ctx)
	parentID := in.ParentSessionID
	if caller.IsAgent() {
		// An agent never chooses its parent: the parent is the caller.
		if parentID != "" && parentID != caller.SessionID {
			return AgentSession{}, ErrCallerMismatch
		}
		parentID = caller.SessionID
	}

	depth := 0
	var parent *AgentSession
	if parentID != "" {
		p, err := s.store.GetSession(ctx, parentID)
		if err != nil {
			return AgentSession{}, err
		}
		if err := CheckDepth(p.Depth, MaxDepth); err != nil {
			return AgentSession{}, err
		}
		depth = p.Depth + 1
		parent = &p
	}

	cwd, err := resolveCwd(in.Cwd, parent, caller.IsAgent())
	if err != nil {
		return AgentSession{}, err
	}

	id := uuid.NewString()
	spec := OpenSpec{Cwd: cwd}
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(id)
		if err != nil {
			return AgentSession{}, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}

	rt, err := adapter.Open(ctx, spec)
	if err != nil {
		closeEndpoint()
		return AgentSession{}, err
	}

	now := time.Now().UTC()
	sess := AgentSession{
		ID:           id,
		AgentType:    in.AgentType,
		ProjectPath:  cwd,
		Status:       SessionStatusIdle,
		Depth:        depth,
		Title:        in.Title,
		ACPSessionID: rt.ACPSessionID(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if parentID != "" {
		pid := parentID
		sess.ParentSessionID = &pid
	}

	if err := s.store.InsertSession(ctx, sess); err != nil {
		_ = rt.Close(ctx)
		closeEndpoint()
		return AgentSession{}, err
	}

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.mu.Unlock()

	return sess, nil
}

// resolveCwd applies the cwd rules. Nobody gets "/" or the home directory.
// An agent caller with no cwd inherits its parent's project; with one, it
// must stay inside the parent's project (symlinks resolved on both sides).
func resolveCwd(cwd string, parent *AgentSession, agentCaller bool) (string, error) {
	if agentCaller && cwd == "" && parent != nil {
		cwd = parent.ProjectPath
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", ErrInvalidCwd
	}
	clean := filepath.Clean(cwd)
	if clean == "/" {
		return "", ErrInvalidCwd
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "", ErrInvalidCwd
	}
	if agentCaller && parent != nil {
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return "", ErrInvalidCwd
		}
		parentResolved, err := filepath.EvalSymlinks(parent.ProjectPath)
		if err != nil {
			return "", ErrInvalidCwd
		}
		rel, err := filepath.Rel(parentResolved, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", ErrCwdOutsideParent
		}
	}
	return clean, nil
}
```

- [x] **Step 6: Descendant rule helpers**

Add to `service.go`:

```go
// sessionsByID loads every session keyed by id (for ancestry walks).
func (s *Service) sessionsByID(ctx context.Context) (map[string]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	return byID, nil
}

// authorizeTarget enforces the descendant rule: an agent caller may only act
// on itself or sessions inside its subtree. UI and terminal callers pass.
func (s *Service) authorizeTarget(ctx context.Context, targetID string) error {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return nil
	}
	if targetID == caller.SessionID {
		return nil
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if ancestorContains(byID, targetID, caller.SessionID) {
		return nil
	}
	return ErrNotDescendant
}
```

Apply it:

- `ListSessions`:

```go
func (s *Service) ListSessions(ctx context.Context) ([]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return list, nil
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	out := make([]AgentSession, 0, len(list))
	for _, item := range list {
		if item.ID == caller.SessionID || ancestorContains(byID, item.ID, caller.SessionID) {
			out = append(out, item)
		}
	}
	return out, nil
}
```

- `GetStatus`: first line `if err := s.authorizeTarget(ctx, id); err != nil { return AgentSession{}, err }`.
- `Cancel` and `CloseSession`: first line `if err := s.authorizeTarget(ctx, id); err != nil { return err }`.
- `Invoke`: right after the `sess, err := s.store.GetSession(ctx, in.SessionID)` error check add:

```go
	if err := s.authorizeTarget(ctx, in.SessionID); err != nil {
		return InvokeResult{}, err
	}
	if caller, _ := identity.FromContext(ctx); caller.IsAgent() {
		if in.CallerSessionID != "" && in.CallerSessionID != caller.SessionID {
			return InvokeResult{}, ErrCallerMismatch
		}
		in.CallerSessionID = caller.SessionID // CheckCycle below runs on the real caller
	}
```

- `CloseSession`: close the endpoint and fire the hook:

```go
	s.mu.Lock()
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}

	sess.Status = SessionStatusClosed
	sess.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return err
	}
	if s.OnSessionClosed != nil {
		s.OnSessionClosed(id)
	}
	return nil
```

- [x] **Step 7: Run tests**

Run: `go test ./internal/agenthub/ -count=1 -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok`. Existing tests use plain `context.Background()` (no identity) with `Cwd: t.TempDir()` and are unaffected; any existing test that passed `Cwd: "/"` must be updated to a temp dir.

- [x] **Step 8: Commit**

```bash
git add internal/agenthub
git commit -m "feat(agenthub): identity-driven parent, depth, cwd and descendant rules; per-session MCP endpoint"
```

---

### Task 11: MCP — per-identity server, middleware, `SessionHost`

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: every `internal/mcp/tools_*.go` (mechanical)
- Modify: `internal/mcp/server_test.go` (`fakeCaller` records the caller)
- Create: `internal/mcp/session_host.go`
- Create: `internal/mcp/identity_test.go`

- [x] **Step 1: Mechanical refactor — register functions take the SDK server**

```bash
cd internal/mcp
sed -i '' 's/sdk\.AddTool(s\.mcp,/sdk.AddTool(ms,/g' tools_*.go
sed -i '' -E 's/^func \(s \*Server\) (register[A-Za-z]+)\(\) \{/func (s *Server) \1(ms *sdk.Server) {/' tools_*.go
grep -n "s\.mcp" *.go | grep -v _test
```

Expected grep output: only the two lines in `server.go` (`s.mcp = ms` and `MCP()`). If any register function referenced `s.mcp` for something other than `AddTool`, change it to `ms` by hand.

- [x] **Step 2: Write the failing test** (`identity_test.go`)

```go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callVaultStatus(t *testing.T, ms *sdk.Server) {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestSDKServerForStampsCaller(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.sdkServerFor(identity.Agent("11111111-2222-3333-4444-555555555555")))
	if caller.last.caller != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("caller reaching the gateway = %q", caller.last.caller)
	}
}

func TestDefaultServerIsUserTerminal(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.MCP())
	if caller.last.caller != "user-terminal" {
		t.Fatalf("shared-socket caller = %q, want user-terminal", caller.last.caller)
	}
}
```

In `server_test.go`, add `caller string` to `callRecord` and record it in `fakeCaller.Call`: `caller: identity.Label(ctx),` (import `devkit/internal/identity`).

- [x] **Step 3: Run to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestSDKServerFor|TestDefaultServer' -v`
Expected: FAIL — `srv.sdkServerFor undefined`.

- [x] **Step 4: Per-identity SDK server**

In `server.go` add import `"devkit/internal/identity"` and replace `NewServer`:

```go
// NewServer builds the tool registry. The returned Server's MCP() is the
// user-terminal instance served on the shared socket; SessionHost builds one
// more instance per agent socket via sdkServerFor.
func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	s.mcp = s.sdkServerFor(identity.UserTerminal())
	return s
}

// sdkServerFor builds an SDK server whose every request carries caller. One
// instance per identity — the shared socket gets user-terminal, each agent
// socket its own — so there is no per-connection lookup to race against.
func (s *Server) sdkServerFor(caller identity.Caller) *sdk.Server {
	ms := sdk.NewServer(&sdk.Implementation{Name: "devkit", Version: "0.1.0"}, nil)
	ms.AddReceivingMiddleware(withCaller(caller))
	s.registerVault(ms)
	s.registerNotes(ms)
	s.registerTools(ms)
	s.registerEnv(ms)
	s.registerSSH(ms)
	s.registerDatabase(ms)
	s.registerKafka(ms)
	s.registerAgentHub(ms)
	s.registerCodeGraph(ms)
	s.registerCodeGraphPlan(ms)
	return ms
}

// withCaller stamps caller on the context of every incoming request.
func withCaller(c identity.Caller) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			return next(identity.WithCaller(ctx, c), method, req)
		}
	}
}
```

- [x] **Step 5: Run and commit the middleware**

Run: `go test ./internal/mcp/ -count=1`
Expected: `ok`.

```bash
git add internal/mcp
git commit -m "feat(mcp): per-identity SDK server stamps the caller on every request"
```

- [x] **Step 6: Write the failing `SessionHost` test** (append to `identity_test.go`)

```go
import (
	"net"
	"os"
	"path/filepath"
	"time"

	"devkit/internal/agenthub"
)

func TestSessionHostOpensIdentityBoundSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "agents")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := NewSessionHost(ctx, srv, dir, "/usr/bin/true")

	const sid = "11111111-2222-3333-4444-555555555555"
	ep, err := host.Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(ep.SocketPath) != "1111111122223333.sock" {
		t.Fatalf("socket name = %s", filepath.Base(ep.SocketPath))
	}
	if ep.Server.Command != "/usr/bin/true" || len(ep.Server.Args) != 2 || ep.Server.Args[1] != ep.SocketPath || ep.Server.Env == nil {
		t.Fatalf("server = %+v", ep.Server)
	}
	if info, err := os.Stat(ep.SocketPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket stat: %v mode=%v", err, info)
	}

	conn, err := net.Dial("unix", ep.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, newRWTransport(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	cs.Close()
	if caller.last.caller != "agent:"+sid {
		t.Fatalf("caller = %q", caller.last.caller)
	}

	ep.Close()
	if _, err := os.Stat(ep.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("socket must be removed on Close, stat err = %v", err)
	}
}

func TestSessionHostWithoutProxyFailsClosed(t *testing.T) {
	host := NewSessionHost(context.Background(), NewServer(testDeps(&fakeCaller{}, nil)), t.TempDir(), "")
	if _, err := host.Open("11111111-2222-3333-4444-555555555555"); err != agenthub.ErrProxyNotFound {
		t.Fatalf("got %v, want ErrProxyNotFound", err)
	}
}

func TestSweepStaleSocketsRemovesDeadOnes(t *testing.T) {
	dir, _ := os.MkdirTemp("/tmp", "agents")
	defer os.RemoveAll(dir)
	dead := filepath.Join(dir, "dead.sock")
	ln, _ := net.Listen("unix", dead)
	ln.Close() // Close removes the file on Go; recreate a stale inode
	_ = os.WriteFile(dead, nil, 0o600)
	live := filepath.Join(dir, "live.sock")
	lln, _ := net.Listen("unix", live)
	defer lln.Close()
	go func() {
		for {
			c, err := lln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	SweepStaleSockets(dir)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("dead socket must be removed")
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("live socket must be kept")
	}
	_ = time.Second
}
```

(merge the import block with the existing one at the top of the file).

- [x] **Step 7: Run to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestSessionHost|TestSweep' -v`
Expected: FAIL — `undefined: NewSessionHost`.

- [x] **Step 8: `session_host.go`**

```go
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrSocketPathTooLong: sun_path is 104 bytes on darwin; a longer path fails
// at bind time with a confusing EINVAL, so refuse it up front.
var ErrSocketPathTooLong = errors.New("mcp: agent socket path exceeds the sun_path limit")

const maxSunPath = 103

// AgentsDir is where per-session sockets live: next to the shared socket.
func AgentsDir(sharedSocketPath string) string {
	return filepath.Join(filepath.Dir(sharedSocketPath), "agents")
}

// socketName shortens the session UUID so the full path fits sun_path even
// under a long ~/Library/Application Support prefix.
func socketName(sessionID string) string {
	id := strings.ReplaceAll(sessionID, "-", "")
	if len(id) > 16 {
		id = id[:16]
	}
	return id + ".sock"
}

// SessionHost opens one identity-bound MCP socket per Agent Hub session. It
// implements agenthub.MCPEndpoints.
type SessionHost struct {
	ctx       context.Context
	server    *Server
	dir       string
	proxyPath string // absolute devkit-mcp; "" = not found → Open fails closed

	mu        sync.Mutex
	listeners map[string]net.Listener
}

var _ agenthub.MCPEndpoints = (*SessionHost)(nil)

// NewSessionHost returns a host serving sockets under dir until ctx is done.
func NewSessionHost(ctx context.Context, server *Server, dir, proxyPath string) *SessionHost {
	return &SessionHost{ctx: ctx, server: server, dir: dir, proxyPath: proxyPath, listeners: map[string]net.Listener{}}
}

// Open creates the socket for sessionID and returns what the sandbox and the
// ACP session/new need. Spawning an agent that cannot reach DevKit is worse
// than not spawning it, so a missing proxy fails here, before the spawn.
func (h *SessionHost) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	if h.proxyPath == "" {
		return agenthub.MCPEndpoint{}, agenthub.ErrProxyNotFound
	}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	path := filepath.Join(h.dir, socketName(sessionID))
	if len(path) > maxSunPath {
		return agenthub.MCPEndpoint{}, fmt.Errorf("%w: %s", ErrSocketPathTooLong, path)
	}
	if conn, err := net.DialTimeout("unix", path, 250*time.Millisecond); err == nil {
		conn.Close()
		return agenthub.MCPEndpoint{}, fmt.Errorf("mcp: agent socket %s is already in use", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return agenthub.MCPEndpoint{}, err
	}

	ms := h.server.sdkServerFor(identity.Agent(sessionID))
	h.mu.Lock()
	h.listeners[sessionID] = ln
	h.mu.Unlock()
	go h.serve(ln, ms)

	closeFn := func() {
		h.mu.Lock()
		delete(h.listeners, sessionID)
		h.mu.Unlock()
		_ = ln.Close()
		_ = os.Remove(path)
	}
	return agenthub.MCPEndpoint{
		SocketPath: path,
		Server: acp.McpServerStdio{
			Name:    "devkit",
			Command: h.proxyPath,
			Args:    []string{"--socket", path},
			Env:     []acp.EnvVariable{},
		},
		Close: closeFn,
	}, nil
}

func (h *SessionHost) serve(ln net.Listener, ms *sdk.Server) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = ms.Run(h.ctx, newRWTransport(c))
		}(conn)
	}
}

// Close closes every open agent socket (app shutdown).
func (h *SessionHost) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ln := range h.listeners {
		_ = ln.Close()
		_ = os.Remove(filepath.Join(h.dir, socketName(id)))
		delete(h.listeners, id)
	}
}

// SweepStaleSockets removes agent sockets nobody answers on — leftovers of a
// DevKit that exited without cleanup. Sockets that accept are another live
// instance's and are left alone.
func SweepStaleSockets(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if conn, err := net.DialTimeout("unix", path, 250*time.Millisecond); err == nil {
			conn.Close()
			continue
		}
		_ = os.Remove(path)
	}
}
```

- [x] **Step 9: Run tests**

Run: `go test ./internal/mcp/ -count=1 && go test ./internal/arch/ -count=1`
Expected: both `ok` (`mcp` newly imports `acp`; no cycle — `acp` is leaf-like).

- [x] **Step 10: Commit**

```bash
git add internal/mcp
git commit -m "feat(mcp): per-session identity-bound sockets (SessionHost) and stale sweep"
```

---

### Task 12: `devkit-mcp --socket`

**Files:**
- Modify: `cmd/devkit-mcp/main.go`

- [x] **Step 1: Implement**

Replace the tail of `main` and `runProxy`:

```go
	fs := flag.NewFlagSet("devkit-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "unix socket of the DevKit MCP host (default: the shared mcp.sock)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if err := runProxy(*socket); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
```

```go
// runProxy pipes stdio to the DevKit MCP host. With --socket it targets the
// per-session socket Agent Hub handed the agent; without, the shared socket.
func runProxy(path string) error {
	if path == "" {
		shared, err := mcp.SocketPath()
		if err != nil {
			return err
		}
		path = shared
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("DevKit is not running or MCP host is unavailable")
	}
```

(the rest of `runProxy` is unchanged).

- [x] **Step 2: Verify**

Run: `go build -o bin/devkit-mcp ./cmd/devkit-mcp && ./bin/devkit-mcp --socket /nonexistent.sock </dev/null; echo "exit=$?"`
Expected: prints `DevKit is not running or MCP host is unavailable`, `exit=1`.

- [x] **Step 3: Commit**

```bash
git add cmd/devkit-mcp/main.go
git commit -m "feat(devkit-mcp): --socket flag for per-session endpoints"
```

---

### Task 13: Bridge — error mapping, pending permission list, grant

**Files:**
- Modify: `internal/bridge/errors.go`
- Modify: `internal/bridge/errors_agenthub_test.go` (append)
- Modify: `internal/bridge/agenthub.go`
- Modify: `internal/app/container.go`

- [x] **Step 1: Failing error-mapping test** (append; follow the file's existing helper for asserting a code — it maps `publicError("builtin.agenthub", err)` to an `apperrors` code)

```go
func TestAgentHubIdentityErrorsMap(t *testing.T) {
	cases := map[error]string{
		agenthub.ErrNotDescendant:    codePermissionDenied,
		agenthub.ErrCwdOutsideParent: codeValidationFailed,
		agenthub.ErrInvalidCwd:       codeValidationFailed,
		agenthub.ErrCallerMismatch:   codeValidationFailed,
		agenthub.ErrProxyNotFound:    codeConnectionFailed,
	}
	for in, want := range cases {
		got := publicError("builtin.agenthub", in)
		var appErr *apperrors.Error
		if !errors.As(got, &appErr) || appErr.Code != want {
			t.Fatalf("%v → %v, want code %s", in, got, want)
		}
	}
}
```

If `apperrors.Error`'s field is not `Code`, use the accessor the existing tests in this file use.

- [x] **Step 2: Map them**

In `errors.go`, inside `if subject == "builtin.agenthub" { switch { ... } }` add:

```go
		case errors.Is(err, agenthub.ErrNotDescendant):
			return apperrors.New(codePermissionDenied, "Agent session is outside the caller's tree.", false)
		case errors.Is(err, agenthub.ErrCwdOutsideParent):
			return apperrors.New(codeValidationFailed, "Agent cwd must be inside the parent session's project.", true)
		case errors.Is(err, agenthub.ErrInvalidCwd):
			return apperrors.New(codeValidationFailed, "Agent cwd is not a valid project directory.", true)
		case errors.Is(err, agenthub.ErrCallerMismatch):
			return apperrors.New(codeValidationFailed, "callerSessionId does not match the calling agent.", true)
		case errors.Is(err, agenthub.ErrProxyNotFound):
			return apperrors.New(codeConnectionFailed, "DevKit MCP proxy (devkit-mcp) was not found; build it first.", true)
```

- [x] **Step 3: Pending permission list**

Add to `internal/app/container.go` grants, next to the other `builtin.agenthub` rows:

```go
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "permission:list"},
```

Add to `internal/bridge/agenthub.go`:

```go
// AgentHubPendingPermissions lists every ACP permission prompt awaiting the
// user, oldest first. The frontend uses it to rebuild its queue on mount;
// live arrivals still come through the agenthub:permission event.
func (s *AppService) AgentHubPendingPermissions() ([]agenthub.PendingPermission, error) {
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:read", "permission:list", map[string]any{
		"action": "pending_permissions",
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		return waiter.PendingList(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.PendingPermission), nil
}
```

- [x] **Step 4: Run tests and commit**

Run: `go test ./internal/bridge/ ./internal/app/ -count=1`
Expected: `ok`.

```bash
git add internal/bridge internal/app/container.go
git commit -m "feat(bridge): map identity errors; list pending agent permissions"
```

---

### Task 14: `main.go` wiring

**Files:**
- Modify: `main.go`

- [x] **Step 1: Persistent audit, before anything captures the gateway**

Right after `container.UIState = uistate.NewService(uistate.NewSQLRepository(db))` add (imports: `devkit/internal/audit`, `devkit/internal/capabilities`, `devkit/internal/identity`, `devkit/internal/mcp/install`, `time` — add whichever are missing):

```go
	// Audit persists to SQLite (audit_events); the in-memory sink stays as the
	// diagnostic view and the fallback when a write fails. Rebuild the gateway
	// here, before RegisterBuiltins / NewAppService capture it.
	auditSQL := audit.NewSQLSink(db, storage.TableAuditEvents, container.Audit)
	defer auditSQL.Close()
	container.Capabilities = capabilities.NewGateway(container.Permissions, audit.NewTee(container.Audit, auditSQL))
	if err := auditSQL.Prune(context.Background()); err != nil {
		log.Printf("audit: prune: %v", err)
	}
```

Verify with `grep -n "container.Capabilities" main.go internal/app/*.go` that nothing reads `Capabilities` between `NewContainerWithStores` and this line.

- [x] **Step 2: Permission event carries the caller and title**

Replace the `agentWaiter := agenthub.NewPermissionWaiter(...)` block:

```go
	agentWaiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		payload := map[string]any{
			"id":        p.ID,
			"sessionId": p.SessionID,
			"params":    rawJSON(p.Params),
			"caller":    identity.Agent(p.SessionID).String(),
		}
		// Title/depth let the prompt say which agent is asking; the hub
		// service is assigned a few lines below and exists by prompt time.
		if container.AgentHub != nil {
			if sess, err := container.AgentHub.GetStatus(context.Background(), p.SessionID); err == nil {
				if sess.Title != nil {
					payload["title"] = *sess.Title
				}
				payload["depth"] = sess.Depth
			}
		}
		emitAppEvent("agenthub:permission", payload)
	})
```

- [x] **Step 3: Hub hook**

Right after `hub := interaction.NewHub(interaction.NoopPrompter{})` / `container.Interaction = hub`:

```go
	agentSvc.OnSessionClosed = func(id string) { hub.ForgetCaller(identity.Agent(id).String()) }
```

- [x] **Step 4: Session host**

Right after the `go func() { ... mcp.ServeUnix(...) }()` block:

```go
	// Per-session identity-bound sockets for Agent Hub. A missing proxy is
	// logged here and surfaces as ErrProxyNotFound on CreateSession.
	agentsDir := mcp.AgentsDir(sockPath)
	mcp.SweepStaleSockets(agentsDir)
	proxyPath, err := install.ResolveCommand()
	if err != nil {
		log.Printf("agenthub: devkit-mcp proxy not found; agent sessions will fail to open until it is built (task mcp): %v", err)
		proxyPath = ""
	}
	sessionHost := mcp.NewSessionHost(mcpCtx, mcpServer, agentsDir, proxyPath)
	defer sessionHost.Close()
	agentSvc.MCPEndpoints = sessionHost
	auditSQL.PruneLoop(mcpCtx, time.Hour)
```

- [x] **Step 5: Build and run the Go suite**

Run: `go build . && go vet . && go test ./internal/... -count=1 2>&1 | grep -E "^(FAIL|ok)" | grep -v "^ok"`
Expected: `go build .` succeeds; the grep prints nothing (no FAIL lines).

- [x] **Step 6: Commit**

```bash
git add main.go
git commit -m "feat(main): wire persistent audit, session MCP host, and hub forget hook"
```

---

### Task 15: Frontend — queues and caller display

**Files:**
- Modify: `frontend/src/bridge/interaction.ts`
- Modify: `frontend/src/workbench/InteractionHost.tsx`
- Modify: `frontend/src/bridge/agenthub.ts`
- Modify: `frontend/src/stores/agentSessions.ts`
- Modify: `frontend/src/workbench/AgentSessionsSubscriber.tsx`
- Modify: `frontend/src/modules/agenthub/AgentHubView.tsx` (and any other `setPending(null)` caller)
- Regenerate: `frontend/bindings/`

- [x] **Step 1: Regenerate bindings**

Run: `wails3 generate bindings -ts`
Expected: `frontend/bindings/devkit/internal/bridge/appservice.ts` now exports `InteractionPendingList` and `AgentHubPendingPermissions`; `models.ts` `Pending` gains `caller`/`createdAt`.

- [x] **Step 2: `interaction.ts`**

Add `caller: string;` to `PendingInteraction`, read it in `toPending` (`caller: raw.caller ?? ""` with `caller?: string` on the raw type), and add:

```ts
export function getInteractionPendingList(): Promise<PendingInteraction[]> {
  return callBridge(async () => {
    const list = (await AppService.InteractionPendingList()) ?? [];
    return list
      .map((raw) => toPending(raw))
      .filter((p): p is PendingInteraction => p !== null);
  });
}
```

- [x] **Step 3: `InteractionHost.tsx` renders the head of a queue**

Replace the polling state and effect:

```tsx
  const [queue, setQueue] = useState<PendingInteraction[]>([]);
  const pending = queue[0] ?? null;
```

```tsx
  useEffect(() => {
    let mounted = true;
    const tick = async () => {
      try {
        const next = await getInteractionPendingList();
        if (!mounted) {
          return;
        }
        setQueue((prev) => {
          const same =
            prev.length === next.length && prev.every((p, i) => p.id === next[i]?.id);
          return same ? prev : next;
        });
      } catch {
        // Ignore transient poll errors; next tick retries.
      }
    };
    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, POLL_MS);
    return () => {
      mounted = false;
      window.clearInterval(timer);
    };
  }, []);
```

In `run`, replace `setPending(null)` with `setQueue((prev) => prev.filter((p) => p.id !== pending?.id))`. Replace the import of `getInteractionPending` with `getInteractionPendingList`.

In both dialogs, after `<DialogDescription …>…</DialogDescription>` inside `DialogHeader`, add the caller line and queue count:

```tsx
            <p className="text-xs text-muted-foreground">
              Requested by <code>{pending.caller || "unknown"}</code>
              {queue.length > 1 ? ` · ${queue.length - 1} more waiting` : ""}
            </p>
```

- [x] **Step 4: `agenthub.ts` — caller, title, depth on the event**

```ts
export type PendingPermission = {
  id: string;
  sessionId: string;
  params: unknown;
  caller: string;
  title: string;
  depth: number;
};
```

In `parsePermission`, return:

```ts
  return {
    id,
    sessionId,
    params: record.params ?? record.Params ?? null,
    caller: typeof record.caller === "string" ? record.caller : `agent:${sessionId}`,
    title: typeof record.title === "string" ? record.title : "",
    depth: typeof record.depth === "number" ? record.depth : 0,
  };
```

Add a fetcher for the queue on mount:

```ts
export function agentHubPendingPermissions(): Promise<PendingPermission[]> {
  return callBridge(async () => {
    const list = (await AppService.AgentHubPendingPermissions()) ?? [];
    return list
      .map((raw) =>
        parsePermission({
          id: raw.ID ?? raw.id,
          sessionId: raw.SessionID ?? raw.sessionId,
          params: raw.Params ?? raw.params,
        }),
      )
      .filter((p): p is PendingPermission => p !== null);
  });
}
```

(adjust `raw.ID`/`raw.id` to whichever casing the generated `models.ts` uses for `PendingPermission`; the JSON tags are `id`, `sessionId`, `params`.)

- [x] **Step 5: Store keeps a queue, `pending` stays the head**

In `stores/agentSessions.ts`:

```ts
  pending: PendingPermission | null;
  pendingQueue: PendingPermission[];
  ...
  enqueuePending: (pending: PendingPermission) => void;
  dequeuePending: (id: string) => void;
```

```ts
  pending: null,
  pendingQueue: [],
  ...
  enqueuePending: (pending) =>
    set((state) => {
      if (state.pendingQueue.some((p) => p.id === pending.id)) {
        return {};
      }
      const pendingQueue = [...state.pendingQueue, pending];
      return { pendingQueue, pending: state.pending ?? pending };
    }),
  dequeuePending: (id) =>
    set((state) => {
      const pendingQueue = state.pendingQueue.filter((p) => p.id !== id);
      return { pendingQueue, pending: pendingQueue[0] ?? null };
    }),
```

Remove `setPending` from the type and the implementation, then:

```bash
grep -rn "setPending" frontend/src
```

For each hit: a call that enqueued a prompt (`setPending(pending)` in `AgentSessionsSubscriber.tsx`) becomes `enqueuePending(pending)`; a call that cleared it after resolving (`setPending(null)` in `AgentHubView.tsx` `resolvePermission`, and the same pattern in `AgentCompanion.tsx` if present) becomes `dequeuePending(pending.id)`; selectors `state.setPending` are renamed accordingly.

In `AgentSessionsSubscriber.tsx`, also rebuild the queue on mount inside the first effect:

```ts
    agentHubPendingPermissions()
      .then((list) => {
        if (mounted) {
          list.forEach((p) => enqueuePending(p));
        }
      })
      .catch(() => {
        // Hub unavailable — no prompts to restore.
      });
```

(import `agentHubPendingPermissions`; select `enqueuePending` from the store.)

- [x] **Step 6: `AgentHubView.tsx` names the agent**

Replace the `<p>Session <code>…</code> needs a permission decision.</p>` with:

```tsx
            <p>
              Agent <strong>{pending.title || pending.sessionId.slice(0, 8)}</strong>
              {" "}(depth {pending.depth}) needs a permission decision.
              {useAgentSessionsStore.getState().pendingQueue.length > 1
                ? ` ${useAgentSessionsStore.getState().pendingQueue.length - 1} more waiting.`
                : ""}
            </p>
```

- [x] **Step 7: Type-check and build**

Run: `cd frontend && bun x tsc --noEmit && bun run build:dev`
Expected: no type errors; build succeeds.

- [x] **Step 8: Commit**

```bash
git add frontend/bindings frontend/src
git commit -m "feat(frontend): queue MCP interactions and agent permissions; show the requesting agent"
```

---

### Task 16: End-to-end check and docs

**Files:**
- Modify (base-doc repo): `base-doc/content/projects/dev-kit/agent-hub.mdx`, `mcp-agent.mdx`, `sandbox.mdx`, `security-controls.mdx`, `implementation-status.mdx`

- [x] **Step 1: Live check with a real agent**

Run: `go build -o bin/devkit-mcp ./cmd/devkit-mcp && task dev`
In the app: open an Agent Hub session (Claude), prompt: `Call the DevKit MCP tool env.listTags and tell me the tag names.` The agent must answer with the tags (`dev`, `stg`, `prod`, …).

Then:

```bash
sqlite3 "$HOME/Library/Application Support/DevKit/devkit.db" \
  "SELECT at, caller, capability, scope, outcome FROM audit_events WHERE caller LIKE 'agent:%' ORDER BY id DESC LIMIT 5;"
```

Expected: rows with `caller = agent:<session uuid>`, `capability = env:read`, `outcome = allowed`. Also `ls "$HOME/Library/Application Support/DevKit/agents/"` shows one `<16hex>.sock` per open session, and none after closing them.

If the agent reports it cannot see a `devkit` MCP server: check `~/.claude/settings.json` has no `sandbox` block enabled (nested sandboxes fail — see `sandbox.mdx`), and that `bin/devkit-mcp` exists (Task 14 logs `devkit-mcp proxy not found` otherwise).

- [x] **Step 2: Docs**

In the four base-doc pages, change the section titles' `(Design)` suffix to `(Implemented)` and update their opening line to name the evidence: `internal/identity`, `internal/mcp/session_host.go`, `internal/audit/sqlsink.go`, tests `internal/agenthub/service_identity_test.go`, `internal/mcp/identity_test.go`, `internal/interaction/hub_identity_test.go`, `internal/sandbox/backend_seatbelt_test.go` (`TestSeatbeltAllowUnixSocketsConnects`). Prose only, no code blocks (project convention). In `security-controls.mdx` move the *Multi-agent (review 2026-09-05)* item from Remaining risks into the control matrix as a new row with the same evidence list, and leave a one-line residual: loopback TCP still reachable (needs the egress proxy) and `codegraph.*` still `project:active` (needs Workspace). In `implementation-status.mdx`, add an *Agent identity* line as Implemented.

- [x] **Step 3: Commit (base-doc is its own tree; commit there if it is a git repo, otherwise leave for the user)**

```bash
git -C ../base-doc add content/projects/dev-kit && git -C ../base-doc commit -m "docs: agent identity implemented" || echo "base-doc not a git repo — skip"
```

---

## Self-review

**Spec coverage.** Identity = socket (T11); shared socket stays `user-terminal` (T11 `NewServer`); id-first creation and `MCPEndpoints` interface (T10); `Open(ctx, spec)` (T9); descendant rule for create/invoke/cancel/get/list (T10); `cwd` rules incl. `/` and home (T10); pending map in `PermissionWaiter` (T6) and `Hub` (T5); `Pending.Caller` + UI naming (T5, T14, T15); `allow_always` key `(caller, tool, profile)` + `ForgetCaller` + `OnSessionClosed` (T5, T10, T14); Gateway stamps `Caller`/`At`, bridge `ui`, `unknown` never denied (T2, T3); `SQLSink` with backpressure/fallback/prune, `Tee`, migration (T4); `AllowUnixSockets` in Seatbelt (resolved path, after network deny) and bwrap (after tmpfs home) with live test (T8); proxy RO + absolute path + `ErrProxyNotFound` (T10, T11, T14); `--socket` (T12); ACP typed stdio server with required `env` (T7, T9); error mapping (T13); docs (T16). Not in spec / not done: multi-store CodeGraph, egress proxy, `invokeAgent` async.

**Placeholders.** None: every step has code or an exact command with expected output. The one discretionary instruction (T15 step 5 `setPending` rename) gives the mechanical rule per call-site kind.

**Type consistency.** `identity.Caller.String()` is the single label used by `Hub` (`allowKey.caller`, `Pending.Caller`), `audit.Event.Caller`, main's `ForgetCaller` argument, and the event payload. `agenthub.MCPEndpoint{SocketPath, Server, Close}` is produced by `mcp.SessionHost.Open` and consumed by `agenthub.Service.CreateSession`. `OpenSpec{Cwd, AllowUnixSockets, McpServers, ReadOnlyPaths}` matches `buildSandbox(spec)` and `MockAdapter.LastSpec()`. `PendingList()` exists on both `Hub` and `PermissionWaiter`; `PendingByID` only on `Hub` (used by `InteractionDeny`).
