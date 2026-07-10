package crypto

import (
	"bytes"
	"testing"
)

func TestKeyringUnlockWithPasswordAndRecovery(t *testing.T) {
	kr, recovery, err := NewKeyring("master-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if recovery == "" {
		t.Fatal("recovery key must be returned")
	}

	dek1, err := kr.UnlockWithPassword("master-pw")
	if err != nil {
		t.Fatalf("UnlockWithPassword: %v", err)
	}
	dek2, err := kr.UnlockWithRecovery(recovery)
	if err != nil {
		t.Fatalf("UnlockWithRecovery: %v", err)
	}
	if !bytes.Equal(dek1, dek2) {
		t.Fatal("password and recovery must unwrap the same DEK")
	}
}

func TestKeyringRejectsWrongPassword(t *testing.T) {
	kr, _, err := NewKeyring("master-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if _, err := kr.UnlockWithPassword("wrong"); err == nil {
		t.Fatal("wrong password must error")
	}
}

func TestKeyringRequiresMasterPassword(t *testing.T) {
	if _, _, err := NewKeyring(""); err == nil {
		t.Fatal("empty master password must error")
	}
}

func TestKeyringChangePasswordKeepsDEK(t *testing.T) {
	kr, _, err := NewKeyring("old-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	before, _ := kr.UnlockWithPassword("old-pw")

	if err := kr.ChangePassword("old-pw", "new-pw"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := kr.UnlockWithPassword("old-pw"); err == nil {
		t.Fatal("old password must stop working")
	}
	after, err := kr.UnlockWithPassword("new-pw")
	if err != nil {
		t.Fatalf("UnlockWithPassword(new): %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("DEK must stay the same after password change")
	}
}
