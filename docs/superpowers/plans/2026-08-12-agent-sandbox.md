# Agent Sandbox (`internal/sandbox`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a reusable Go package `internal/sandbox` that starts subprocesses under OS-enforced isolation (Linux **bubblewrap** + best-effort **Landlock**; macOS **Seatbelt** / `sandbox-exec`) so Agent Hub can later spawn vendor CLIs without full home-directory access.

**Architecture:** Leaf package — no imports of `bridge`, `mcp`, `app`, `storage`, or domain modules. Callers pass a `Policy` (workdir RW, extra RO/RW binds, env allowlist, network mode) and a command; the selected `Backend` rewrites the exec into `bwrap … -- cmd` or `sandbox-exec -p PROFILE cmd`. Tests use real `bwrap` when present (skip otherwise) plus a `fake` backend for unit logic. Agent Hub remains postponed until this package lands and a smoke spawn of an agent-shaped command passes.

**Tech Stack:** Go 1.26, `os/exec`, Linux `bwrap` (system package), optional Landlock via `kernel.org/pub/linux/libs/security/libcap/psx` **or** a tiny `golang.org/x/sys/unix` Landlock syscall wrapper — prefer **no CGO**. macOS Seatbelt profile generation as text SBPL. No Node/`srt` runtime dependency.

**Decision source:** `docs/superpowers/research/2026-08-12-agent-sandbox-oss.md` (locked 2026-08-12); ADR-009 on base.

**Explicitly out of scope (v1):**
- Domain-allowlist egress HTTP/SOCKS proxy (v1.1)
- Windows backend
- Firejail / gVisor / Firecracker / Docker
- Embedding `@anthropic-ai/sandbox-runtime`
- Wiring into Agent Hub UI (Hub plan resumes after this ships)
- Auto-install of `bwrap` for end users (detect + clear error; document `apt install bubblewrap`)

**Concrete defaults:**

| Item | Decision |
|---|---|
| Missing backend | `ErrUnavailable` — callers (Hub) fail closed for real vendors |
| Default denies (always overlay) | `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.config/gcloud` (path expand); never bind these even if caller asks unless `Policy.DangerousAllowSensitive` (tests only; Hub must not set) |
| Env | Clear env then set only `Policy.Env` + forced `HOME`/`TMPDIR` inside sandbox when using private home |
| Network v1 | `NetworkMode`: `none` (unshare-net / seatbelt deny) or `host` (inherit) |
| Private home | Default `true`: empty/tmp home inside jail; bind only explicit config dirs if policy lists them |
| Workdir | Required; must be absolute; mounted RW |
| System RO | `/usr`, `/bin`, `/lib`, `/lib64`, `/etc` (ro) on Linux as needed for dynamic loaders — mirror common bwrap agent recipes; keep list in one constant |
| Die with parent | `bwrap --die-with-parent` when available |
| Landlock | Apply after fork path if supported; if apply fails, log via returned `BackendInfo.Warnings` but still require bwrap success |
| macOS | Generate SBPL allow-list for workdir RW + system RO + optional network |

---

## File map

| Path | Responsibility |
|---|---|
| `internal/sandbox/policy.go` | `Policy`, `NetworkMode`, validation |
| `internal/sandbox/errors.go` | `ErrUnavailable`, `ErrUnsupportedPlatform`, `ErrInvalidPolicy` |
| `internal/sandbox/runner.go` | `Runner`, backend selection |
| `internal/sandbox/backend.go` | `Backend` interface |
| `internal/sandbox/backend_bwrap.go` | Linux bubblewrap (`//go:build linux`) |
| `internal/sandbox/backend_landlock_linux.go` | Optional Landlock helpers (`//go:build linux`) |
| `internal/sandbox/backend_seatbelt.go` | macOS Seatbelt (`//go:build darwin`) |
| `internal/sandbox/backend_unsupported.go` | Stub for other GOOS |
| `internal/sandbox/proc.go` | `Proc` wait/kill wrappers |
| `internal/sandbox/*_test.go` | Validation + integration tests |
| `internal/arch/arch_test.go` | Ban sandbox → bridge/mcp/app/storage/…; ban domain → nothing special |
| base ADR-009 | Record stack decision |

---

### Task 1: Policy types + validation

**Files:**
- Create: `internal/sandbox/policy.go`
- Create: `internal/sandbox/errors.go`
- Create: `internal/sandbox/policy_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestPolicyRequiresAbsoluteWorkDir(t *testing.T) {
	err := sandbox.Policy{WorkDir: "rel"}.Validate()
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyRejectsEmptyCommand(t *testing.T) {
	err := sandbox.Policy{WorkDir: t.TempDir()}.ValidateCmd(sandbox.Cmd{Path: ""})
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultSensitivePathsNormalized(t *testing.T) {
	p := sandbox.Policy{WorkDir: t.TempDir()}
	paths := p.EffectiveDenyRead() // includes ~/.ssh etc expanded
	if len(paths) == 0 {
		t.Fatal("expected default denies")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/sandbox/ -count=1
```

