# MCP Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship DevKit as an MCP **Server** (Tools only, stdio) so external agents can call existing built-in modules through the **same** Capability Gateway path as the UI Bridge — with interactive **active-target selection** and **env-tag confirmation** for SSH/DB/Kafka — matching `/dev-kit/mcp-agent` and per-module MCP tool surfaces.

**Architecture:** `internal/mcp` is a thin transport/tool adapter. Tool handlers never call domain services directly without going through an exported `AppService.Call` (same gateway + audit as Bridge), always with `Metadata["origin"]="mcp"`. Because selection/confirmation popups and an unlocked vault live in the **desktop GUI process**, the GUI hosts the real MCP tool runtime over a Unix domain socket; `cmd/devkit-mcp` is a stdio↔socket proxy that agents spawn. Headless / no GUI → selection/confirmation fail immediately (no hang).

**Tech Stack:** Go 1.26, official `github.com/modelcontextprotocol/go-sdk` (`mcp` package, target protocol `2026-07-28` / SDK ≥ v1.7), existing `capabilities` / `bridge` / `environments` / SSH / Database / Kafka / Notes / Vault / React+Vite frontend, `go-task` + bun.

**Spec sources (authoritative):**
- `syurodev/base` → `content/projects/dev-kit/mcp-agent.mdx`
- Per-module tool tables: `vault.mdx`, `notes.mdx`, `tools.mdx`, `ssh.mdx`, `database.mdx`, `kafka.mdx`, `environments.mdx`, `technical-contracts.mdx`

**Explicitly out of scope (this plan / v1):**
- Streamable HTTP / remote MCP / OAuth
- MCP Resources and Prompts
- SSH forwarding tools, Sync mutation tools, vault setup/unlock/lock tools
- Create/delete profile tools that accept secrets
- Code Graph / Agent Hub tools (modules not Implemented)
- Per-tool enable/disable Settings UI (static container grants only)
- SQL parse split for `db.query` read vs write (treat all queries as execute + `destructiveHint`)

**Concrete defaults (open questions locked):**

| Item | Decision |
|---|---|
| Process model | GUI hosts tools + interaction; `cmd/devkit-mcp` = stdio proxy to Unix socket |
| Socket path | `filepath.Join(configDir, "DevKit", "mcp.sock")` mode `0600`; remove on clean shutdown |
| Selection / confirmation timeout | `60s` each |
| Headless / no Prompter | Return structured `selection_required` / `confirmation_required` immediately |
| Confirmation choices | Allow / Deny only — **no** `allow_always` |
| Unresolvable env tag | Fail closed → treat as `requiresConfirmation=true` |
| `db.query` annotations | Always `readOnlyHint=false`, `destructiveHint=true` |
| `tools/list` filtering | Only register tools whose subject/capability/scope are granted in `container` policy (static = register allowlist ∩ grants) |
| SDK | `github.com/modelcontextprotocol/go-sdk` latest ≥ v1.7 |
| Logging | Never write to stdout from MCP host/proxy internals (stdout = JSON-RPC); use stderr / existing audit |

---

## Process model (read before coding)

```text
  Claude Code / Codex
        |  stdio JSON-RPC
        v
  cmd/devkit-mcp  (proxy, no vault, no business logic)
        |  Unix domain socket (length-prefixed JSON frames — see Task 8)
        v
  DevKit desktop (Wails) — unlocked vault, React UI
        |  internal/mcp.Server (tool handlers)
        |  AppService.Call(origin=mcp) → Capability Gateway → domain services
        |  interaction.Hub → frontend dialogs (selection / confirmation)
```

**Why not** run tools inside the agent-spawned process alone?
- Vault DEK is in-memory in the GUI after unlock; a separate process would see `vault_locked` forever unless we invent a second unlock path (forbidden as MCP tool).
- Selection/confirmation **require** a live UI.

**Server mode (`task run:server`):** socket host may start, but Prompter is nil → active-target tools that need popup fail fast. Tools that need no interaction (e.g. `vault.status`, `notes.list` if vault unlocked in that process) still work when wired with an in-process vault.

---

## File map

