package bridge

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/storage"
	"devkit/internal/terminal"
)

// newBridgeWithAgentAccounts is newBridgeWithAgentHub with the account book, a folder for
// added accounts and the real preferences wired in, as main.go does.
func newBridgeWithAgentAccounts(t *testing.T) *AppService {
	t.Helper()
	return newBridgeWithAgentAccountsAdapter(t, agenthub.NewMockAdapter())
}

func newBridgeWithAgentAccountsAdapter(t *testing.T, adapter agenthub.VendorAdapter) *AppService {
	t.Helper()
	svc, _ := newBridgeWithAgentHubAdapter(t, adapter)
	db, err := storage.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	svc.container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	hub := svc.container.AgentHub
	hub.Accounts = agenthub.NewSQLAccountStore(db)
	hub.AccountsDir = t.TempDir()
	hub.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeMock}
	shared := t.TempDir()
	hub.AccountSharedHome = func(agenthub.AgentType) string { return shared }
	hub.AccountPolicy = svc.container.Preferences.AgentAccountPolicy
	return svc
}

func TestAgentAccountsRoundTripThroughTheGateway(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	ctx := context.Background()
	added, err := svc.AgentAccountsAdd(ctx, "mock", "Work")
	if err != nil || added.ID != "mock-work" || added.Builtin || added.SignedIn {
		t.Fatalf("added = %+v, %v", added, err)
	}
	list, err := svc.AgentAccountsList(ctx, "mock")
	if err != nil || len(list) != 2 || !list[0].Builtin || list[1].ID != "mock-work" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[1].Usage == nil {
		t.Error("usage must be an empty list, not null, for the window")
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
	svc := newBridgeWithAgentAccounts(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	terminalCaller := identity.WithCaller(context.Background(), identity.UserTerminal())
	win := context.Background()
	added, err := svc.AgentAccountsAdd(win, "mock", "Work")
	if err != nil {
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
		"startSignIn": func(c context.Context) error {
			_, err := svc.AgentAccountsStartSignIn(c, "mock-work", "claude-ai", 24, 80)
			return err
		},
		"browserSignIn": func(c context.Context) error {
			_, err := svc.AgentAccountsStartBrowserSignIn(c, "mock-work")
			return err
		},
		"deviceSignIn": func(c context.Context) error {
			_, err := svc.AgentAccountsStartDeviceSignIn(c, "mock-work")
			return err
		},
		"policyGet": func(c context.Context) error { _, err := svc.AgentAccountPolicyGet(c); return err },
		"policySet": func(c context.Context) error {
			_, err := svc.AgentAccountPolicySet(c, AgentAccountPolicyState{}, false)
			return err
		},
	}
	// A fixed order, "remove" last: it deletes the account every other call names.
	names := make([]string, 0, len(calls))
	for name := range calls {
		if name != "remove" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	names = append(names, "remove")

	// Every refusal first, while the account still exists. uiOnly answers the bare
	// "Permission denied."; a gateway refusal (a grant DevKit forgot) tells a non-window
	// caller what was missing, so this exact text also says WHY the call was refused.
	for _, name := range names {
		for who, caller := range map[string]context.Context{"agent": agent, "terminal": terminalCaller} {
			err := calls[name](caller)
			if err == nil {
				t.Errorf("%s: a %s caller must be refused", name, who)
				continue
			}
			if publicCode(err) != "permission_denied" || err.Error() != "permission_denied: Permission denied." {
				t.Errorf("%s: a %s caller got %v, want the window-only refusal permission_denied: Permission denied.", name, who, err)
			}
		}
	}
	// None of those refusals changed anything.
	list, err := svc.AgentAccountsList(win, "mock")
	if err != nil || len(list) != 2 || list[1].ID != added.ID || list[1].Label != "Work" || list[1].Shared {
		t.Fatalf("a refused call changed the account: %+v, %v", list, err)
	}
	// Then the window: the real gateway must have the grants (a missing one is a
	// permission_denied). "remove" runs last.
	for _, name := range names {
		assertNotPermissionDenied(t, "window "+name, calls[name](win))
	}
}

// A caller-chosen id is a scope and an audit field: it must not carry anything
// but the shape DevKit itself generates.
func TestAccountScopeIDKeepsOnlyGeneratedIDs(t *testing.T) {
	for _, ok := range []string{"mock-work", "claude-default", "codex-a1"} {
		if got := accountScopeID(ok); got != ok {
			t.Errorf("accountScopeID(%q) = %q", ok, got)
		}
	}
	for _, bad := range []string{"", "Work Account", "x:*", "a/../b", "-lead", strings.Repeat("a", 65), "id\nnewline"} {
		if got := accountScopeID(bad); got != invalidOpaqueID {
			t.Errorf("accountScopeID(%q) = %q, want %q", bad, got, invalidOpaqueID)
		}
	}
	svc := newBridgeWithAgentAccounts(t)
	_, err := svc.AgentAccountsRename(context.Background(), "x:*", "Name")
	assertNotPermissionDenied(t, "rename with a hostile id", err)
	if err == nil {
		t.Error("an id that is not an account must be refused")
	}
}

func TestAgentAccountPolicyChecksTheRevisionAndOwnsTheRiskTimestamp(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
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
	if _, err := svc.AgentAccountPolicySet(ctx, enable, false); err == nil || !strings.Contains(err.Error(), "terms") {
		t.Fatalf("a caller-supplied timestamp must not count as the acknowledgement: %v", err)
	}
	saved, err := svc.AgentAccountPolicySet(ctx, enable, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.RiskAcknowledgedAt == "" || saved.RiskAcknowledgedAt == "1999-01-01T00:00:00Z" {
		t.Fatalf("saved = %+v, want the server's own timestamp", saved)
	}
	if v := saved.Vendors["claude"]; v.ThresholdPercent != 90 || v.UsageMaxAgeSeconds != 300 || v.OnLimitRefusal != "none" || v.Order == nil {
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
	bad := AgentAccountPolicyState{Revision: 2, Vendors: map[string]AgentAccountVendorPolicyState{"claude": {ThresholdPercent: 10}}}
	if _, err := svc.AgentAccountPolicySet(ctx, bad, false); err == nil || !strings.Contains(err.Error(), "between 50 and 100") {
		t.Errorf("an out-of-range threshold: %v", err)
	}
}

func TestAgentAccountPolicyCarriesTheStrategyAndAccountLimits(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	ctx := context.Background()
	state := AgentAccountPolicyState{Revision: 0, Vendors: map[string]AgentAccountVendorPolicyState{"claude": {
		Strategy: "consumeFirst", AccountThresholds: map[string]int{"claude-work": 70},
	}}}
	saved, err := svc.AgentAccountPolicySet(ctx, state, false)
	if err != nil {
		t.Fatal(err)
	}
	if v := saved.Vendors["claude"]; v.Strategy != "consumeFirst" || v.AccountThresholds["claude-work"] != 70 {
		t.Fatalf("saved = %+v, want the strategy and the account's limit back", v)
	}
	read, err := svc.AgentAccountPolicyGet(ctx)
	if err != nil || read.Vendors["claude"].Strategy != "consumeFirst" || read.Vendors["claude"].AccountThresholds["claude-work"] != 70 {
		t.Fatalf("read = %+v, %v", read, err)
	}
	// A vendor with no limits answers an empty map, never null, and an unset strategy reads as order.
	plain, err := svc.AgentAccountPolicySet(ctx, AgentAccountPolicyState{Revision: 1, Vendors: map[string]AgentAccountVendorPolicyState{"codex": {}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v := plain.Vendors["codex"]; v.Strategy != "order" || v.AccountThresholds == nil || len(v.AccountThresholds) != 0 {
		t.Errorf("codex = %+v, want strategy order and an empty limits map", v)
	}
	bad := AgentAccountPolicyState{Revision: 2, Vendors: map[string]AgentAccountVendorPolicyState{"claude": {Strategy: "random"}}}
	if _, err := svc.AgentAccountPolicySet(ctx, bad, false); err == nil || !strings.Contains(err.Error(), "strategy") {
		t.Errorf("an unknown strategy: %v", err)
	}
}

// A save drops the Service's cached policy read and tells open Accounts pages, so the
// rules apply at the next turn and not after the cache's few seconds.
func TestAgentAccountPolicySaveIsSeenByTheNextTurnAndAnnounced(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	hub := svc.container.AgentHub
	var reads, changed atomic.Int32
	hub.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		reads.Add(1)
		return svc.container.Preferences.AgentAccountPolicy()
	}
	hub.OnAccountsChanged = func() { changed.Add(1) }
	ctx := context.Background()
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() {
		t.Helper()
		if _, err := svc.AgentHubInvoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
			t.Fatal(err)
		}
	}
	invoke()
	first := reads.Load()
	if first == 0 {
		t.Fatal("a turn did not read the policy; the freshness check below proves nothing")
	}
	invoke()
	if got := reads.Load(); got != first {
		t.Fatalf("the second turn read the policy again (%d, %d): the cache is not in play", first, got)
	}
	before := changed.Load()
	if _, err := svc.AgentAccountPolicySet(ctx, AgentAccountPolicyState{Vendors: map[string]AgentAccountVendorPolicyState{"mock": {}}}, false); err != nil {
		t.Fatal(err)
	}
	if changed.Load() == before {
		t.Error("a policy save must announce the change so open Accounts pages refresh")
	}
	afterSave := reads.Load()
	invoke()
	if reads.Load() == afterSave {
		t.Error("the turn after a save served the old cached policy")
	}
}

func TestAgentAccountsNeedAnAgentHub(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	svc.container.AgentHub = nil
	ctx := context.Background()
	if _, err := svc.AgentAccountsList(ctx, "mock"); err == nil {
		t.Error("without an agent hub the call must fail, not panic")
	}
	if _, err := svc.AgentAccountsStartSignIn(ctx, "mock-work", "x", 24, 80); err == nil {
		t.Error("without an agent hub the sign-in must fail, not panic")
	}
	// The policy is a preference: saving it with no hub (server mode, tests) still works.
	if _, err := svc.AgentAccountPolicySet(ctx, AgentAccountPolicyState{}, false); err != nil {
		t.Errorf("policy save without a hub: %v", err)
	}
}

// A vendor that signs in through a terminal, with the checks the sign-in goroutine ends in.
type signInAdapter struct {
	*agenthub.MockAdapter
	path     string
	args     []string
	released atomic.Int32
	checks   atomic.Int32
}

func (a *signInAdapter) SignInCommandFor(_ context.Context, _, _ string) (agenthub.SignInCommand, error) {
	return agenthub.SignInCommand{Path: a.path, Args: a.args, Label: "fake", Release: func() { a.released.Add(1) }}, nil
}

func (a *signInAdapter) CheckSignIn(context.Context, agenthub.OpenSpec) (agenthub.SignInState, error) {
	a.checks.Add(1)
	return agenthub.SignInState{SignedIn: true}, nil
}

func newSignInBridge(t *testing.T, path string, args ...string) (*AppService, *signInAdapter) {
	t.Helper()
	adapter := &signInAdapter{MockAdapter: agenthub.NewMockAdapter(), path: path, args: args}
	svc := newBridgeWithAgentAccountsAdapter(t, adapter)
	svc.container.Terminal = terminal.NewRegistry(nil)
	return svc, adapter
}

// When the sign-in's terminal process ends, the command's Release runs exactly once
// and the account's status is read again (the fake's CheckSignIn, whose answer marks
// the account signed in and so announces the change).
func TestAgentAccountsSignInEndsWithReleaseAndAStatusRead(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no `true` binary to stand in for the login command")
	}
	svc, adapter := newSignInBridge(t, truePath)
	ctx := context.Background()
	if _, err := svc.AgentAccountsAdd(ctx, "mock", "Work"); err != nil {
		t.Fatal(err)
	}
	changed := make(chan struct{}, 4)
	svc.container.AgentHub.OnAccountsChanged = func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	id, err := svc.AgentAccountsStartSignIn(ctx, "mock-work", "any", 24, 80)
	if err != nil || id == "" {
		t.Fatalf("StartSignIn = %q, %v (a PTY may be unavailable here)", id, err)
	}
	select {
	case <-changed:
	case <-time.After(15 * time.Second):
		t.Fatal("the sign-in's exit did not lead to a status read")
	}
	if got := adapter.released.Load(); got != 1 {
		t.Errorf("Release ran %d times, want exactly 1", got)
	}
	if got := adapter.checks.Load(); got != 1 {
		t.Errorf("CheckSignIn ran %d times, want exactly 1", got)
	}
	acct, err := svc.container.AgentHub.Accounts.Get(ctx, "mock-work")
	if err != nil || !acct.SignedIn {
		t.Errorf("account after the sign-in = %+v, %v; want signed in", acct, err)
	}
}

// A command that cannot start still releases what it holds, once, and no status read
// follows (nothing signed in).
func TestAgentAccountsSignInReleasesWhenTheTerminalCannotStart(t *testing.T) {
	svc, adapter := newSignInBridge(t, filepath.Join(t.TempDir(), "no-such-login"))
	ctx := context.Background()
	if _, err := svc.AgentAccountsAdd(ctx, "mock", "Work"); err != nil {
		t.Fatal(err)
	}
	changed := make(chan struct{}, 1)
	svc.container.AgentHub.OnAccountsChanged = func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	if id, err := svc.AgentAccountsStartSignIn(ctx, "mock-work", "any", 24, 80); err == nil || id != "" {
		t.Fatalf("StartSignIn = %q, %v; want a refusal", id, err)
	}
	if got := adapter.released.Load(); got != 1 {
		t.Errorf("Release ran %d times, want exactly 1", got)
	}
	select {
	case <-changed:
		t.Error("a sign-in that never started read the account's status")
	case <-time.After(200 * time.Millisecond): // absence: nothing to wait on
	}
	if got := adapter.checks.Load(); got != 0 {
		t.Errorf("CheckSignIn ran %d times for a sign-in that never started", got)
	}
}

// The server-mode bridge is unauthenticated loopback where an unstamped call is the
// window, so any local process could fabricate the terms acknowledgement or start a
// login. Reads still work.
func TestAgentAccountsMutationsDoNotExistInServerMode(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	ctx := context.Background()
	added, err := svc.AgentAccountsAdd(ctx, "mock", "Work")
	if err != nil {
		t.Fatal(err)
	}
	svc.serverMode = func() bool { return true }
	svc.container.Terminal = terminal.NewRegistry(nil)
	mutations := map[string]func() error{
		"add":           func() error { _, err := svc.AgentAccountsAdd(ctx, "mock", "Other"); return err },
		"rename":        func() error { _, err := svc.AgentAccountsRename(ctx, added.ID, "X"); return err },
		"shared":        func() error { _, err := svc.AgentAccountsSetShared(ctx, added.ID, true); return err },
		"remove":        func() error { return svc.AgentAccountsRemove(ctx, added.ID) },
		"refreshStatus": func() error { _, err := svc.AgentAccountsRefreshStatus(ctx, added.ID); return err },
		"refreshUsage":  func() error { _, err := svc.AgentAccountsRefreshUsage(ctx, added.ID); return err },
		"startSignIn": func() error {
			_, err := svc.AgentAccountsStartSignIn(ctx, added.ID, "any", 24, 80)
			return err
		},
		"browserSignIn": func() error { _, err := svc.AgentAccountsStartBrowserSignIn(ctx, added.ID); return err },
		"deviceSignIn":  func() error { _, err := svc.AgentAccountsStartDeviceSignIn(ctx, added.ID); return err },
		"policySet": func() error {
			_, err := svc.AgentAccountPolicySet(ctx, AgentAccountPolicyState{Vendors: map[string]AgentAccountVendorPolicyState{"mock": {Enabled: true}}}, true)
			return err
		},
	}
	for name, call := range mutations {
		if err := call(); err == nil || !strings.Contains(err.Error(), "desktop app") {
			t.Errorf("%s in server mode = %v, want the desktop-app refusal", name, err)
		}
	}
	list, err := svc.AgentAccountsList(ctx, "mock")
	if err != nil || len(list) != 2 || list[1].Label != "Work" || list[1].Shared {
		t.Fatalf("a refused call changed the accounts: %+v, %v", list, err)
	}
	state, err := svc.AgentAccountPolicyGet(ctx)
	if err != nil || state.RiskAcknowledgedAt != "" || len(state.Vendors) != 0 {
		t.Fatalf("a refused policy save left %+v, %v", state, err)
	}
}

// Listing creates the vendor's built-in row, so a vendor the hub does not know is
// refused and leaves nothing behind.
func TestAgentAccountsListRefusesAnUnknownVendorAndWritesNoRow(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	ctx := context.Background()
	for _, vendor := range []string{"nope", "Claude", ""} {
		if _, err := svc.AgentAccountsList(ctx, vendor); err == nil {
			t.Errorf("AgentAccountsList(%q) succeeded", vendor)
		}
	}
	book := svc.container.AgentHub.Accounts
	for _, id := range []string{"nope-default", "invalid-default", "-default"} {
		if _, err := book.Get(ctx, id); !errors.Is(err, agenthub.ErrAccountNotFound) {
			t.Errorf("%s: %v; an unknown vendor left a built-in row behind", id, err)
		}
	}
	if _, err := svc.AgentAccountsRefreshStatus(ctx, "nope-default"); err == nil {
		t.Error("refreshing a vendor the hub does not know succeeded")
	}
	if _, err := book.Get(ctx, "nope-default"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("refresh left a row: %v", err)
	}
}
