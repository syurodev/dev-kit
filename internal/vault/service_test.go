package vault

import (
	"testing"
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

func TestVaultSealOpenRoundTrip(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	env, err := service.Seal("note:1", []byte("secret body"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	plain, err := service.Open(env)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plain) != "secret body" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestVaultSealFailsWhenLocked(t *testing.T) {
	service := NewService(NewMemoryKeyringStore())
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	service.Lock()

	if _, err := service.Seal("note:1", []byte("x")); err == nil {
		t.Fatal("Seal must fail when locked")
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
