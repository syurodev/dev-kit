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
