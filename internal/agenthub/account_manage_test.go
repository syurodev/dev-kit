package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func nowForTest() time.Time { return time.Now() }

func TestAddAccountHomeStaysInsideTheAccountsFolder(t *testing.T) {
	rig, dir, _ := manageRig(t)
	// A label full of path syntax is slugged, never a path.
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(acct.HomeDir, dir+string(filepath.Separator)) || strings.Contains(acct.ID, "..") || strings.ContainsAny(acct.ID, `/\`) {
		t.Fatalf("home %q (id %q) escapes %q", acct.HomeDir, acct.ID, dir)
	}
	// A vendor folder that is a symlink out of the accounts folder is refused,
	// and nothing is created behind it.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "mock")); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeMock, "Escape"); !errors.Is(err, agenthub.ErrAccountHome) {
		t.Fatalf("symlinked vendor folder: error = %v, want ErrAccountHome", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something was created outside the accounts folder: %v", entries)
	}
}

func TestRemoveAccountNeverDeletesOutsideTheAccountsFolder(t *testing.T) {
	rig, _, _ := manageRig(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "precious")
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	// A row whose home points elsewhere (written by an older build or by hand).
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "codex-stray", Vendor: agenthub.AgentTypeCodex, Label: "Stray", HomeDir: victim}); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(uiCtx(), "codex-stray"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a folder outside the accounts folder was deleted: %v", err)
	}
}

func TestRemoveAccountForgetsWhatTheServiceRemembersOfIt(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	dir, vendors, sharedHome := rig.svc.AccountsDir, rig.svc.AccountVendors, rig.svc.AccountSharedHome
	rig.restart(t) // the new service holds no process, like one whose process was evicted
	rig.svc.AccountsDir, rig.svc.AccountVendors, rig.svc.AccountSharedHome = dir, vendors, sharedHome
	// A session whose process was evicted still has its entry: no runtime, so the
	// account is not "in use", but the stale binding must not outlive the account.
	rig.svc.SetSessionAccountCacheForTest(sess.ID, acct.ID)
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if got := rig.svc.SessionAccountCacheForTest(sess.ID); got != "" {
		t.Errorf("the service still says session runs as removed account %q", got)
	}
}
