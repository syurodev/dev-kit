package bridge

import (
	"testing"

	"changeme/internal/app"
)

func TestVaultBridgeSetupAndStatus(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	status, err := service.VaultStatus()
	if err != nil {
		t.Fatalf("VaultStatus: %v", err)
	}
	if status.State != "not_configured" {
		t.Fatalf("state = %q, want not_configured", status.State)
	}

	recovery, err := service.SetupVault("master-password")
	if err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if recovery == "" {
		t.Fatal("SetupVault must return a recovery key")
	}

	status, _ = service.VaultStatus()
	if status.State != "unlocked" {
		t.Fatalf("state = %q, want unlocked", status.State)
	}
}

func TestVaultBridgeLockThenUnlock(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if err := service.LockVault(); err != nil {
		t.Fatalf("LockVault: %v", err)
	}
	if err := service.UnlockVault("master-password"); err != nil {
		t.Fatalf("UnlockVault: %v", err)
	}
	status, _ := service.VaultStatus()
	if status.State != "unlocked" {
		t.Fatalf("state = %q, want unlocked", status.State)
	}
}