| Path | Responsibility |
|---|---|
| `internal/bridge/gateway.go` | Export `Call`; document origin key |
| `internal/bridge/caller.go` | `Caller` interface for mcp tests |
| `internal/interaction/hub.go` | Selection + confirmation wait/resume; timeouts; headless errors |
| `internal/interaction/hub_test.go` | Unit tests with fake Prompter |
| `internal/interaction/errors.go` | `ErrSelectionRequired`, `ErrConfirmationDenied`, `ErrInteractionTimeout`, `ErrConfirmationRequired` (headless) |
| `internal/environments/environments.go` | Add `RequiresConfirmation(name string) bool` (fail-closed) |
| `internal/ssh/active.go` (+ tests) | Agent sticky active profile ID (separate from UI `profileId` args) |
| `internal/database/active.go` (+ tests) | Same for DB |
| `internal/bridge/ssh_active.go` | `SSHActiveProfile`, `SSHSetActiveProfile` (Set = bridge/UI only) |
| `internal/bridge/database_active.go` | `DBActiveProfile`, `DBSetActiveProfile` |
| `internal/bridge/interaction.go` | Bridge methods for UI to complete/deny interactions |
| `internal/bridge/errors.go` | Map interaction + mcp errors to `AppError` codes |
| `internal/mcp/server.go` | Build `mcp.Server`, register tools, `ServeConn` for socket |
| `internal/mcp/origin.go` | `OriginMCP = "mcp"` helper merging metadata |
| `internal/mcp/tools_*.go` | One file per module family (vault, notes, tools, env, ssh, db, kafka) |
| `internal/mcp/server_test.go` | In-process tool call tests with fake Caller / services |
| `internal/mcp/host.go` | Listen Unix socket; accept; serve one MCP session per conn |
| `cmd/devkit-mcp/main.go` | Stdio ↔ socket proxy |
| `internal/app/container.go` | Wire `Interaction` hub; grant `profile:active` for ssh/db |
| `main.go` | Start/stop MCP host with app lifecycle; pass event emitter / Prompter |
| `frontend/src/bridge/interaction.ts` | Typed adapters for pending interactions |
| `frontend/src/workbench/InteractionHost.tsx` | Global dialogs: select profile / confirm env |
| `frontend/src/workbench/Workbench.tsx` | Mount `InteractionHost` |
| `internal/arch/arch_test.go` | Ban `mcp → storage/crypto/sync*`; ban domain → mcp |
| `Taskfile.yml` | `task mcp` runs `go run ./cmd/devkit-mcp` |
| base docs | Flip MCP callouts to Implemented / Partial as appropriate |

---

## Tool allowlist (v1 — implement exactly these)

| Tool | Effect / hints | Subject / Cap / Scope | Needs active target? | Env confirmation? |
|---|---|---|---|---|
| `vault.status` | read | `builtin.vault` / `vault:manage` / `vault` | no | no |
| `vault.search` | read | `builtin.search` / `vault:search` / `item:search` | no | no |
| `notes.list` | read | `builtin.notes` / `notes:read` / `note:list` | no | no |
| `notes.search` | read | `builtin.notes` / `notes:read` / `note:search` | no | no |
| `notes.get` | read | `builtin.notes` / `notes:read` / `note:<id>` | no | no |
| `notes.create` | execute | `builtin.notes` / `notes:write` / `note:new` | no | no |
| `notes.delete` | execute + destructive | `builtin.notes` / `notes:write` / `note:<id>` | no | no |
| `tools.hash` | execute | `builtin.tools` / `tools:hash` / `command:tools.hash` | no | no |
| `env.listTags` | read | `builtin.environments` / `env:read` / `tag:list` | no | no |
| `ssh.listProfiles` | read | `builtin.ssh` / `ssh:connect` / `profile:list` | no | no |
| `ssh.testConnection` | execute | `builtin.ssh` / `ssh:connect` / `profile:<id>` | no (explicit id) | no |
| `ssh.activeProfile` | read | `builtin.ssh` / `ssh:connect` / `profile:active` | no | no |
| `ssh.runCommand` | execute + destructive | `builtin.ssh` / `ssh:connect` / `profile:active` | **yes** | **yes** |
| `db.listProfiles` | read | `builtin.database` / `db:query` / `profile:list` | no | no |
| `db.testConnection` | execute | `builtin.database` / `db:query` / `profile:<id>` | no | no |
| `db.activeProfile` | read | `builtin.database` / `db:query` / `profile:active` | no | no |
| `db.query` | execute + destructive | `builtin.database` / `db:query` / `profile:active` | **yes** | **yes** |
| `kafka.listProfiles` | read | `builtin.kafka` / `kafka:connect` / `profile:list` | no | no |
| `kafka.connectionStatus` | read | `builtin.kafka` / `kafka:connect` / `connection` | no | no |
| `kafka.listTopics` | read | `builtin.kafka` / `kafka:connect` / `connection` | **yes** (live conn) | **yes** |
| `kafka.describeTopic` | read | same | **yes** | **yes** |
| `kafka.peekMessages` | read | same | **yes** | **yes** |
| `kafka.listConsumerGroups` | read | same | **yes** | **yes** |
| `kafka.publishMessage` | execute + destructive | `builtin.kafka` / `kafka:produce` / `connection` | **yes** | **yes** |

**Hard exclusions (must not register):** vault setup/unlock/lock; SSH trust/observe host key; SSH forward; profile create/delete; Kafka connect/disconnect; env create/update/delete; sync.*.

---

### Task 1: Export `AppService.Call` + `Caller` interface

**Files:**
- Modify: `internal/bridge/gateway.go`
- Create: `internal/bridge/caller.go`
- Modify: `internal/bridge/gateway_test.go` (or add `caller_test.go`)

- [ ] **Step 1: Write failing test for exported Call**

```go
func TestAppServiceCallSetsMetadata(t *testing.T) {
	// Build AppService the same way existing gateway_test helpers do.
	var saw map[string]any
	svc := newTestAppServiceWithGatewayCapture(t, func(meta map[string]any) {
		saw = meta
	})
	_, err := svc.Call(context.Background(), "builtin.notes", "notes:read", "note:list", map[string]any{
		"origin": "mcp",
		"action": "list",
	}, func(ctx context.Context) (any, error) {
		return []string{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if saw["origin"] != "mcp" {
		t.Fatalf("origin = %v", saw["origin"])
	}
}
```

Adapt helper construction to existing test fixtures in `gateway_test.go` — do not invent a parallel container if one already exists.

