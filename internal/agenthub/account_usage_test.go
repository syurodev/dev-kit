package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

// usagePolicyFor is a policy with an entry for the vendor, as a person who
// configured accounts has.
func usagePolicyFor(vendor AgentType) func() (preferences.AgentAccountPolicy, error) {
	return func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(vendor): {Enabled: true},
		}}, nil
	}
}

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
	// The person configured the mock vendor (as for Claude or Codex): only then is usage observed.
	svc.AccountPolicy = usagePolicyFor(AgentTypeMock)
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

// usageUpdateAt is a usage_update with the context counter at used and the
// 5-hour window at pct.
func usageUpdateAt(used int, pct int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"sessionUpdate":"usage_update","used":%d,"size":100,
		"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":%d}}}`, used, pct))
}

var usageUpdate92 = usageUpdateAt(10, 92)

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

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

	r.wantFiveHour(t, 92)
}

func TestUsageOfABrandNewBuiltInAccountIsStored(t *testing.T) {
	r := newUsageRig(t)
	r.setAccount(BuiltinAccountID(AgentTypeMock))
	// No List yet: the built-in account has no row.
	if _, err := r.book.Get(r.ctx, BuiltinAccountID(AgentTypeMock)); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Get before List = %v, want ErrAccountNotFound", err)
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

	r.wantFiveHour(t, 92)
}

func TestUsageOfASessionWithNoRecordedAccountIsTheBuiltInAccounts(t *testing.T) {
	r := newUsageRig(t)
	// Exercise the legacy cache-miss fallback; newly opened runtimes now publish attribution.
	r.svc.mu.Lock()
	delete(r.svc.sessionAccount, r.sess.ID)
	r.svc.mu.Unlock()

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

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
	r.svc.mu.Lock()
	before := len(r.svc.sessionAccount) + len(r.svc.usageSeen)
	r.svc.mu.Unlock()
	r.svc.handleUpdate(r.ctx, "no-such-session", usageUpdate92)
	r.svc.mu.Lock()
	n := len(r.svc.sessionAccount) + len(r.svc.usageSeen)
	r.svc.mu.Unlock()
	if n != before {
		t.Errorf("an unknown session left %d entries behind", n)
	}
}

// countingBook counts SaveUsage calls and can make them fail.
type countingBook struct {
	AccountBook
	saves int
	fail  bool
}

func (c *countingBook) SaveUsage(ctx context.Context, id string, w []LimitWindow, source string, at time.Time) error {
	c.saves++
	if c.fail {
		return errors.New("disk full")
	}
	return c.AccountBook.SaveUsage(ctx, id, w, source, at)
}

func TestAnUnchangedUsageUpdateIsNotWrittenAgain(t *testing.T) {
	r := newUsageRig(t)
	book := &countingBook{AccountBook: r.book}
	r.svc.Accounts = book
	r.setAccount(BuiltinAccountID(AgentTypeMock))
	if _, err := r.book.List(r.ctx, AgentTypeMock); err != nil {
		t.Fatal(err)
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)
	// The context counter moves on every turn; the limits did not.
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdateAt(50, 92))
	if book.saves != 1 {
		t.Fatalf("SaveUsage calls = %d, want 1 (the first reading), none for repeats", book.saves)
	}

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdateAt(50, 93))
	if book.saves != 2 {
		t.Fatalf("SaveUsage calls = %d, want 2 after a changed percentage", book.saves)
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

	book.fail = true
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92) // must not panic or block
	book.fail = false
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

	if book.saves != 2 {
		t.Errorf("SaveUsage calls = %d, want 2 (the failure, then the retry)", book.saves)
	}
	r.wantFiveHour(t, 92)
}

func TestUsageOfADeletedAccountIsDroppedQuietlyAndNotRetried(t *testing.T) {
	r := newUsageRig(t)
	book := &countingBook{AccountBook: r.book}
	r.svc.Accounts = book
	// Not the session's built-in account, and no such row: it was deleted
	// while the session runs. "foo-default" must not be taken for a built-in.
	r.setAccount("foo-default")

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

	if book.saves != 1 {
		t.Errorf("SaveUsage calls = %d, want 1: one failed write, no retry, identical update skipped", book.saves)
	}
	if logged.Len() != 0 {
		t.Errorf("a deleted account logged %q, want nothing", logged.String())
	}
	if _, err := r.book.Get(r.ctx, "foo-default"); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("Get(foo-default) = %v, want no row created", err)
	}
	if _, err := r.book.Get(r.ctx, BuiltinAccountID(AgentTypeMock)); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("the session's built-in account got a row it was never asked for: %v", err)
	}
}

func TestAFailedUsageWriteIsLogged(t *testing.T) {
	r := newUsageRig(t)
	book := &countingBook{AccountBook: r.book, fail: true}
	r.svc.Accounts = book
	r.setAccount(BuiltinAccountID(AgentTypeMock))
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)

	if !strings.Contains(logged.String(), "saving usage") {
		t.Errorf("log = %q, want the failure", logged.String())
	}
}

func TestUsageUpdatesNeedNoAccountBook(t *testing.T) {
	r := newUsageRig(t)
	r.svc.Accounts = nil
	r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92) // no panic
}

func TestAnUnchangedUsageReadingIsNotAChange(t *testing.T) {
	svc := &Service{usageSeen: map[string]seenUsage{}}
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

// Without a policy entry for the vendor nothing is observed: no row for the
// built-in account, no SaveUsage, no dedupe entry, whatever the update carries.
func TestUsageIsNotObservedForAVendorThePersonDidNotConfigure(t *testing.T) {
	cases := map[string]func() (preferences.AgentAccountPolicy, error){
		"no policy": nil,
		"no entry for the vendor": func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
				string(AgentTypeCodex): {Enabled: true},
			}}, nil
		},
		"an unreadable policy": func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{}, errors.New("disk full")
		},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			r := newUsageRig(t)
			book := &countingBook{AccountBook: r.book}
			r.svc.Accounts = book
			r.svc.AccountPolicy = policy
			var logged bytes.Buffer
			log.SetOutput(&logged)
			defer log.SetOutput(os.Stderr)

			r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdate92)
			r.svc.handleUpdate(r.ctx, r.sess.ID, usageUpdateAt(50, 93))

			if book.saves != 0 {
				t.Errorf("SaveUsage calls = %d, want none", book.saves)
			}
			if _, err := r.book.Get(r.ctx, BuiltinAccountID(AgentTypeMock)); !errors.Is(err, ErrAccountNotFound) {
				t.Errorf("Get(built-in) = %v, want no row created", err)
			}
			r.svc.mu.Lock()
			seen := len(r.svc.usageSeen)
			r.svc.mu.Unlock()
			if seen != 0 {
				t.Errorf("usageSeen has %d entries, want none", seen)
			}
			if strings.Contains(logged.String(), "saving usage") {
				t.Errorf("log = %q, want no write attempted", logged.String())
			}
		})
	}
}

func TestAnIdenticalUsageReadingIsWrittenAgainAfterAMinute(t *testing.T) {
	svc := &Service{usageSeen: map[string]seenUsage{}}
	now := time.Unix(1_800_000_000, 0)
	svc.usageClock = func() time.Time { return now }
	w := []LimitWindow{{Label: "5-hour", UsedPercent: 50, ResetsAt: 1_900_000_000}}
	if !svc.usageChanged("a", w) {
		t.Fatal("the first reading is a change")
	}
	now = now.Add(30 * time.Second)
	if svc.usageChanged("a", w) {
		t.Error("the same reading 30 s later needs no write")
	}
	now = now.Add(31 * time.Second)
	if !svc.usageChanged("a", w) {
		t.Error("the same reading after a minute must be written so observed_at stays fresh")
	}
}
