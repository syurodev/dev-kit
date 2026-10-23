package bridge

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

// newBridgeWithAgentAccounts is newBridgeWithAgentHub with the account book, a folder for
// added accounts and the real preferences wired in, as main.go does.
func newBridgeWithAgentAccounts(t *testing.T) *AppService {
	t.Helper()
	svc := newBridgeWithAgentHub(t)
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
	for name, call := range calls {
		if err := call(agent); err == nil {
			t.Errorf("%s: an agent caller must be refused", name)
		}
		if err := call(terminalCaller); err == nil {
			t.Errorf("%s: a terminal caller must be refused", name)
		}
		// The real gateway must have the grants: a missing one is a permission_denied.
		assertNotPermissionDenied(t, "window "+name, call(win))
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