- [ ] **Step 2: Run test — expect fail**

```bash
go test ./internal/bridge/ -run TestAppServiceCallSetsMetadata -count=1
```

Expected: FAIL (`Call` undefined).

- [ ] **Step 3: Implement**

In `gateway.go`, rename usage so public method exists:

```go
// Call runs a capability-gated handler and maps errors through publicError.
// Bridge UI methods and MCP tool handlers must both use this entrypoint.
func (s *AppService) Call(
	ctx context.Context,
	subject string,
	capability string,
	scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	return s.call(ctx, subject, capability, scope, metadata, handler)
}
```

Keep unexported `call` as the implementation body (or inline once). Add `caller.go`:

```go
package bridge

import (
	"context"

	"devkit/internal/capabilities"
)

// Caller is the narrow surface internal/mcp depends on (tests fake it).
type Caller interface {
	Call(
		ctx context.Context,
		subject, capability, scope string,
		metadata map[string]any,
		handler capabilities.Handler,
	) (any, error)
}

// Ensure AppService implements Caller.
var _ Caller = (*AppService)(nil)
```

Update existing private call sites if needed (they can keep using `call`).

- [ ] **Step 4: Run tests**

```bash
go test ./internal/bridge/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/gateway.go internal/bridge/caller.go internal/bridge/gateway_test.go
git commit -m "feat(bridge): export Call and Caller interface for MCP"
```

---

### Task 2: Environments `RequiresConfirmation` fail-closed helper

**Files:**
- Modify: `internal/environments/environments.go`
- Modify: `internal/environments/environments_test.go` (or create)

- [ ] **Step 1: Failing tests**

```go
func TestRequiresConfirmationProdTrue(t *testing.T) {
	svc := newMemoryEnvService(t) // use existing memory helper / NewService(memoryRepo, nil)
	if !svc.RequiresConfirmation("prod") {
		t.Fatal("prod must require confirmation")
	}
}

func TestRequiresConfirmationUnknownFailClosed(t *testing.T) {
	svc := newMemoryEnvService(t)
	if !svc.RequiresConfirmation("no-such-tag") {
		t.Fatal("unknown env must fail closed (true)")
	}
}

func TestRequiresConfirmationDevDefaultFalse(t *testing.T) {
	svc := newMemoryEnvService(t)
	if svc.RequiresConfirmation("dev") {
		t.Fatal("dev default false")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/environments/ -run TestRequiresConfirmation -count=1
```

- [ ] **Step 3: Implement**

```go
// RequiresConfirmation reports the MCP confirmation policy for an env tag name.
// Unknown or unreadable tags fail closed (true).
func (s *Service) RequiresConfirmation(name string) bool {
	if name == "" {
		return true
	}
	tag, err := s.repo.Get(name)
	if err != nil {
		return true
	}
	return tag.RequiresConfirmation
}
```

- [ ] **Step 4: Pass + commit**

```bash
go test ./internal/environments/ -count=1
git add internal/environments/
git commit -m "feat(environments): RequiresConfirmation fail-closed helper for MCP"
```

---

### Task 3: Interaction hub (selection + confirmation)

**Files:**
- Create: `internal/interaction/errors.go`
- Create: `internal/interaction/hub.go`
- Create: `internal/interaction/hub_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestSelectBlocksUntilComplete(t *testing.T) {
	h := NewHub(nil) // Prompter optional for Complete path
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got string
	done := make(chan error, 1)
	go func() {
		id, err := h.SelectProfile(ctx, SelectRequest{
			Kind:     KindSSH,
			PromptID: "ignored-if-completed-via-api",
		})
		got = id
		done <- err
	}()

	// Wait until pending exists
	waitForPending(t, h)
	pend := h.Pending()
	if pend == nil || pend.Type != TypeSelect {
		t.Fatalf("pending = %+v", pend)
	}
	if err := h.Complete(pend.ID, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectTimeout(t *testing.T) {
	h := NewHub(nil)
	h.Timeout = 20 * time.Millisecond
	_, err := h.SelectProfile(context.Background(), SelectRequest{Kind: KindKafka})
	if !errors.Is(err, ErrInteractionTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectHeadlessNoPrompterImmediate(t *testing.T) {
	h := NewHub(nil)
	h.Headless = true
	_, err := h.SelectProfile(context.Background(), SelectRequest{Kind: KindSSH})
	if !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmAllowAndDeny(t *testing.T) {
	h := NewHub(nil)
	ctx := context.Background()
	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "allow")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "prod", Tool: "ssh.runCommand", Summary: "ls"}); err != nil {
		t.Fatal(err)
	}

	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "deny")
	}()
	if err := h.Confirm(ctx, ConfirmRequest{Env: "prod", Tool: "ssh.runCommand", Summary: "rm"}); !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/interaction/ -count=1
```

- [ ] **Step 3: Implement hub**

