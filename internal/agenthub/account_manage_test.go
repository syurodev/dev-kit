package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	wireManage(rig.svc, dir, shared)
	return rig, dir, shared
}

// wireManage gives a service the folders and vendors accounts are added under.
func wireManage(svc *agenthub.Service, dir, shared string) {
	svc.AccountsDir = dir
	svc.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeCodex, agenthub.AgentTypeMock}
	svc.AccountSharedHome = func(agenthub.AgentType) string { return shared }
}

// restartManaged restarts the rig's service (no process, like the app after a
// restart) and wires it to the same folders as before.
func restartManaged(t *testing.T, rig *accountRig, dir, shared string) {
	t.Helper()
	rig.restart(t)
	wireManage(rig.svc, dir, shared)
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
	if err := rig.book.SaveUsage(context.Background(), acct.ID, []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 30}}, "observed", time.Now()); err != nil {
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
}

func TestTheBuiltInAccountCannotBeRenamedOrSharedHere(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	// Reading the vendor's accounts creates its built-in row on first use.
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeCodex); err != nil {
		t.Fatal(err)
	}
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeCodex)
	if _, err := rig.svc.RenameAccount(ctx, builtin, "Mine"); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("renaming the built-in account: %v, want ErrInvalidAccount", err)
	}
	if _, err := rig.svc.SetAccountShared(ctx, builtin, true); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("sharing the built-in account: %v, want ErrInvalidAccount", err)
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
	// Seed: listing creates the mock vendor's built-in row, which is what is removed below.
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
	if _, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeMock, "Second"); err != nil {
		t.Fatal(err)
	}
	list, err := rig.svc.ListAccounts(uiCtx(), agenthub.AgentTypeMock)
	if err != nil || len(list) != 3 || !list[0].Builtin() || list[2].ID != "mock-second" {
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
	rig, dir, shared := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	restartManaged(t, rig, dir, shared) // the new service holds no process, like one whose process was evicted
	// A session whose process was evicted still has its entry: no runtime, so the
	// account is not "in use", but the stale binding must not outlive the account.
	rig.svc.SetSessionAccountCacheForTest(sess.ID, acct.ID)
	rig.svc.SetAccountSwitchForTest(sess.ID, acct.ID)
	rig.svc.SetUsageSeenForTest(acct.ID)
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if got := rig.svc.SessionAccountCacheForTest(sess.ID); got != "" {
		t.Errorf("the service still says session runs as removed account %q", got)
	}
	if rig.svc.AccountSwitchKnownForTest(sess.ID) {
		t.Error("the service still remembers a move away from the removed account")
	}
	if rig.svc.UsageSeenKnownForTest(acct.ID) {
		t.Error("the service still remembers the usage last written for the removed account")
	}
}

func TestRemoveAccountDeletesOnlyTheCanonicalHome(t *testing.T) {
	rig, dir, _ := manageRig(t)
	ctx := uiCtx()
	work, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	other, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Other")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, home string }{
		{"the whole vendor folder", filepath.Join(dir, "codex")},
		{"another account's home", other.HomeDir},
	} {
		id := "codex-bad"
		if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeCodex, Label: "Bad", HomeDir: tc.home}); err != nil {
			t.Fatal(err)
		}
		if err := rig.svc.RemoveAccount(ctx, id); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, h := range []string{work.HomeDir, other.HomeDir} {
			if _, err := os.Stat(h); err != nil {
				t.Fatalf("%s: removing the row wiped %s: %v", tc.name, h, err)
			}
		}
	}
}

func TestRemoveAccountLeavesAHomeReachedThroughASymlinkAlone(t *testing.T) {
	rig, dir, _ := manageRig(t)
	outside := t.TempDir()
	precious := filepath.Join(outside, "auth.json")
	if err := os.WriteFile(precious, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The canonical path of the account's home is a link to somewhere else.
	if err := os.MkdirAll(filepath.Join(dir, "codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "codex", "codex-link")
	if err := os.Symlink(outside, home); err != nil {
		t.Fatal(err)
	}
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "codex-link", Vendor: agenthub.AgentTypeCodex, Label: "Link", HomeDir: home}); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(uiCtx(), "codex-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("a folder behind a symbolic link was deleted: %v", err)
	}
}

func TestRemoveAccountCountsASessionWhoseResumeIsUnderway(t *testing.T) {
	rig, dir, shared := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Busy")
	if err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	restartManaged(t, rig, dir, shared) // no process is published for the session yet
	// resumeRuntime records the account long before it publishes the runtime.
	rig.svc.SetSessionAccountCacheForTest(sess.ID, acct.ID)
	release := rig.svc.SetResumingForTest(sess.ID)
	if err := rig.svc.RemoveAccount(ctx, acct.ID); !errors.Is(err, agenthub.ErrAccountInUse) {
		t.Fatalf("a session mid-resume: %v, want ErrAccountInUse", err)
	}
	release()
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatalf("after the resume ended: %v", err)
	}
}

