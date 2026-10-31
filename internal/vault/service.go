package vault

import (
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"

	"devkit/internal/crypto"
)

type State string

const (
	StateNotConfigured State = "not_configured"
	StateLocked        State = "locked"
	StateUnlocked      State = "unlocked"
)

// MinMasterPasswordLength is the minimum length enforced at setup.
// ponytail: length-only policy for now; add a strength meter later if needed.
const MinMasterPasswordLength = 8

// KeyringStore persists the vault keyring (wrapped DEK + KDF params).
type KeyringStore interface {
	Exists() (bool, error)
	Load() (*crypto.Keyring, error)
	Save(keyring *crypto.Keyring) error
}

// Service owns vault lock state and holds the decrypted DEK in memory only
// while unlocked. A Master Password is mandatory: a new install starts
// NotConfigured and must call Setup before anything can be unlocked.
type Service struct {
	lifecycleMu    sync.Mutex
	mu             sync.RWMutex
	store          KeyringStore
	dek            []byte
	locking        bool
	state          State
	incarnation    string
	revision       uint64
	nextGuard      uint64
	guards         map[uint64]func(Snapshot)
	subscribers    map[uint64]*Subscription
	nextSubscriber uint64
}

// Snapshot is a state-only, owner-versioned view of the vault lifecycle.
// Incarnation changes whenever a Service is created; Revision increases for
// each state change within that incarnation. Repeating an operation that leaves
// State unchanged does not increment Revision.
type Snapshot struct {
	Incarnation string `json:"incarnation"`
	Revision    uint64 `json:"revision"`
	State       State  `json:"state"`
}

// Subscription receives bounded, state-only lifecycle notifications. Once
// Lagging is true, callers must use Resync and treat queued events as stale.
type Subscription struct {
	owner   *Service
	id      uint64
	Events  <-chan Snapshot
	ch      chan Snapshot
	lagging atomic.Bool
	closed  atomic.Bool
}

const subscriberBufferSize = 1

// Lagging reports whether notifications overflowed. The caller must resync.
func (sub *Subscription) Lagging() bool { return sub == nil || sub.lagging.Load() }

// Resync atomically reads the current snapshot, discards stale queued events,
// and resumes notifications from that revision.
func (sub *Subscription) Resync() Snapshot {
	if sub == nil || sub.owner == nil {
		return Snapshot{}
	}
	s := sub.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.closed.Load() {
		return s.snapshotLocked()
	}
	for {
		select {
		case <-sub.ch:
		default:
			goto drained
		}
	}
drained:
	sub.lagging.Store(false)
	return s.snapshotLocked()
}

// Close stops notifications. It is safe to call more than once.
func (sub *Subscription) Close() {
	if sub == nil || sub.owner == nil {
		return
	}
	s := sub.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.closed.CompareAndSwap(false, true) {
		delete(s.subscribers, sub.id)
		close(sub.ch)
	}
}

// NewService begins locked unless the store confirms that no keyring exists,
// in which case the observable state is not_configured. An Exists error is
// conservatively represented as locked; Status still reports the store error.
func NewService(store KeyringStore) *Service {
	inc := rand.Text()
	state := StateLocked
	if exists, err := store.Exists(); err == nil && !exists {
		state = StateNotConfigured
	}
	return &Service{store: store, state: state, incarnation: inc, guards: map[uint64]func(Snapshot){}, subscribers: map[uint64]*Subscription{}}
}

// Observe registers a bounded subscriber and returns its initial snapshot in
// one critical section, so no transition can fall between registration/read.
func (s *Service) Observe() (Snapshot, *Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSubscriber++
	ch := make(chan Snapshot, subscriberBufferSize)
	sub := &Subscription{owner: s, id: s.nextSubscriber, ch: ch, Events: ch}
	s.subscribers[sub.id] = sub
	return s.snapshotLocked(), sub
}

// Snapshot returns the current owner snapshot.
func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Service) snapshotLocked() Snapshot {
	return Snapshot{Incarnation: s.incarnation, Revision: s.revision, State: s.state}
}

// RegisterLockGuard installs a synchronous in-memory lifecycle guard and calls
// it immediately with the current snapshot. Guards then run for every state
// change, outside the crypto mutex but serialized by the lifecycle mutex. For
// Lock, the locked snapshot is passed before DEK zeroization. Guards must be
// bounded, nonblocking, perform no I/O, and must not call Service. Unregister
// waits for in-flight callbacks and must not be called from a guard.
func (s *Service) RegisterLockGuard(guard func(Snapshot)) func() {
	if guard == nil {
		return func() {}
	}
	s.lifecycleMu.Lock()
	s.mu.Lock()
	s.nextGuard++
	id := s.nextGuard
	s.guards[id] = guard
	snap := s.snapshotLocked()
	s.mu.Unlock()
	guard(snap)
	s.lifecycleMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { s.lifecycleMu.Lock(); s.mu.Lock(); delete(s.guards, id); s.mu.Unlock(); s.lifecycleMu.Unlock() })
	}
}