```go
package interaction

import (
	"context"
	"errors"
	"sync"
	"time"

	"devkit/internal/vaultitem"
)

const DefaultTimeout = 60 * time.Second

var (
	ErrSelectionRequired     = errors.New("selection required")
	ErrConfirmationRequired  = errors.New("confirmation required")
	ErrConfirmationDenied    = errors.New("confirmation denied")
	ErrInteractionTimeout    = errors.New("interaction timeout")
	ErrNoPendingInteraction  = errors.New("no pending interaction")
	ErrInvalidInteractionID  = errors.New("invalid interaction id")
)

type Type string

const (
	TypeSelect  Type = "select"
	TypeConfirm Type = "confirm"
)

type Kind string

const (
	KindSSH      Kind = "ssh"
	KindDatabase Kind = "database"
	KindKafka    Kind = "kafka"
)

// Prompter notifies the UI that a human decision is needed.
// Implementations emit a Wails event (or no-op in tests that Complete via API).
type Prompter interface {
	Prompt(pending Pending) error
}

type SelectRequest struct {
	Kind Kind
}

type ConfirmRequest struct {
	Env     string
	Tool    string
	Summary string
	Kind    Kind
}

type Pending struct {
	ID      string `json:"id"`
	Type    Type   `json:"type"`
	Kind    Kind   `json:"kind,omitempty"`
	Env     string `json:"env,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type Hub struct {
	Timeout  time.Duration
	Headless bool
	prompter Prompter

	mu      sync.Mutex
	pending *pendingWait
}

type pendingWait struct {
	Pending
	result chan string
}

func NewHub(p Prompter) *Hub {
	return &Hub{Timeout: DefaultTimeout, prompter: p}
}

func (h *Hub) Pending() *Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return nil
	}
	cp := h.pending.Pending
	return &cp
}

func (h *Hub) Complete(id, value string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return ErrNoPendingInteraction
	}
	if h.pending.ID != id {
		return ErrInvalidInteractionID
	}
	select {
	case h.pending.result <- value:
	default:
	}
	return nil
}

func (h *Hub) SelectProfile(ctx context.Context, req SelectRequest) (string, error) {
	if h.Headless || h.prompter == nil {
		return "", ErrSelectionRequired
	}
	return h.wait(ctx, Pending{Type: TypeSelect, Kind: req.Kind}, func(v string) (string, error) {
		if v == "" {
			return "", ErrConfirmationDenied
		}
		return v, nil
	})
}

func (h *Hub) Confirm(ctx context.Context, req ConfirmRequest) error {
	if h.Headless || h.prompter == nil {
		return ErrConfirmationRequired
	}
	_, err := h.wait(ctx, Pending{
		Type: TypeConfirm, Kind: req.Kind, Env: req.Env, Tool: req.Tool, Summary: req.Summary,
	}, func(v string) (string, error) {
		if v != "allow" {
			return "", ErrConfirmationDenied
		}
		return v, nil
	})
	return err
}

func (h *Hub) wait(ctx context.Context, p Pending, mapResult func(string) (string, error)) (string, error) {
	id, err := vaultitem.NewID()
	if err != nil {
		return "", err
	}
	p.ID = id
	resCh := make(chan string, 1)

	h.mu.Lock()
	if h.pending != nil {
		h.mu.Unlock()
		return "", errors.New("another interaction is already pending")
	}
	h.pending = &pendingWait{Pending: p, result: resCh}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.pending = nil
		h.mu.Unlock()
	}()

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

Refine `Headless` vs `prompter == nil`: tests that Complete via API still need a non-nil Prompter no-op:

```go
type NoopPrompter struct{}

func (NoopPrompter) Prompt(Pending) error { return nil }
```

Use `Headless` only for true headless; unit tests use `NoopPrompter`.

- [ ] **Step 4: Pass + commit**

```bash
go test ./internal/interaction/ -count=1
git add internal/interaction/
git commit -m "feat(interaction): selection and confirmation hub with timeouts"
```

---

### Task 4: SSH + Database agent active profile

**Files:**
- Create: `internal/ssh/active.go`, `internal/ssh/active_test.go`
- Create: `internal/database/active.go`, `internal/database/active_test.go`
- Create: `internal/bridge/ssh_active.go`, `internal/bridge/database_active.go`
- Modify: `internal/app/container.go` (grants `profile:active`)
- Modify: `internal/app/app.go` (manifest capability strings if listed)

- [ ] **Step 1: Failing service tests**

```go
func TestSSHAgentActiveProfileSticky(t *testing.T) {
	svc := newTestSSHService(t) // existing helper
	if id := svc.AgentActiveProfileID(); id != "" {
		t.Fatalf("want empty, got %q", id)
	}
	// create a real profile via CreateProfile in helper, capture summary.ID
	id := createSSHProfile(t, svc, "dev")
	if err := svc.SetAgentActiveProfile(id); err != nil {
		t.Fatal(err)
	}
	if svc.AgentActiveProfileID() != id {
		t.Fatal("sticky id mismatch")
	}
	if err := svc.SetAgentActiveProfile("deadbeefdeadbeefdeadbeef"); err == nil {
		t.Fatal("unknown id must fail")
	}
}
```

Mirror for database.

- [ ] **Step 2: Implement on services**

```go
// internal/ssh/active.go
func (s *Service) AgentActiveProfileID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentActiveID
}

func (s *Service) SetAgentActiveProfile(id string) error {
	if id == "" {
		s.mu.Lock()
		s.agentActiveID = ""
		s.mu.Unlock()
		return nil
	}
	if _, err := s.store.Get(id); err != nil {
		return err
	}
	s.mu.Lock()
	s.agentActiveID = id
	s.mu.Unlock()
	return nil
}
```

