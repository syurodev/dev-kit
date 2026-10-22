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
	reg.Register(resumableMock{rig.mock}) // reload needs a vendor that can reopen a conversation
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

func TestResumedSessionOnTheBuiltinOrAMissingAccountLeavesTheSpecAlone(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string) {
		t.Helper()
		if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		spec := rig.mock.LastSpec()
		if spec.AccountHome != "" || len(spec.AccountSharedPaths) != 0 {
			t.Errorf("%s: spec carries home %q / shared %v, want the built-in account", what, spec.AccountHome, spec.AccountSharedPaths)
		}
	}
	check("session on the built-in account")
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	check("session whose account row is gone")
}