func TestRemoveAccountNeedsTheAccountsFolderBeforeDeletingTheRow(t *testing.T) {
	rig, _, _ := manageRig(t)
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	rig.svc.AccountsDir = ""
	if err := rig.svc.RemoveAccount(uiCtx(), acct.ID); !errors.Is(err, agenthub.ErrAccountsUnavailable) {
		t.Fatalf("no accounts folder: %v, want ErrAccountsUnavailable", err)
	}
	if _, err := rig.book.Get(context.Background(), acct.ID); err != nil {
		t.Fatalf("the row was deleted although the folder could not be: %v", err)
	}
}

func TestAddAccountDoesNotInheritALeftoverFolder(t *testing.T) {
	rig, dir, _ := manageRig(t)
	// A folder a failed removal or a crash left behind, with its old login.
	leftover := filepath.Join(dir, "codex", "codex-work")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "auth.json"), []byte("old login"), 0o600); err != nil {
		t.Fatal(err)
	}
	acct, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID == "codex-work" {
		t.Fatalf("a new account took the id of a folder that still exists: %+v", acct)
	}
	if _, err := os.Stat(filepath.Join(acct.HomeDir, "auth.json")); !os.IsNotExist(err) {
		t.Errorf("the new account inherited an old login (stat error %v)", err)
	}
}

func TestAccountLabelsThatSlugAlikeGetDistinctIds(t *testing.T) {
	rig, _, _ := manageRig(t)
	a, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work!")
	if err != nil {
		t.Fatal(err)
	}
	b, err := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeCodex, "Work?")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.HomeDir == b.HomeDir {
		t.Fatalf("labels with one slug share an account: %+v, %+v", a, b)
	}
	if b.ID != "codex-work-2" {
		t.Errorf("second id = %q, want codex-work-2", b.ID)
	}
}

// slowBook holds the first Get and the first List of an account book until
// gate is closed, after telling started: the window in which a second
// management call used to slip between a read and its write.
type slowBook struct {
	agenthub.AccountBook
	gate    chan struct{}
	started chan struct{}
	once    sync.Once
}

func (b *slowBook) hold() {
	b.once.Do(func() {
		close(b.started)
		<-b.gate
	})
}

func (b *slowBook) Get(ctx context.Context, id string) (agenthub.AccountRecord, error) {
	a, err := b.AccountBook.Get(ctx, id)
	b.hold()
	return a, err
}

func (b *slowBook) List(ctx context.Context, v agenthub.AgentType) ([]agenthub.AccountRecord, error) {
	a, err := b.AccountBook.List(ctx, v)
	b.hold()
	return a, err
}

func TestRenameRacingRemoveNeverResurrectsTheAccount(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	slow := &slowBook{AccountBook: rig.book, gate: make(chan struct{}), started: make(chan struct{})}
	rig.svc.Accounts = slow
	var wg sync.WaitGroup
	var renameErr, removeErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, renameErr = rig.svc.RenameAccount(ctx, acct.ID, "Personal") }()
	<-slow.started // the rename has read the account
	go func() { defer wg.Done(); removeErr = rig.svc.RemoveAccount(ctx, acct.ID) }()
	time.Sleep(100 * time.Millisecond) // long enough for the removal to run if nothing stops it
	close(slow.gate)
	wg.Wait()
	_, getErr := rig.book.Get(context.Background(), acct.ID)
	_, statErr := os.Stat(acct.HomeDir)
	if removeErr == nil && getErr == nil {
		t.Fatalf("the account was resurrected after Remove succeeded (rename error %v)", renameErr)
	}
	if getErr == nil && statErr != nil {
		t.Fatalf("a row survives with its home gone: %v", statErr)
	}
}