Add `agentActiveID string` to `Service` struct (under existing mutex). Same pattern for `internal/database`.

**UI unchanged:** `SSHRunCommand(ctx, id, ...)` and `DBQuery(ctx, id, sql)` keep requiring explicit `id`.

- [ ] **Step 3: Bridge methods**

```go
func (s *AppService) SSHActiveProfile() (map[string]any, error) {
	_, err := s.call(ctx..., "builtin.ssh", "ssh:connect", "profile:active", map[string]any{
		"action": "active_profile",
	}, func(ctx context.Context) (any, error) {
		id := s.container.SSH.AgentActiveProfileID()
		out := map[string]any{}
		if id != "" {
			out["profileId"] = id
		}
		return out, nil
	})
	// match existing bridge style (context.Background(), publicError already in call)
}
```

Follow the exact style of `KafkaConnectionStatus` for return types — prefer a small struct:

```go
type ActiveProfileDTO struct {
	ProfileID string `json:"profileId,omitempty"`
}
```

`SSHSetActiveProfile(id string)` / `DBSetActiveProfile(id string)` — **bridge only**, used by InteractionHost after user picks a profile.

- [ ] **Step 4: Grants**

In `container.go` policy grants, add:

```go
{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:active"},
{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:active"},
```

- [ ] **Step 5: Test + commit**

```bash
go test ./internal/ssh/ ./internal/database/ ./internal/bridge/ ./internal/app/ -count=1
git add internal/ssh/active.go internal/ssh/active_test.go internal/database/active.go internal/database/active_test.go internal/bridge/ssh_active.go internal/bridge/database_active.go internal/app/container.go
git commit -m "feat(ssh,database): agent sticky active profile for MCP"
```

---

### Task 5: MCP gate helper (ensure active + env confirmation)

**Files:**
- Create: `internal/mcp/gate.go`
- Create: `internal/mcp/gate_test.go`

This is the shared pre-flight used by SSH/DB/Kafka MCP tools that touch an active target.

- [ ] **Step 1: Define interfaces (local to mcp)**

```go
type EnvConfirmer interface {
	RequiresConfirmation(name string) bool
}

type ProfileEnvReader interface {
	// EnvForProfile returns metadata.env for the profile id.
	EnvForProfile(id string) (string, error)
}

type ActiveTarget interface {
	ActiveID() (id string, ok bool)
	// EnsureActive may block on interaction.Hub.SelectProfile then set active.
	EnsureActive(ctx context.Context) (id string, err error)
}
```

- [ ] **Step 2: Failing tests with fakes**

```go
func TestGateSkipsConfirmWhenDev(t *testing.T) {
	// active id set, env=dev → RequiresConfirmation false → Gate returns nil without Confirm call
}

func TestGateConfirmsWhenProd(t *testing.T) {
	// Confirm invoked once; deny → ErrConfirmationDenied
}

func TestGateFailClosedUnknownEnv(t *testing.T) {
	// env name missing from registry → confirmer returns true → Confirm required
}
```

- [ ] **Step 3: Implement**

```go
func GateActiveMCP(ctx context.Context, hub *interaction.Hub, env EnvConfirmer, profiles ProfileEnvReader, active ActiveTarget, tool, summary string, kind interaction.Kind) error {
	id, err := active.EnsureActive(ctx)
	if err != nil {
		return err
	}
	envName, err := profiles.EnvForProfile(id)
	if err != nil {
		return err
	}
	if !env.RequiresConfirmation(envName) {
		return nil
	}
	return hub.Confirm(ctx, interaction.ConfirmRequest{
		Env: envName, Tool: tool, Summary: summary, Kind: kind,
	})
}
```

For Kafka, `ActiveID`/`EnsureActive` wrap `Kafka.Status` / `Hub.SelectProfile` + `Kafka.Connect`.

For SSH/DB, `EnsureActive` = if `AgentActiveProfileID()==""` then select + `SetAgentActiveProfile`.

- [ ] **Step 4: Commit**

```bash
go test ./internal/mcp/ -run TestGate -count=1
git add internal/mcp/gate.go internal/mcp/gate_test.go
git commit -m "feat(mcp): active-target and env confirmation gate"
```

---

### Task 6: MCP server skeleton + first tools (vault/notes/tools/env)

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/origin.go`
- Create: `internal/mcp/tools_vault.go`
- Create: `internal/mcp/tools_notes.go`
- Create: `internal/mcp/tools_tools.go`
- Create: `internal/mcp/tools_env.go`
- Create: `internal/mcp/server_test.go`
- Modify: `go.mod` / `go.sum` via `go get`

- [ ] **Step 1: Add dependency**

```bash
go get github.com/modelcontextprotocol/go-sdk@latest
```

Pin a version ≥ v1.7 in go.mod after tidy.

- [ ] **Step 2: Failing test — notes.list via in-process server**

Use go-sdk client with in-memory transport if available; otherwise call tool handler functions directly through a test helper `InvokeTool(t, server, name, args)`.

Preferred approach for unit tests (avoid stdio flakiness):

```go
func TestNotesListTool(t *testing.T) {
	caller := &fakeCaller{/* allow builtin.notes notes:read note:list */}
	notes := &fakeNotes{list: []notes.Summary{{ID: "1", Title: "a"}}}
	srv := NewServer(Deps{Caller: caller, Notes: notes, /* ... */})
	out, err := srv.toolNotesList(context.Background(), &mcp.CallToolRequest{}, struct{}{})
	// Assert caller saw origin=mcp and output titles.
}
```

Also add one integration-style test that builds `mcp.NewServer`, `mcp.AddTool`, and uses `mcp.NewInMemoryTransports` **if** the SDK exposes them — check SDK at pin time. If not, handler-level tests are enough for Task 6; Task 8 covers proxy I/O.

- [ ] **Step 3: Implement server + tools**

```go
// origin.go
const OriginMCP = "mcp"

