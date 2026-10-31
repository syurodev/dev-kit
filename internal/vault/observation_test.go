package vault

import (
	"sync"
	"testing"
	"time"

	"devkit/internal/crypto"
)

func TestObserveAfterRecoveryUnlockReturnsCurrentRevision(t *testing.T) {
	store := NewMemoryKeyringStore()
	first := NewService(store)
	recovery, err := first.Setup("master-password")
	if err != nil {
		t.Fatal(err)
	}
	first.Lock()
	if err := first.UnlockWithRecovery(recovery); err != nil {
		t.Fatal(err)
	}

	snapshot, sub := first.Observe()
	defer sub.Close()
	if snapshot.State != StateUnlocked || snapshot.Revision != 3 {
		t.Fatalf("snapshot = %#v, want unlocked revision 3", snapshot)
	}
	if snapshot.Incarnation == "" {
		t.Fatal("snapshot has no owner incarnation")
	}
	// An unlock before registration is represented by the consistent snapshot;
	// later transitions continue from its revision on the channel.
	first.Lock()
	select {
	case got := <-sub.Events:
		if got.Revision != snapshot.Revision+1 || got.State != StateLocked {
			t.Fatalf("event = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("lock transition was lost")
	}
}

func TestSlowSubscriberMustResyncWithoutBlockingVault(t *testing.T) {
	svc := NewService(NewMemoryKeyringStore())
	_, sub := svc.Observe()
	defer sub.Close()
	if _, err := svc.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	lockDone := make(chan struct{})
	go func() { svc.Lock(); close(lockDone) }()
	select {
	case <-lockDone:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked Lock")
	}
	if !sub.Lagging() {
		t.Fatal("overflowed subscriber was not marked lagging")
	}
	resynced := make(chan Snapshot, 1)
	go func() { resynced <- sub.Resync() }()
	select {
	case got := <-resynced:
		if got.Revision != svc.Snapshot().Revision {
			t.Fatalf("resync revision=%d current=%d", got.Revision, svc.Snapshot().Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("resync blocked behind slow subscriber")
	}
	if sub.Lagging() {
		t.Fatal("resync did not clear lagging state")
	}
}

func TestLockGuardRunsBeforeZeroAndCryptoClosesAtBoundary(t *testing.T) {
	svc := NewService(NewMemoryKeyringStore())
	if _, err := svc.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	key := svc.dek
	guardEntered, releaseGuard := make(chan struct{}), make(chan struct{})
	svc.RegisterLockGuard(func(snapshot Snapshot) {
		if snapshot.State != StateLocked {
			return
		}
		close(guardEntered)
		<-releaseGuard
	})
	lockDone := make(chan struct{})
	go func() { svc.Lock(); close(lockDone) }()
	select {
	case <-guardEntered:
	case <-time.After(time.Second):
		t.Fatal("lock guard did not run")
	}
	if !svc.IsLocked() {
		t.Fatal("vault crypto remained available during lock guard")
	}
	if _, err := svc.SealV2(crypto.AssociatedData{RecordType: "test", RecordID: "1", KeyID: "1"}, []byte("x")); err == nil {
		t.Fatal("seal succeeded after lock boundary began")
	}
	svc.mu.RLock()
	keyStillPresent := false
	for _, b := range svc.dek {
		keyStillPresent = keyStillPresent || b != 0
	}
	svc.mu.RUnlock()
	if !keyStillPresent {
		t.Fatal("DEK was zeroed before synchronous guard completed")
	}
	close(releaseGuard)
	select {
	case <-lockDone:
	case <-time.After(time.Second):
		t.Fatal("lock did not publish final snapshot")
	}
	if svc.Snapshot().State != StateLocked {
		t.Fatal("final snapshot is not locked")
	}
	for _, b := range key {
		if b != 0 {
			t.Fatal("DEK was not zeroed")
		}
	}
}

func TestObserveAndTransitionHaveNoRegistrationGap(t *testing.T) {
	for i := 0; i < 3; i++ {
		svc := NewService(NewMemoryKeyringStore())
		recovery, err := svc.Setup("master-password")
		if err != nil {
			t.Fatal(err)
		}
		svc.Lock()
		start := make(chan struct{})
		type observed struct {
			snapshot Snapshot
			sub      *Subscription
		}
		registered := make(chan observed, 1)
		unlockDone := make(chan error, 1)
		go func() { <-start; snap, sub := svc.Observe(); registered <- observed{snap, sub} }()
		go func() { <-start; unlockDone <- svc.UnlockWithRecovery(recovery) }()
		close(start)
		got := <-registered
		if err := <-unlockDone; err != nil {
			t.Fatal(err)
		}
		defer got.sub.Close()
		current := svc.Snapshot()
		if got.snapshot.Revision == current.Revision {
			if got.snapshot.State != StateUnlocked {
				t.Fatalf("snapshot=%#v current=%#v", got.snapshot, current)
			}
		} else {
			select {
			case event := <-got.sub.Events:
				if event.Revision != got.snapshot.Revision+1 || event.Revision != current.Revision || event.State != StateUnlocked {
					t.Fatalf("snapshot=%#v event=%#v current=%#v", got.snapshot, event, current)
				}
			case <-time.After(time.Second):
				t.Fatal("transition fell between observation and registration")
			}
		}
	}
}

func TestUnregisterWaitsForInFlightGuard(t *testing.T) {
	svc := NewService(NewMemoryKeyringStore())
	if _, err := svc.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	remove := svc.RegisterLockGuard(func(snapshot Snapshot) {
		if snapshot.State != StateLocked {
			return
		}
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		close(entered)
		<-release
	})
	lockDone := make(chan struct{})
	go func() { svc.Lock(); close(lockDone) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("guard did not start")
	}
	removed := make(chan struct{})
	go func() { remove(); close(removed) }()
	select {
	case <-removed:
		t.Fatal("unregister returned during callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-lockDone:
	case <-time.After(time.Second):
		t.Fatal("lock remained blocked")
	}
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("unregister remained blocked")
	}
	if err := svc.Unlock("master-password"); err != nil {
		t.Fatal(err)
	}
	svc.Lock()
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Fatalf("guard calls=%d after unregister, want 1", calls)
	}
}

func TestResyncCloseAndReaderAreSafeConcurrently(t *testing.T) {
	svc := NewService(NewMemoryKeyringStore())
	if _, err := svc.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	_, sub := svc.Observe()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case _, ok := <-sub.Events:
				if !ok {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			sub.Resync()
		}
	}()
	closeDone := make(chan struct{})
	go func() { sub.Close(); close(closeDone) }()
	svc.Lock()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("concurrent close blocked")
	}
	close(stop)
	wg.Wait()
	sub.Close()
}