- [ ] **Step 3: Implement**

```go
package sandbox

type NetworkMode string

const (
	NetworkNone NetworkMode = "none"
	NetworkHost NetworkMode = "host"
)

type Policy struct {
	WorkDir                 string
	ReadOnlyPaths           []string
	ReadWritePaths          []string // extra RW beyond WorkDir
	DenyReadPaths           []string // added on top of defaults
	Env                     map[string]string
	Network                 NetworkMode // default none
	PrivateHome             bool        // default true
	DangerousAllowSensitive bool        // default false; test-only escape
}

type Cmd struct {
	Path string
	Args []string // without Path
	Dir  string   // optional; default WorkDir
}

func (p Policy) Validate() error
func (p Policy) ValidateCmd(c Cmd) error
func (p Policy) EffectiveDenyRead() []string
func (p Policy) withDefaults() Policy // Network empty → none; PrivateHome default true
```

- [ ] **Step 4: PASS + commit**

```bash
go test ./internal/sandbox/ -count=1
git add internal/sandbox/
git commit -m "feat(sandbox): policy types and validation"
```

---

### Task 2: Runner + Backend interface + fake backend

**Files:**
- Create: `internal/sandbox/backend.go`
- Create: `internal/sandbox/runner.go`
- Create: `internal/sandbox/proc.go`
- Create: `internal/sandbox/backend_fake.go` (test-only or `fake` for unit tests in `_test.go`)
- Create: `internal/sandbox/runner_test.go`

- [ ] **Step 1: Failing test with injectable backend**

```go
func TestRunnerStartUsesBackend(t *testing.T) {
	var saw sandbox.Policy
	fake := &fakeBackend{start: func(ctx context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
		saw = p
		cmd := exec.CommandContext(ctx, "true")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return sandbox.NewProc(cmd), nil
	}}
	r := sandbox.NewRunner(fake)
	proc, err := r.Start(context.Background(), sandbox.Policy{WorkDir: t.TempDir()}, sandbox.Cmd{Path: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()
	if saw.WorkDir == "" {
		t.Fatal("backend not called")
	}
}
```

```go
type Backend interface {
	Name() string
	Available(context.Context) error // nil if usable
	Start(ctx context.Context, p Policy, c Cmd) (*Proc, error)
}

type Runner struct{ b Backend }

func NewRunner(b Backend) *Runner
func DefaultRunner() *Runner // platform default
func (r *Runner) Start(ctx context.Context, p Policy, c Cmd) (*Proc, error)
```

`Proc` wraps `*exec.Cmd`: `Wait`, `Kill`, `PID`, stdin/stdout/stderr accessors set by backend before Start.

- [ ] **Step 2–4: Implement, PASS, commit**

```bash
go test ./internal/sandbox/ -count=1
git add internal/sandbox/
git commit -m "feat(sandbox): Runner and Backend interface"
```

---

### Task 3: Linux bubblewrap backend

**Files:**
- Create: `internal/sandbox/backend_bwrap.go` (`//go:build linux`)
- Create: `internal/sandbox/backend_bwrap_test.go` (`//go:build linux`)
- Create: `internal/sandbox/bwrap_args.go` (`//go:build linux`) — pure arg builder testable without exec

- [ ] **Step 1: Unit-test arg builder (no bwrap required)**

```go
func TestBwrapArgsContainsWorkDirBindAndDenySSH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	_ = os.MkdirAll(ssh, 0o700)
	wd := t.TempDir()
	args := buildBwrapArgs(Policy{WorkDir: wd, Network: NetworkNone, PrivateHome: true}.withDefaults(), Cmd{Path: "/bin/true"})
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, wd) {
		t.Fatal(args)
	}
	// deny: bind empty tmpfs or --ro-bind /dev/null style over ssh — document chosen pattern
}
```

Chosen Linux filesystem pattern (lock):

1. `--unshare-user --unshare-pid --unshare-ipc --die-with-parent`
2. `--unshare-net` when `NetworkNone`
3. RO binds for `/usr`, `/bin`, `/lib`, `/lib64`, `/etc` (skip missing)
4. `--bind WorkDir WorkDir` (+ extras)
5. For each EffectiveDenyRead path that exists: `--tmpfs` that path **or** bind empty dir over it (prefer tmpfs when path is directory)
6. Private home: `--tmpfs $HOME` then re-bind only allowlisted paths under home if any
7. `--chdir WorkDir -- /path/to/cmd args…`

