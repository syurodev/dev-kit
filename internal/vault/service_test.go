package vault

import (
	"testing"
	"time"

	"devkit/internal/crypto"
)

func TestVaultStartsNotConfigured(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	state, err := service.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != StateNotConfigured {
		t.Fatalf("state = %q, want %q", state, StateNotConfigured)
	}
}

func TestVaultSetupUnlocksAndReturnsRecovery(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())

	recovery, err := service.Setup("master-password")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if recovery == "" {
		t.Fatal("Setup must return a recovery key")
	}

	state, _ := service.Status()
	if state != StateUnlocked {
		t.Fatalf("state = %q, want unlocked", state)
	}
}

func TestVaultSetupRejectsSecondSetup(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := service.Setup("another-password"); err == nil {
		t.Fatal("second Setup must fail")
	}
}

func TestVaultSetupRejectsShortPassword(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("short"); err == nil {
		t.Fatal("short master password must fail")
	}
}

func TestVaultLockThenUnlock(t *testing.T) {
	store := NewMemoryKeyringStore()
	service := NewService(store)
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	service.Lock()
	if !service.IsLocked() {
		t.Fatal("vault should be locked")
	}

	if err := service.Unlock("wrong-password"); err == nil {
		t.Fatal("wrong password must fail unlock")
	}
	if err := service.Unlock("master-password"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if service.IsLocked() {
		t.Fatal("vault should be unlocked")
	}
}

func TestVaultSealV2OpenWithADRoundTrip(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	ad := crypto.AssociatedData{RecordType: "note", RecordID: "note-1", KeyID: "note-body"}
	env, err := service.SealV2(ad, []byte("secret body"))
	if err != nil {
		t.Fatalf("SealV2: %v", err)
	}
	plain, err := service.OpenWithAD(env, ad)
	if err != nil {
		t.Fatalf("OpenWithAD: %v", err)
	}
	if string(plain) != "secret body" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestVaultSealV2FailsWhenLocked(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	service.Lock()

	ad := crypto.AssociatedData{RecordType: "note", RecordID: "note-1", KeyID: "note-body"}
	if _, err := service.SealV2(ad, []byte("x")); err == nil {
		t.Fatal("SealV2 must fail when locked")
	}
}

func TestVaultUnlockWithRecovery(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	recovery, err := service.Setup("master-password")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	service.Lock()

	if err := service.UnlockWithRecovery(recovery); err != nil {
		t.Fatalf("UnlockWithRecovery: %v", err)
	}
	if service.IsLocked() {
		t.Fatal("vault should be unlocked after recovery")
	}
}

func TestVaultLockDuringUnlockDerivationStaysLocked(t *testing.T) {
	store := NewMemoryKeyringStore()
	service := NewService(store)
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	service.Lock()

	unlocked := make(chan error, 1)
	go func() {
		unlocked <- service.Unlock("master-password")
	}()
	// The Argon2 derivation runs for hundreds of milliseconds under the service
	// write lock, so a short delay guarantees Unlock is mid-derivation when
	// Lock is requested. Lock blocks until Unlock finishes, then zeroizes the
	// freshly installed DEK — the vault must end locked, never silently
	// re-unlocked by the in-flight derivation.
	time.Sleep(50 * time.Millisecond)
	service.Lock()
	if err := <-unlocked; err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if !service.IsLocked() {
		t.Fatal("vault must stay locked when Lock completes after an in-flight Unlock")
	}
}

func TestVaultConcurrentSetupSucceedsExactlyOnce(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	const attempts = 3
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			_, err := service.Setup("master-password")
			results <- err
		}()
	}
	successes := 0
	for i := 0; i < attempts; i++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Setup successes = %d, want exactly 1", successes)
	}
	if service.IsLocked() {
		t.Fatal("vault should be unlocked after the winning Setup")
	}
}
