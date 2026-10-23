package agenthub

import (
	"sync"
	"testing"
	"time"

	"devkit/internal/preferences"
)

func TestThePolicyReadIsCachedBrieflyAndCanBeInvalidated(t *testing.T) {
	reads := 0
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		reads++
		return preferences.AgentAccountPolicy{Revision: int64(reads)}, nil
	}}
	now := time.Unix(1_800_000_000, 0)
	svc.policyClock = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, err := svc.cachedAccountPolicy(); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Fatalf("reads = %d, want one for five calls inside the TTL", reads)
	}
	now = now.Add(accountPolicyTTL + time.Second)
	got, _ := svc.cachedAccountPolicy()
	if reads != 2 || got.Revision != 2 {
		t.Errorf("after the TTL: reads=%d revision=%d, want a fresh read", reads, got.Revision)
	}
	svc.InvalidateAccountPolicy()
	if _, _ = svc.cachedAccountPolicy(); reads != 3 {
		t.Errorf("after InvalidateAccountPolicy: reads = %d, want 3", reads)
	}
}

func TestAPolicyReadErrorIsNotCached(t *testing.T) {
	calls := 0
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		calls++
		return preferences.AgentAccountPolicy{}, errAccountsUnavailableForTest
	}}
	svc.policyClock = time.Now
	_, _ = svc.cachedAccountPolicy()
	_, _ = svc.cachedAccountPolicy()
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (an error must be retried, not remembered)", calls)
	}
}

var errAccountsUnavailableForTest = ErrAccountsUnavailable

// A read that was in flight when the policy was invalidated must not be stored
// as if it were newer than the invalidation.
func TestAReadStartedBeforeAnInvalidationDoesNotOutliveIt(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	revision := int64(1)
	first := true
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		mu.Lock()
		r := revision
		wasFirst := first
		first = false
		mu.Unlock()
		if wasFirst {
			close(started)
			<-release
		}
		return preferences.AgentAccountPolicy{Revision: r}, nil
	}}
	svc.policyClock = time.Now
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.cachedAccountPolicy() // reads revision 1, then waits
	}()
	<-started
	mu.Lock()
	revision = 2 // Settings saved a new policy...
	mu.Unlock()
	svc.InvalidateAccountPolicy() // ...and told us
	close(release)
	<-done
	got, err := svc.cachedAccountPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 {
		t.Errorf("revision = %d, want 2: the stale in-flight read must not be cached over the invalidation", got.Revision)
	}
}

// What the cache returns is the caller's: mutating it must not reach the next read.
func TestMutatingAReturnedPolicyDoesNotChangeLaterReads(t *testing.T) {
	svc := &Service{AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			"claude": {Enabled: true, Order: []string{"a", "b"}},
		}}, nil
	}}
	svc.policyClock = func() time.Time { return time.Unix(1_800_000_000, 0) }
	first, err := svc.cachedAccountPolicy() // the read that fills the cache
	if err != nil {
		t.Fatal(err)
	}
	first.Vendors["claude"].Order[0] = "mutated"
	first.Vendors["codex"] = preferences.AccountVendorPolicy{Enabled: true}
	delete(first.Vendors, "claude")

	second, _ := svc.cachedAccountPolicy() // served from the cache
	if v := second.Vendors["claude"]; len(v.Order) == 2 {
		v.Order[1] = "mutated too"
	}
	third, _ := svc.cachedAccountPolicy()
	got, ok := third.Vendors["claude"]
	if !ok || len(third.Vendors) != 1 || len(got.Order) != 2 || got.Order[0] != "a" || got.Order[1] != "b" {
		t.Errorf("a later read = %+v, want the policy as the source gave it", third)
	}
}

// vendorConfigured and accountPolicyFor read through the cache, not around it.
func TestVendorConfiguredAndPolicyForShareOneCachedRead(t *testing.T) {
	reads := 0
	svc := &Service{Accounts: noBook{}, AccountPolicy: func() (preferences.AgentAccountPolicy, error) {
		reads++
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{"claude": {Enabled: true}}}, nil
	}}
	now := time.Unix(1_800_000_000, 0)
	svc.policyClock = func() time.Time { return now }
	if !svc.vendorConfigured("claude") {
		t.Fatal("claude has an entry")
	}
	if _, ok := svc.accountPolicyFor("claude"); !ok {
		t.Fatal("claude is enabled")
	}
	if reads != 1 {
		t.Errorf("policy source read %d times for two questions inside the TTL, want 1", reads)
	}
	svc.InvalidateAccountPolicy()
	svc.vendorConfigured("claude")
	if reads != 2 {
		t.Errorf("reads = %d after Invalidate, want 2", reads)
	}
	now = now.Add(accountPolicyTTL + time.Second)
	svc.accountPolicyFor("claude")
	if reads != 3 {
		t.Errorf("reads = %d after the TTL, want 3", reads)
	}
}

// Settings enables then disables a vendor; with Invalidate between, each change
// applies to the very next question, inside the TTL.
func TestEnablingThenDisablingAVendorTakesEffectAcrossTheCache(t *testing.T) {
	policy := preferences.AgentAccountPolicy{}
	svc := &Service{Accounts: noBook{}, AccountPolicy: func() (preferences.AgentAccountPolicy, error) { return policy, nil }}
	svc.policyClock = func() time.Time { return time.Unix(1_800_000_000, 0) } // time never moves
	if _, ok := svc.accountPolicyFor("claude"); ok || svc.vendorConfigured("claude") {
		t.Fatal("nothing configured yet")
	}
	policy = preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{"claude": {Enabled: true}}}
	svc.InvalidateAccountPolicy()
	if _, ok := svc.accountPolicyFor("claude"); !ok || !svc.vendorConfigured("claude") {
		t.Error("after enabling and invalidating, the vendor must be enabled and configured at once")
	}
	policy = preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{"claude": {Enabled: false}}}
	svc.InvalidateAccountPolicy()
	if _, ok := svc.accountPolicyFor("claude"); ok {
		t.Error("after disabling and invalidating, the vendor must not be enabled")
	}
	if !svc.vendorConfigured("claude") {
		t.Error("a disabled vendor still has an entry: configured")
	}
	policy = preferences.AgentAccountPolicy{}
	svc.InvalidateAccountPolicy()
	if svc.vendorConfigured("claude") {
		t.Error("after removing the entry the vendor is not configured")
	}
}

// noBook is an AccountBook that is only present: the tests above never call it.
type noBook struct{ AccountBook }
