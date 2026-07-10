package vault

import (
	"errors"
	"sync"

	"changeme/internal/crypto"
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
func (s *Service) Setup(masterPassword string) (string, error) {
	if len(masterPassword) < MinMasterPasswordLength {
		return "", errors.New("master password too short")
	}
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

	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
	return recoveryKey, nil
}

// Unlock decrypts the DEK using the Master Password.
func (s *Service) Unlock(masterPassword string) error {
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
	return nil
}

// UnlockWithRecovery decrypts the DEK using the recovery key.
func (s *Service) UnlockWithRecovery(recoveryKey string) error {
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithRecovery(recoveryKey)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
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

// Seal encrypts plaintext with the in-memory DEK. The DEK never leaves the
// vault: callers get a capability to encrypt, not the key itself. Fails if the
// vault is locked.
func (s *Service) Seal(keyID string, plaintext []byte) (crypto.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil {
		return crypto.Envelope{}, ErrLocked
	}
	return crypto.Seal(keyID, s.dek, plaintext)
}

// Open decrypts an envelope with the in-memory DEK. Fails if the vault is locked.
func (s *Service) Open(envelope crypto.Envelope) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dek == nil {
		return nil, ErrLocked
	}
	return crypto.Open(s.dek, envelope)
}