func meta(action string, extra map[string]any) map[string]any {
	m := map[string]any{"origin": OriginMCP, "action": action}
	for k, v := range extra {
		m[k] = v
	}
	return m
}
```

```go
// server.go
type Deps struct {
	Caller Caller // bridge.Caller
	Notes  NotesAPI
	Vault  VaultAPI
	Search SearchAPI
	Tools  ToolsAPI
	Envs   EnvAPI
	// later: SSH, DB, Kafka, Hub
}

type Server struct {
	deps Deps
	mcp  *mcp.Server
}

func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	ms := mcp.NewServer(&mcp.Implementation{Name: "devkit", Version: "0.1.0"}, nil)
	s.mcp = ms
	s.registerVault()
	s.registerNotes()
	s.registerTools()
	s.registerEnv()
	return s
}

func (s *Server) MCP() *mcp.Server { return s.mcp }
```

Example notes list tool:

```go
func (s *Server) registerNotes() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "notes.list",
		Description: "List encrypted notes (metadata)",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolNotesList)
}

func (s *Server) toolNotesList(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:read", "note:list", meta("list", nil), func(ctx context.Context) (any, error) {
		return s.deps.Notes.List()
	})
	if err != nil {
		return nil, nil, err // bridge already returns *apperrors.AppError
	}
	return nil, v, nil
}
```

Register `notes.search`, `notes.get`, `notes.create`, `notes.delete`, `vault.status`, `vault.search`, `tools.hash`, `env.listTags` the same way. Match input structs to bridge method args (`id`, `title`, `body`, `query`, hash payload fields — copy from existing bridge DTOs / tools API).

**Destructive annotations:**

```go
Annotations: &mcp.ToolAnnotations{
	ReadOnlyHint:    boolPtr(false),
	DestructiveHint: boolPtr(true),
},
```

Check SDK field types (`*bool` vs `bool`) at pin time and match exactly.

- [ ] **Step 4: arch bans**

In `arch_test.go`:

```go
pkg("mcp"): {pkg("storage"), pkg("syncstore"), pkg("sync"), pkg("crypto"), pkg("app")},
// domain must not import mcp:
pkg("ssh"): append(..., pkg("mcp")),
pkg("database"): append(..., pkg("mcp")),
pkg("kafka"): append(..., pkg("mcp")),
pkg("notes"): {pkg("mcp")}, // if not already covered
```

`mcp` may import `bridge` **only for the `Caller` interface** — prefer defining a local `Caller` interface in `internal/mcp` identical to `bridge.Caller` to avoid `mcp → bridge` if arch forbids it.

**Locked decision:** define `mcp.Caller` locally (duck-typed); `main`/`app` passes `*bridge.AppService`. Then arch ban: `mcp` must not import `bridge` or `app`.

- [ ] **Step 5: Test + commit**

```bash
go test ./internal/mcp/ ./internal/arch/ -count=1
git add go.mod go.sum internal/mcp/ internal/arch/arch_test.go
git commit -m "feat(mcp): server skeleton with vault notes tools env tools"
```

---

### Task 7: Register SSH / Database / Kafka MCP tools + wire gate

**Files:**
- Create: `internal/mcp/tools_ssh.go`, `tools_database.go`, `tools_kafka.go`
- Create: `internal/mcp/active_*.go` adapters implementing `ActiveTarget` / `ProfileEnvReader`
- Modify: `internal/mcp/server.go` `Deps` + `NewServer`
- Modify: `internal/bridge/errors.go` — map interaction errors

- [ ] **Step 1: Error mapping tests**

```go
func TestPublicErrorInteractionTimeout(t *testing.T) {
	err := publicError("builtin.ssh", interaction.ErrInteractionTimeout)
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "operation_cancelled" {
		t.Fatalf("%+v", err)
	}
}
```

Map:
- `ErrInteractionTimeout` → `operation_cancelled`
- `ErrConfirmationDenied` → `permission_denied` (or `operation_cancelled` — **locked:** `permission_denied`)
- `ErrSelectionRequired` / `ErrConfirmationRequired` → `validation_failed` with stable messages `"Selection required."` / `"Confirmation required."`

Because MCP uses `Caller.Call` → `publicError`, mapping must live in `bridge/errors.go` (or mcp maps before return). **Locked:** map inside MCP tool wrappers with `apperrors.New(...)` when `errors.Is`, so bridge UI paths stay unchanged; still add bridge mappings if interaction errors can surface via bridge Complete APIs.

- [ ] **Step 2: Implement Kafka active adapter**

```go
type kafkaActive struct {
	svc *kafka.Service
	hub *interaction.Hub
}