- [ ] **Step 2: Integration test — skip if no bwrap**

```go
func TestBwrapCannotReadDeniedSSH(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	secret := filepath.Join(home, ".ssh", "id_test")
	_ = os.MkdirAll(filepath.Dir(secret), 0o700)
	_ = os.WriteFile(secret, []byte("SECRET"), 0o600)
	wd := t.TempDir()
	r := sandbox.NewRunner(sandbox.NewBwrapBackend())
	// run: /bin/cat $secret → expect fail / empty
	proc, err := r.Start(context.Background(), sandbox.Policy{
		WorkDir: wd,
		Env:     map[string]string{"HOME": home, "PATH": "/usr/bin:/bin"},
		Network: sandbox.NetworkNone,
	}, sandbox.Cmd{Path: "/bin/cat", Args: []string{secret}})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(proc.Stdout()) // or CombinedOutput pattern via Wait
	err = proc.Wait()
	if err == nil && strings.Contains(string(out), "SECRET") {
		t.Fatal("sandbox leaked ssh secret")
	}
}

func TestBwrapCanWriteWorkDir(t *testing.T) {
	// echo hi > $wd/f && cat it
}
```

- [ ] **Step 3: Implement `NewBwrapBackend`, `Available` (`LookPath("bwrap")`), `Start`**

- [ ] **Step 4: PASS + commit**

```bash
go test ./internal/sandbox/ -count=1
git add internal/sandbox/
git commit -m "feat(sandbox): Linux bubblewrap backend"
```

---

### Task 4: Landlock best-effort (Linux)

**Files:**
- Create: `internal/sandbox/backend_landlock_linux.go`
- Create: `internal/sandbox/backend_landlock_linux_test.go`

- [ ] **Step 1: Test that `tryLandlock(policy)` returns `skipped` on unsupported kernel without failing Start**

Landlock v1 path-beneath rules: allow RO/RW for policy paths only. If `landlock` syscalls unavailable, return warning string, do not fail.

**Lock:** Landlock is applied **inside** the child after bwrap if we use a tiny helper `devkit-sandbox-helper` — **YAGNI for v1**: either (a) skip Landlock until helper exists, or (b) use pure Go Landlock in a forked child before exec **without** bwrap as fallback-only.

**v1 decision:** Implement Landlock as **optional pre-exec in a Go-started child** only when `DEVKIT_SANDBOX_LANDLOCK=1` **or** always try and ignore ENOSYS — but **bwrap remains mandatory** on Linux. Simplest path that still matches research: export `func LandlockAvailable() bool` + `func RestrictSelf(p Policy) error` called from an optional internal helper; wire call from `BwrapBackend` only if build tag `sandbox_landlock` — **actually keep it simpler:**

**Minimal v1:** document Landlock as follow-up; implement stub:

```go
func landlockRestrictSelf(p Policy) error { return errLandlockSkipped }
```

with a test that documents skip. Full Landlock in Task 4b only if time — **prefer implementing real RestrictSelf** using `unix.Landlock*` APIs from `golang.org/x/sys/unix` if present in module; otherwise stub with TODO comment referencing research.

If `golang.org/x/sys/unix` already in go.mod, implement real restrict for RW/RO paths; call from a test-only `StartLandlockOnly` backend for unit verification; **do not** block bwrap merge on Landlock perfection.

- [ ] **Step 2–4: Implement best-effort, commit**

```bash
git commit -m "feat(sandbox): best-effort Landlock helper on Linux"
```

---

### Task 5: macOS Seatbelt backend

**Files:**
- Create: `internal/sandbox/backend_seatbelt.go` (`//go:build darwin`)
- Create: `internal/sandbox/backend_seatbelt_test.go` (`//go:build darwin`)
- Create: `internal/sandbox/seatbelt_profile.go` (`//go:build darwin`)

- [ ] **Step 1: Profile golden test (can run on Linux with build ignore — put profile builder in file without darwin-only if pure string build)**

Prefer `seatbelt_profile.go` **without** build tag so Linux CI can test SBPL string contains workdir and denies network when `NetworkNone`.

```go
func TestSeatbeltProfileAllowsWorkDir(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone})
	if !strings.Contains(prof, "/tmp/proj") {
		t.Fatal(prof)
	}
	if !strings.Contains(prof, "network-outbound") && !strings.Contains(prof, "deny") {
		// assert network denied — match whatever SBPL form you generate
	}
}
```

- [ ] **Step 2: On Darwin integration test with `sandbox-exec`**