// A sign-in check that lands between the rename's read and its write must survive:
// the rename writes the label alone, not the record it read.
func TestRenameAndShareKeepASignInThatMovedAfterTheRead(t *testing.T) {
	for _, op := range []string{"rename", "share"} {
		t.Run(op, func(t *testing.T) {
			rig, _, _ := manageRig(t)
			ctx := uiCtx()
			acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
			if err != nil {
				t.Fatal(err)
			}
			slow := &slowBook{AccountBook: rig.book, gate: make(chan struct{}), started: make(chan struct{})}
			rig.svc.Accounts = slow
			done := make(chan error, 1)
			go func() {
				var err error
				if op == "rename" {
					_, err = rig.svc.RenameAccount(ctx, acct.ID, "Personal")
				} else {
					_, err = rig.svc.SetAccountShared(ctx, acct.ID, true)
				}
				done <- err
			}()
			<-slow.started // the edit has read the account, signed out
			if err := rig.book.SetSignedIn(context.Background(), acct.ID, true); err != nil {
				t.Fatal(err)
			}
			close(slow.gate)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			got, _ := rig.book.Get(context.Background(), acct.ID)
			if !got.SignedIn {
				t.Fatalf("the edit wrote a stale sign-in state back: %+v", got)
			}
			if op == "rename" && got.Label != "Personal" || op == "share" && !got.Shared {
				t.Errorf("the edit itself was lost: %+v", got)
			}
		})
	}
}

func TestRenameAndShareOfARemovedAccountCreateNoRow(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.RenameAccount(ctx, acct.ID, "Back"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("rename: %v, want ErrAccountNotFound", err)
	}
	if _, err := rig.svc.SetAccountShared(ctx, acct.ID, true); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("share: %v, want ErrAccountNotFound", err)
	}
	if _, err := rig.book.Get(context.Background(), acct.ID); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("a row was created for a removed account: %v", err)
	}
}

// A hook that calls back into account management must not deadlock on accountMgmt.
func TestAccountsChangedHookRunsAfterManagementIsUnlocked(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	reentered := 0
	rig.svc.OnAccountsChanged = func() {
		if reentered++; reentered > 1 {
			return
		}
		if _, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Inner"); err != nil {
			t.Errorf("a hook could not call back in: %v", err)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Outer"); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the hook ran under accountMgmt and deadlocked")
	}
}

func TestConcurrentAddsOfOneSlugGetDifferentIds(t *testing.T) {
	rig, _, _ := manageRig(t)
	ctx := uiCtx()
	slow := &slowBook{AccountBook: rig.book, gate: make(chan struct{}), started: make(chan struct{})}
	rig.svc.Accounts = slow
	var wg sync.WaitGroup
	got := make([]agenthub.AccountRecord, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); got[0], errs[0] = rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work!") }()
	<-slow.started // the first add has listed the vendor's accounts
	go func() { defer wg.Done(); got[1], errs[1] = rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work?") }()
	time.Sleep(100 * time.Millisecond)
	close(slow.gate)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errors: %v, %v", errs[0], errs[1])
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("two adds got one id %q: the second overwrote the first", got[0].ID)
	}
	list, _ := rig.book.List(context.Background(), agenthub.AgentTypeCodex)
	if len(list) != 3 { // built-in + two
		t.Fatalf("accounts = %+v", list)
	}
}

// Listing creates the vendor's built-in row, so it only answers for a vendor the
// service knows: a caller-chosen string must not leave a row behind.
func TestListAccountsRefusesAnUnknownVendorAndWritesNothing(t *testing.T) {
	rig, _, _ := manageRig(t)
	if _, err := rig.svc.ListAccounts(uiCtx(), agenthub.AgentType("nope")); !errors.Is(err, agenthub.ErrVendorUnknown) {
		t.Fatalf("error = %v, want ErrVendorUnknown", err)
	}
	if _, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID("nope")); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("an unknown vendor left a built-in row behind: %v", err)
	}
}

// The row is already gone when the folder cannot be deleted, and the error says so with
// its own sentinel (not ErrAccountHome, whose public sentence says nothing was changed).
func TestRemoveAccountSaysWhenOnlyTheFolderWasLeft(t *testing.T) {
	rig, dir, _ := manageRig(t)
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeCodex, "Work")
	if err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(dir, string(agenthub.AgentTypeCodex))
	if err := os.Chmod(vendorDir, 0o500); err != nil { // the home cannot be unlinked from it
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(vendorDir, 0o700) })
	if f, err := os.Create(filepath.Join(vendorDir, "probe")); err == nil { // running as root: the test proves nothing
		f.Close()
		t.Skip("directory permissions are not enforced for this user")
	}
	err = rig.svc.RemoveAccount(ctx, acct.ID)
	if !errors.Is(err, agenthub.ErrAccountFolderLeft) {
		t.Fatalf("error = %v, want ErrAccountFolderLeft", err)
	}
	if errors.Is(err, agenthub.ErrAccountHome) {
		t.Errorf("a removal that already happened must not read as the Add path's \"nothing was changed\": %v", err)
	}
	if _, err := rig.book.Get(context.Background(), acct.ID); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("the row should be gone: %v", err)
	}
}
