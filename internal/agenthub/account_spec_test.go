package agenthub

import (
	"os"
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

// The jail's own preparation follows the account home: its policy files are
// reset like the built-in home's, and the link to the shared store is left alone.
func TestCodexJailPreparesTheAccountHome(t *testing.T) {
	builtin := t.TempDir()
	account := filepath.Join(t.TempDir(), "work")
	if err := PrepareAccountHome(AgentTypeCodex, account, builtin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(account, "config.toml"), []byte("approval_policy = \"never\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewCodexAdapter(CodexOptions{Home: builtin})
	spec := OpenSpec{AccountHome: account, AccountSharedPaths: SharedStorePaths(AgentTypeCodex, builtin)}
	pol, _, err := a.jail(t.TempDir(), launch{}, spec, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pol.DenyWritePaths, filepath.Join(account, "config.toml")) {
		t.Errorf("deny-write = %v, want the account home's config.toml", pol.DenyWritePaths)
	}
	if got, err := os.ReadFile(filepath.Join(account, "config.toml")); err != nil || string(got) == "approval_policy = \"never\"\n" {
		t.Errorf("account config.toml = %q, %v; want it reset", got, err)
	}
	if got, err := os.Readlink(filepath.Join(account, "sessions")); err != nil || got != filepath.Join(builtin, "sessions") {
		t.Errorf("sessions link = %q, %v; want the shared store", got, err)
	}
	if _, err := os.Stat(filepath.Join(builtin, "config.toml")); err == nil {
		t.Errorf("the built-in home was prepared for an account session")
	}
}

// An account session sees its own home and the shared store, and no other
// account's home or the built-in login.
func TestCodexJailHidesOtherHomesFromAnAccountSession(t *testing.T) {
	data := t.TempDir()
	builtin := filepath.Join(data, "agent-homes", "codex")
	account := filepath.Join(data, "agent-accounts", "codex", "work")
	other := filepath.Join(data, "agent-accounts", "codex", "other")
	for _, dir := range []string{builtin, other} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(builtin, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareAccountHome(AgentTypeCodex, account, builtin); err != nil {
		t.Fatal(err)
	}
	a := NewCodexAdapter(CodexOptions{Home: builtin})
	shared := SharedStorePaths(AgentTypeCodex, builtin)
	spec := OpenSpec{DenyReadPaths: []string{data}, AccountHome: account, AccountSharedPaths: shared}
	pol, _, err := a.jail(t.TempDir(), launch{}, spec, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{filepath.Join(builtin, "auth.json"), other} {
		if !slices.Contains(pol.DenyReadPaths, hidden) {
			t.Errorf("deny-read = %v, want %s hidden", pol.DenyReadPaths, hidden)
		}
	}
	for _, open := range append([]string{data, account, builtin}, shared...) {
		if slices.Contains(pol.DenyReadPaths, open) {
			t.Errorf("deny-read = %v must not hide %s", pol.DenyReadPaths, open)
		}
	}
}

// The built-in account keeps today's policy: the original carveOut (a keep that
// is itself denied stays denied), the built-in home granted, no account paths.
func TestCodexJailWithoutAccountIsUnchanged(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	data := t.TempDir()
	builtin := filepath.Join(data, "agent-homes", "codex")
	if err := os.MkdirAll(filepath.Join(data, "vault"), 0o700); err != nil {
		t.Fatal(err)
	}
	a := NewCodexAdapter(CodexOptions{Home: builtin})
	extra := t.TempDir()
	spec := OpenSpec{
		DenyReadPaths:       []string{data, builtin, filepath.Join(t.TempDir(), "missing")},
		ExtraReadWritePaths: []string{extra},
	}
	pol, _, err := a.jail(t.TempDir(), launch{}, spec, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Env["CODEX_HOME"] != builtin {
		t.Errorf("CODEX_HOME = %q, want the built-in home %q", pol.Env["CODEX_HOME"], builtin)
	}
	if want := uniqueAbsPaths(builtin, extra); !slices.Equal(pol.ReadWritePaths, want) {
		t.Errorf("read-write = %v, want %v", pol.ReadWritePaths, want)
	}
	want := append(carveOut(spec.DenyReadPaths, builtin), filepath.Join(host, ".codex"))
	if !slices.Equal(pol.DenyReadPaths, want) {
		t.Errorf("deny-read = %v, want %v", pol.DenyReadPaths, want)
	}
	// carveOut keeps a path that is itself denied; carveOutAll would grant it.
	if !slices.Contains(pol.DenyReadPaths, builtin) {
		t.Errorf("deny-read = %v lost the built-in home entry", pol.DenyReadPaths)
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
	for _, own := range claudeCredentialPaths(pol.Env["HOME"]) {
		if slices.Contains(pol.ReadWritePaths, own) {
			t.Errorf("read-write = %v must not grant the built-in login path %s", pol.ReadWritePaths, own)
		}
	}
}

func TestClaudeSandboxCarvesTheAccountOutOfTheDataDirectory(t *testing.T) {
	data := t.TempDir()
	account := filepath.Join(data, "agent-accounts", "claude", "work")
	other := filepath.Join(data, "agent-accounts", "claude", "other")
	builtin := filepath.Join(data, "agent-homes", "claude")
	for _, dir := range []string{account, other, builtin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	shared := filepath.Join(builtin, "projects")
	if err := os.MkdirAll(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	spec := OpenSpec{DenyReadPaths: []string{data}, AccountHome: account, AccountSharedPaths: []string{shared}}
	pol, _ := sandboxFor(t.TempDir(), launch{path: "/bin/true"}, spec)
	if !slices.Contains(pol.DenyReadPaths, other) {
		t.Errorf("deny-read = %v, want the other account hidden", pol.DenyReadPaths)
	}
	for _, open := range []string{data, account, shared} {
		if slices.Contains(pol.DenyReadPaths, open) {
			t.Errorf("deny-read = %v must not hide %s", pol.DenyReadPaths, open)
		}
	}
}

// The built-in account keeps today's policy: no CLAUDE_CONFIG_DIR, the login
// paths and the extra grants, and the deny list exactly as given (a path that
// is also a keep of some account stays denied).
func TestClaudeSandboxWithoutAccountIsUnchanged(t *testing.T) {
	extra := t.TempDir()
	deny := []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")}
	spec := OpenSpec{DenyReadPaths: deny, ExtraReadWritePaths: []string{extra}}
	pol, _ := sandboxFor(t.TempDir(), launch{path: "/bin/true"}, spec)
	if _, set := pol.Env["CLAUDE_CONFIG_DIR"]; set {
		t.Errorf("CLAUDE_CONFIG_DIR must not be set for the built-in account")
	}
	want := uniqueAbsPaths(append(claudeCredentialPaths(pol.Env["HOME"]), extra)...)
	if !slices.Equal(pol.ReadWritePaths, want) {
		t.Errorf("read-write = %v, want %v", pol.ReadWritePaths, want)
	}
	if !slices.Equal(pol.DenyReadPaths, deny) {
		t.Errorf("deny-read = %v, want %v", pol.DenyReadPaths, deny)
	}
	// A zero spec (the install probe) is still valid.
	zero, _ := sandboxFor(t.TempDir(), launch{path: "/bin/true"}, OpenSpec{})
	if _, set := zero.Env["CLAUDE_CONFIG_DIR"]; set || len(zero.DenyReadPaths) != 0 {
		t.Errorf("zero spec policy changed: env=%v deny=%v", zero.Env, zero.DenyReadPaths)
	}
}