```go
//go:build darwin
func TestSeatbeltDeniesDotSSH(t *testing.T) { /* similar to bwrap test */ }
```

- [ ] **Step 3: `NewSeatbeltBackend` — write profile to temp file, `sandbox-exec -f profile -n cmd…` (flags per `man sandbox-exec`)**

- [ ] **Step 4: Commit**

```bash
git add internal/sandbox/
git commit -m "feat(sandbox): macOS Seatbelt backend"
```

---

### Task 6: Platform default runner + unsupported stubs

**Files:**
- Create: `internal/sandbox/backend_unsupported.go` (`//go:build !linux && !darwin`)
- Create: `internal/sandbox/default_linux.go`, `default_darwin.go`, `default_other.go`
- Modify: `internal/arch/arch_test.go`

- [ ] **Step 1: `DefaultRunner()` returns bwrap on linux, seatbelt on darwin, unavailable elsewhere**

```go
func TestDefaultRunnerAvailableOnCI(t *testing.T) {
	r := sandbox.DefaultRunner()
	err := r.Available(context.Background())
	if runtime.GOOS == "linux" {
		if err != nil {
			t.Skip("bwrap missing on this host:", err)
		}
	}
}
```

Arch bans:

```go
pkg("sandbox"): {pkg("bridge"), pkg("mcp"), pkg("app"), pkg("storage"), pkg("sync"), pkg("crypto"), pkg("agenthub"), pkg("acp")},
```

(agenthub/acp may not exist yet — only add bans for packages that exist; add agenthub/acp bans when those packages appear.)

- [ ] **Step 2–4: Implement, `go test ./internal/sandbox/ ./internal/arch/ -count=1`, commit**

```bash
git commit -m "feat(sandbox): DefaultRunner and arch leaf bans"
```

---

### Task 7: Smoke — agent-shaped command under jail

**Files:**
- Create: `internal/sandbox/smoke_test.go` (`//go:build linux`)
- Optional: `Taskfile.yml` target `sandbox:smoke`

- [ ] **Step 1: Test that node/npx or a stand-in works**

If `npx` missing, simulate agent with a small Go program written to workdir that reads `../outside.txt` (must fail) and writes `./inside.txt` (must succeed).

```go
func TestSmokeAgentShapedProcess(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	wd := filepath.Join(root, "proj")
	_ = os.MkdirAll(wd, 0o755)
	_ = os.WriteFile(filepath.Join(root, "outside.txt"), []byte("OUT"), 0o644)
	// compile or use python/node one-liner inside wd
	// assert cannot read outside, can write inside
}
```

- [ ] **Step 2: Document required host packages in comment + research doc “Ops” section**

```text
Linux: apt install bubblewrap
macOS: sandbox-exec (system)
```

- [ ] **Step 3: Commit**

```bash
git commit -m "test(sandbox): agent-shaped smoke under bwrap"
```

---

### Task 8: Docs sync + Hub plan pointer

**Files:**
- Modify: `docs/superpowers/plans/2026-08-12-agent-hub.md` — note sandbox package prerequisite path
- Modify: base ADR-009 (if not already merged)
- Modify: `roadmap.mdx` / `agent-hub.mdx` — “sandbox stack locked; implement `internal/sandbox` next”

- [ ] **Step 1: Add to Hub plan (top):**

```markdown
**Prerequisite:** `internal/sandbox` Implemented per `2026-08-12-agent-sandbox.md` (bwrap + Seatbelt). Claude adapter `Open()` must call `sandbox.DefaultRunner().Start(…)`.
```

- [ ] **Step 2: Commit desktop + push base**

```bash
git commit -m "docs: point Agent Hub at sandbox prerequisite"
```

---

### Task 9: Verification gate

```bash
go test ./internal/sandbox/ ./internal/arch/ -count=1
gofmt -l internal/sandbox
go vet ./internal/sandbox/
```

On Linux with bwrap: integration tests not skipped.  
Hello-world: Start `Policy{WorkDir: …}` + `Cmd{Path:"/bin/sh", Args:[]string{"-c", "echo ok > "$PWD/hi"}}` and read file back from host.

---

## Spec coverage

| Requirement | Task |
|---|---|
| bwrap Linux | 3 |
| Landlock best-effort | 4 |
| Seatbelt macOS | 5 |
| Allowlist policy + default deny ssh/aws | 1, 3 |
| Fail unavailable | 2, 6 |
| Leaf package / arch | 6 |
| Agent-shaped smoke | 7 |
| Unblocks Hub | 8 |

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-12-agent-sandbox.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task  
2. **Inline Execution** — this session with checkpoints  

Which approach?
