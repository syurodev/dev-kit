package vault

import (
	"errors"
	"sync"

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
	mu    sync.RWMutex
	store KeyringStore
	dek   []byte
}

func NewService(store KeyringStore) *Service {
	return &Service{store: store}
}

// Status reports whether the vault still needs setup, is locked, or unlocked.
func (s *Service) Status() (State, error) {
	s.mu.RLock()
	unlocked := s.dek != nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.store.Exists()
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("vault already configured")
	}

	keyring, recoveryKey, err := crypto.NewKeyring(masterPassword)
	if err != nil {
		return "", err
	}
	if err := s.store.Save(keyring); err != nil {
		return "", err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return "", err
	}

	s.dek = dek
	return recoveryKey, nil
}

// Unlock decrypts the DEK using the Master Password. Derivation happens under
// the service write lock so a concurrent Lock cannot complete mid-derivation
// and then be silently undone by the freshly installed DEK — after Lock
// returns, the vault stays locked until the next explicit Unlock.
func (s *Service) Unlock(masterPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return err
	}
	s.dek = dek
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
	s.mu.Lock()
	defer s.mu.Unlock()
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithRecovery(recoveryKey)
	if err != nil {
		return err
	}
	s.dek = dek
	return nil
}

// Lock zeroizes the in-memory DEK.
func (s *Service) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dek {
		s.dek[i] = 0
	}
	s.dek = nil
}

func (s *Service) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dek == nil
}

// ErrLocked is returned when a crypto operation is attempted while the vault is
// locked.
var ErrLocked = errors.New("vault is locked")

// SealV2 encrypts plaintext with a record-identity binding using the in-memory
// DEK. Callers must allocate the record ID before sealing.
func (s *Service) SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil {
		return crypto.Envelope{}, ErrLocked
	}
	return crypto.SealV2(associatedData, s.dek, plaintext)
}

// OpenWithAD decrypts a V2 envelope with its record-identity binding.
func (s *Service) OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil {
		return nil, ErrLocked
	}
	return crypto.OpenWithAD(s.dek, envelope, associatedData)
}