func (k kafkaActive) EnsureActive(ctx context.Context) (string, error) {
	st := k.svc.Status()
	if st.Connected && st.ProfileID != "" {
		return st.ProfileID, nil
	}
	id, err := k.hub.SelectProfile(ctx, interaction.SelectRequest{Kind: interaction.KindKafka})
	if err != nil {
		return "", err
	}
	if err := k.svc.Connect(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}
```

SSH/DB adapters set agent active id without necessarily opening a long-lived connection (DB/SSH already connect per call).

- [ ] **Step 3: Implement tools**

`ssh.runCommand` input: `{command: string, passphrase?: string}` — **no profileId**.  
`db.query` input: `{sql: string}` — **no profileId**.  
`kafka.peekMessages` input: `{topic, partition, limit?, offset?}`.  
`kafka.publishMessage` input: `{topic, key?, value|payload, partition?}` — match existing `kafka.PublishRequest` field names (`value` in Go API).

Each gated tool:

```go
if err := GateActiveMCP(ctx, s.deps.Hub, s.deps.Envs, sshEnvReader, sshActive, "ssh.runCommand", input.Command, interaction.KindSSH); err != nil {
	return nil, nil, mapInteractErr(err)
}
id := s.deps.SSH.AgentActiveProfileID()
// Caller.Call(... profile:active ..., handler → SSH.RunCommand(ctx, id, ...))
```

- [ ] **Step 4: Tests**

- Gate denied → tool error
- Not connected kafka + headless hub → selection required
- Happy path with fakes

- [ ] **Step 5: Commit**

```bash
go test ./internal/mcp/ ./internal/bridge/ -count=1
git add internal/mcp/ internal/bridge/errors.go
git commit -m "feat(mcp): SSH Database Kafka tools with active-target gate"
```

---

### Task 8: Socket host + `cmd/devkit-mcp` proxy

**Files:**
- Create: `internal/mcp/host.go`, `internal/mcp/frame.go`, `internal/mcp/host_test.go`
- Create: `cmd/devkit-mcp/main.go`
- Modify: `Taskfile.yml`
- Modify: `main.go` (desktop lifecycle)

**Framing (locked):** each socket message = `uint32 big-endian length` + payload bytes. Proxy copies stdio raw JSON-RPC stream by reading MCP transport on both sides **or** simpler: treat the Unix connection as the SDK transport.

**Preferred SDK path:** If `mcp.Server.Run(ctx, transport)` accepts a custom `mcp.Transport` built from `net.Conn`, use that on the host for each accepted connection. Proxy then:

```go
// cmd/devkit-mcp/main.go
conn, err := net.Dial("unix", socketPath())
// Bidirectional copy between conn and stdio WITHOUT reinterpretation if using raw JSON-RPC stream over the conn.
```

If SDK `StdioTransport` owns os.Stdin/Stdout, proxy should:

```text
os.Stdin  -> conn
conn      -> os.Stdout
```

using `io.Copy` in two goroutines (half-close aware). Host accepts conn and runs:

```go
server.Run(ctx, NewConnTransport(conn))
```

Implement `NewConnTransport` wrapping `net.Conn` as `io.ReadWriteCloser` compatible with whatever the SDK needs — inspect `mcp.StdioTransport` source at pin time and mirror it for an arbitrary `ReadWriteCloser`.

- [ ] **Step 1: Test host listen + dial echo / tool call**

Start hub-less server with fake notes tool; dial unix socket; send initialize/tools/list via SDK client if possible.

- [ ] **Step 2: Implement host**

```go
func ServeUnix(ctx context.Context, path string, s *Server) error {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		ln.Close()
		_ = os.Remove(path)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = s.MCP().Run(context.Background(), newRWTransport(c))
		}(conn)
	}
}
```

- [ ] **Step 3: Wire `main.go`**

After container + AppService construction, build `interaction.Hub` with a Wails Prompter, build `mcp.Server` deps from container services + `AppService` as Caller, start `ServeUnix` in a goroutine cancelled on app shutdown.

Socket path helper shared between host and `cmd/devkit-mcp`.

- [ ] **Step 4: Taskfile**

```yaml
mcp:
  desc: Stdio MCP proxy (requires running DevKit desktop with vault unlocked)
  cmds:
    - go run ./cmd/devkit-mcp