func (s *Service) transitionLocked(state State) ([]func(Snapshot), Snapshot) {
	if s.state == state {
		return nil, s.snapshotLocked()
	}
	s.revision++
	s.state = state
	snap := s.snapshotLocked()
	for _, sub := range s.subscribers {
		if sub.closed.Load() || sub.lagging.Load() {
			continue
		}
		select {
		case sub.ch <- snap:
		default:
			sub.lagging.Store(true)
		}
	}
	guards := make([]func(Snapshot), 0, len(s.guards))
	for _, guard := range s.guards {
		guards = append(guards, guard)
	}
	return guards, snap
}

func runGuards(guards []func(Snapshot), snap Snapshot) {
	for _, guard := range guards {
		guard(snap)
	}
}

// Status reports whether the vault still needs setup, is locked, or unlocked.
func (s *Service) Status() (State, error) {
	s.mu.RLock()
	unlocked := s.dek != nil && s.state == StateUnlocked && !s.locking
	s.mu.RUnlock()
	if unlocked {
		return StateUnlocked, nil
	}
	exists, err := s.store.Exists()
	if err != nil {
		return "", err
	}
	if !exists {
		return StateNotConfigured, nil
	}
	return StateLocked, nil
}

// Setup creates the vault on first run. It fails if a keyring already exists,
// enforcing that a Master Password is chosen exactly once. It returns the
// recovery key, which must be shown to the user and is never stored.
//
// The whole sequence — existence check, keyring derivation, persistence, and
// DEK installation — runs under the service write lock: a double-clicked Setup
// cannot replace a keyring concurrently created, and Lock can never interleave
// between derivation and installation to leave an unlocked vault behind.
func (s *Service) Setup(masterPassword string) (string, error) {
	if len(masterPassword) < MinMasterPasswordLength {
		return "", errors.New("master password too short")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	exists, err := s.store.Exists()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if exists {
		s.mu.Unlock()
		return "", errors.New("vault already configured")
	}

	keyring, recoveryKey, err := crypto.NewKeyring(masterPassword)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if err := s.store.Save(keyring); err != nil {
		s.mu.Unlock()
		return "", err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}

	zero(s.dek)
	s.dek = dek
	guards, snap := s.transitionLocked(StateUnlocked)
	s.mu.Unlock()
	runGuards(guards, snap)
	return recoveryKey, nil
}

// Unlock decrypts the DEK using the Master Password. Derivation happens under
// the service write lock so a concurrent Lock cannot complete mid-derivation
// and then be silently undone by the freshly installed DEK — after Lock
// returns, the vault stays locked until the next explicit Unlock.
func (s *Service) Unlock(masterPassword string) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	keyring, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	zero(s.dek)
	s.dek = dek
	guards, snap := s.transitionLocked(StateUnlocked)
	s.mu.Unlock()
	runGuards(guards, snap)
	return nil
}

// VerifyMasterPassword checks a password without changing anything.
//
// It re-authenticates an action inside a session that is already unlocked —
// revealing a credential — which is not the same as unlocking. The derived key
// is zeroed and s.dek is never touched, so a wrong answer cannot lock a vault
// that is open and a right answer cannot open one that is not.
func (s *Service) VerifyMasterPassword(masterPassword string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return err
	}
	for i := range dek {
		dek[i] = 0
	}
	return nil
}

// UnlockWithRecovery decrypts the DEK using the recovery key, serialized
// against Lock exactly like Unlock.
func (s *Service) UnlockWithRecovery(recoveryKey string) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	keyring, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	dek, err := keyring.UnlockWithRecovery(recoveryKey)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	zero(s.dek)
	s.dek = dek
	guards, snap := s.transitionLocked(StateUnlocked)
	s.mu.Unlock()
	runGuards(guards, snap)
	return nil
}

// Lock zeroizes the in-memory DEK.
func (s *Service) Lock() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.dek == nil {
		s.mu.Unlock()
		return
	}
	s.locking = true
	snap := Snapshot{Incarnation: s.incarnation, Revision: s.revision + 1, State: StateLocked}
	guards := make([]func(Snapshot), 0, len(s.guards))
	for _, guard := range s.guards {
		guards = append(guards, guard)
	}
	s.mu.Unlock()
	runGuards(guards, snap)
	s.mu.Lock()
	zero(s.dek)
	s.dek = nil
	s.locking = false
	_, _ = s.transitionLocked(StateLocked)
	s.mu.Unlock()
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (s *Service) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state != StateUnlocked || s.dek == nil || s.locking
}

// ErrLocked is returned when a crypto operation is attempted while the vault is
// locked.
var ErrLocked = errors.New("vault is locked")

// SealV2 encrypts plaintext with a record-identity binding using the in-memory
// DEK. Callers must allocate the record ID before sealing.
func (s *Service) SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil || s.state != StateUnlocked || s.locking {
		return crypto.Envelope{}, ErrLocked
	}
	return crypto.SealV2(associatedData, s.dek, plaintext)
}

// OpenWithAD decrypts a V2 envelope with its record-identity binding.
func (s *Service) OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil || s.state != StateUnlocked || s.locking {
		return nil, ErrLocked
	}
	return crypto.OpenWithAD(s.dek, envelope, associatedData)
}
