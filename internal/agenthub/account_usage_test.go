package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/identity"
	"devkit/internal/storage"
)

// usageRig is a service with the SQL account book and one mock session.
type usageRig struct {
	svc  *Service
	book *SQLAccountStore
	ctx  context.Context
	sess AgentSession
}

func newUsageRig(t *testing.T) *usageRig {
	t.Helper()
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
	return &usageRig{svc: svc, book: book, ctx: ctx, sess: sess}
}

func (r *usageRig) setAccount(id string) {
	r.svc.mu.Lock()
	r.svc.sessionAccount[r.sess.ID] = id
	r.svc.mu.Unlock()
}

const usageUpdate92 = `{"sessionUpdate":"usage_update","used":10,"size":100,
	"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":92}}}`

func (r *usageRig) wantFiveHour(t *testing.T, pct float64) {
	t.Helper()
	got, err := r.book.Get(r.ctx, BuiltinAccountID(AgentTypeMock))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Usage) != 1 || got.Usage[0].Label != "5-hour" || got.Usage[0].UsedPercent != pct || got.UsageSource != "observed" {
		t.Fatalf("account usage = %+v (source %q), want one 5-hour window at %v observed", got.Usage, got.UsageSource, pct)
	}
}

func TestUsageUpdatesAreMergedIntoTheSessionsAccount(t *testing.T) {
	r := newUsageRig(t)
	if _, err := r.book.List(r.ctx, AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	r.setAccount(BuiltinAccountID(AgentTypeMock))

	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))

	r.wantFiveHour(t, 92)
}

func TestUsageOfABrandNewBuiltInAccountIsStored(t *testing.T) {
	r := newUsageRig(t)
	r.setAccount(BuiltinAccountID(AgentTypeMock))
	// No List yet: the built-in account has no row.
	if _, err := r.book.Get(r.ctx, BuiltinAccountID(AgentTypeMock)); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Get before List = %v, want ErrAccountNotFound", err)
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))

	r.wantFiveHour(t, 92)
}

func TestUsageOfASessionWithNoRecordedAccountIsTheBuiltInAccounts(t *testing.T) {
	r := newUsageRig(t)
	// CreateSession's runtime is never recorded in sessionAccount.
	r.svc.mu.Lock()
	_, known := r.svc.sessionAccount[r.sess.ID]
	r.svc.mu.Unlock()
	if known {
		t.Fatal("test premise: CreateSession must not record an account")
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))

	r.wantFiveHour(t, 92)
	r.svc.mu.Lock()
	got := r.svc.sessionAccount[r.sess.ID]
	r.svc.mu.Unlock()
	if got != BuiltinAccountID(AgentTypeMock) {
		t.Errorf("sessionAccount = %q, want it cached as the built-in account", got)
	}
}

func TestUsageOfAnUnknownSessionIsDropped(t *testing.T) {
	r := newUsageRig(t)
	r.svc.handleUpdate(r.ctx, "no-such-session", json.RawMessage(usageUpdate92))
	r.svc.mu.Lock()
	n := len(r.svc.sessionAccount) + len(r.svc.usageSeen)
	r.svc.mu.Unlock()
	if n != 0 {
		t.Errorf("an unknown session left %d entries behind", n)
	}
}

// countingBook counts SaveUsage calls and can make them fail.
type countingBook struct {
	AccountBook
	saves atomic.Int32
	fail  atomic.Bool
}

func (c *countingBook) SaveUsage(ctx context.Context, id string, w []LimitWindow, source string, at time.Time) error {
	c.saves.Add(1)
	if c.fail.Load() {
		return errors.New("disk full")
	}
	return c.AccountBook.SaveUsage(ctx, id, w, source, at)
}

func TestAnUnchangedUsageUpdateIsNotWrittenAgain(t *testing.T) {
	r := newUsageRig(t)
	book := &countingBook{AccountBook: r.book}
	r.svc.Accounts = book
	r.setAccount(BuiltinAccountID(AgentTypeMock))

	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))
	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))
	// The context counter moves on every turn; the limits did not.
	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(
		`{"sessionUpdate":"usage_update","used":50,"size":100,"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":92}}}`))
	// 2 saves: the first attempt hit a missing built-in row, then the retry after List.
	if got := book.saves.Load(); got != 2 {
		t.Fatalf("SaveUsage calls = %d, want 2 (first reading + its retry after List), none for repeats", got)
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(
		`{"sessionUpdate":"usage_update","used":50,"size":100,"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":93}}}`))
	if got := book.saves.Load(); got != 3 {
		t.Fatalf("SaveUsage calls = %d, want 3 after a changed percentage", got)
	}
	r.wantFiveHour(t, 93)
}

func TestAFailedUsageWriteIsRetriedByTheNextIdenticalUpdate(t *testing.T) {
	r := newUsageRig(t)
	book := &countingBook{AccountBook: r.book}
	r.svc.Accounts = book
	r.setAccount(BuiltinAccountID(AgentTypeMock))
	if _, err := r.book.List(r.ctx, AgentTypeMock); err != nil {
		t.Fatal(err)
	}

	book.fail.Store(true)
	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92)) // must not panic or block
	book.fail.Store(false)
	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92))

	r.wantFiveHour(t, 92)
}

func TestUsageUpdatesNeedNoAccountBook(t *testing.T) {
	r := newUsageRig(t)
	r.svc.Accounts = nil
	r.svc.handleUpdate(r.ctx, r.sess.ID, json.RawMessage(usageUpdate92)) // no panic
}

func TestAnUnchangedUsageReadingIsNotAChange(t *testing.T) {
	svc := &Service{usageSeen: map[string]string{}}
	w := []LimitWindow{{Label: "5-hour", UsedPercent: 50, ResetsAt: 1900000000}}
	if !svc.usageChanged("a", w) {
		t.Fatal("the first reading is a change")
	}
	if svc.usageChanged("a", w) {
		t.Error("the same reading again must not be a change")
	}
	if !svc.usageChanged("b", w) {
		t.Error("another account's first reading is a change")
	}
	w[0].UsedPercent = 51
	if !svc.usageChanged("a", w) {
		t.Error("a different percentage is a change")
	}
}