```

- [ ] **Step 5: Commit**

```bash
go test ./internal/mcp/ -count=1
git add internal/mcp/ cmd/devkit-mcp/ Taskfile.yml main.go
git commit -m "feat(mcp): unix socket host and stdio proxy command"
```

---

### Task 9: Bridge interaction APIs + frontend dialogs

**Files:**
- Create: `internal/bridge/interaction.go`
- Create: `frontend/src/bridge/interaction.ts`
- Create: `frontend/src/workbench/InteractionHost.tsx`
- Modify: `frontend/src/workbench/Workbench.tsx`
- Modify: `frontend/src/bridge/ssh.ts`, `database.ts`, `kafka.ts` — add setActive / connect helpers if missing
- Regenerate bindings: `wails3 generate bindings` (or hand-update `frontend/bindings/...` like prior modules)

- [ ] **Step 1: Bridge methods**

```go
func (s *AppService) InteractionPending() (*interaction.Pending, error)
func (s *AppService) InteractionComplete(id, value string) error
func (s *AppService) InteractionDeny(id string) error // Complete(id, "deny") or "" for select cancel
```

Gate with a new subject **or** reuse `builtin.vault`? **Locked:** add grants:

```go
{SubjectID: "builtin.interaction", Capability: "interaction:respond", Scope: "pending"},
```

And call() with that subject from bridge methods. Register grants in container. Frontend is still human-driven.

- [ ] **Step 2: Prompter emits event**

Use Wails v3 event API already in the project (search `Event` / `Emit` in Wails app setup). If none exists yet, Prompter can set an atomic “pending” flag and frontend polls `InteractionPending` every 500ms while mounted — **locked fallback:** polling every 500ms from `InteractionHost` (simpler, no new event subsystem). Prefer events if a one-liner exists in Wails v3; otherwise poll.

- [ ] **Step 3: UI**

`InteractionHost.tsx`:
- On pending `select` + kind ssh/db/kafka → modal listing profiles via existing `listProfiles` bridge; on confirm call `SSHSetActiveProfile` / `DBSetActiveProfile` / `KafkaConnect`, then `InteractionComplete(id, profileId)`.
- On pending `confirm` → modal showing env badge (prod/stg/dev colors), tool name, summary; Allow → `Complete(id,"allow")`; Deny → `Complete(id,"deny")`.

Use existing `frontend/src/components/ui/alert-dialog.tsx` / `dialog.tsx`.

- [ ] **Step 4: Frontend build**

```bash
cd frontend && bun run build
```

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/interaction.go internal/app/container.go frontend/
git commit -m "feat(ui): MCP selection and env confirmation dialogs"
```

---

### Task 10: Docs sync (base) + README/AGENTS notes

**Files (base repo):**
- `content/projects/dev-kit/mcp-agent.mdx` — Callout → Implemented (stdio + tools); note socket proxy
- `implementation-status.mdx` — MCP row → Implemented (or Partial if Code Graph tools absent — **locked: Implemented** for runtime+allowlist; Remaining = Resources/HTTP/tool Settings UI)
- Module pages SSH/DB/Kafka/Notes/Vault/Environments — MCP sections stay accurate; change “design” → “Implemented” where tools shipped
- `technical-contracts.mdx` — MCP column updates

**Files (dev-kit):**
- `README.md` — mention `task mcp` + “DevKit desktop must be running”
- `AGENTS.md` — cloud caveat: MCP popups need GUI; headless fails selection

- [ ] **Step 1: Edit base docs; commit & push base `main`** (PAT flow used previously)

- [ ] **Step 2: Edit README/AGENTS; commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document MCP stdio proxy and desktop host requirements"
```

---

### Task 11: Verification gate

- [ ] **Step 1: Go tests**

```bash
go test ./internal/mcp/ ./internal/interaction/ ./internal/ssh/ ./internal/database/ ./internal/kafka/ ./internal/bridge/ ./internal/environments/ ./internal/arch/ ./internal/app/ -count=1
go vet -tags server ./internal/mcp/ ./internal/interaction/ ./cmd/devkit-mcp/
gofmt -l internal/mcp internal/interaction internal/bridge/interaction.go internal/bridge/ssh_active.go internal/bridge/database_active.go cmd/devkit-mcp
```

Expected: all PASS; gofmt clean.

- [ ] **Step 2: Frontend**

```bash
cd frontend && bun run build
```

- [ ] **Step 3: Manual smoke (desktop available)**

1. Start DevKit GUI; unlock vault.
2. Confirm `mcp.sock` exists under config dir.
3. Configure a client:

```json
{
  "mcpServers": {
    "devkit": {
      "command": "task",
      "args": ["mcp"]
    }
  }
}
```

Or `go run ./cmd/devkit-mcp`.

4. `tools/list` → includes `notes.list`, excludes `vault.unlock`.
5. `notes.list` with locked vault in a **fresh** GUI before unlock → `vault_locked`.
6. Set Kafka/SSH/DB profiles; call gated tool without active → popup; Allow confirm on `prod`.

- [ ] **Step 4: Final commit if fixes needed**

```bash
git commit -m "fix(mcp): verification gate follow-ups"
```

---

## Self-review checklist (author)

| Spec item | Task |
|---|---|
| FR-1 stdio + go-sdk | 6, 8 |
| FR-2 Call + same subject/cap/scope | 1, 6, 7 |
| FR-3 tools/list ⊆ grants | 6 (register ∩ grants) |
| FR-4 origin=mcp | 1, 6 |
| FR-5 annotations | 6, 7 |
| FR-6 vault_locked | 6 (existing publicError) |
| NFR-1 stdio only | 8 |
| NFR-2 no OAuth | locked |
| NFR-3 no raw secrets | allowlist exclusions |
| NFR-4 hard exclusions | allowlist |
| Active target pattern | 4, 5, 7, 9 |
| Env confirmation | 2, 5, 7, 9 |
| Kafka MCP tools | 7 |
| Headless fail-fast | 3, 5 |

**Placeholder scan:** none intentional — SDK `*bool` annotation shapes and exact Wails event API must be verified at pin time (called out inline, not TBD).

**Type consistency:** `interaction.Kind` values `ssh|database|kafka`; Complete select returns profile id string; Confirm values `allow|deny`; origin constant `mcp`.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-11-mcp-module.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, spec + quality review between tasks  
2. **Inline Execution** — executing-plans in this session with checkpoints  

Which approach?
