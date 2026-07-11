package bridge

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"changeme/internal/app"
)

func TestVaultBridgeAuditsSuccessfulLifecycleTransitionsWithoutCredentials(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	const masterPassword = "master-password"

	recovery, err := service.SetupVault(masterPassword)
	if err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if err := service.LockVault(); err != nil {
		t.Fatalf("LockVault: %v", err)
	}
	if err := service.UnlockVault(masterPassword); err != nil {
		t.Fatalf("UnlockVault: %v", err)
	}
	if err := service.LockVault(); err != nil {
		t.Fatalf("LockVault: %v", err)
	}
	if err := service.UnlockVaultWithRecovery(recovery); err != nil {
		t.Fatalf("UnlockVaultWithRecovery: %v", err)
	}

	wantEvents := map[string]bool{
		"setup":           false,
		"lock":            false,
		"unlock":          false,
		"recovery_unlock": false,
	}
	for _, event := range container.Audit.Events() {
		if event.Capability != "vault:lifecycle" {
			continue
		}
		if event.SubjectID != "builtin.vault" || event.Scope != "vault" || event.Outcome != "allowed" {
			t.Fatalf("unexpected lifecycle audit event: %#v", event)
		}
		if !reflect.DeepEqual(event.Metadata, map[string]any{"event": event.Metadata["event"]}) {
			t.Fatalf("lifecycle audit metadata contains more than its event name: %#v", event.Metadata)
		}
		eventName, ok := event.Metadata["event"].(string)
		if _, exists := wantEvents[eventName]; !ok || !exists {
			t.Fatalf("unexpected lifecycle event name: %#v", event.Metadata)
		}
		wantEvents[eventName] = true
	}
	for eventName, found := range wantEvents {
		if !found {
			t.Fatalf("missing lifecycle audit event %q", eventName)
		}
	}

	auditText := fmt.Sprint(container.Audit.Events())
	for _, credential := range []string{masterPassword, recovery} {
		if strings.Contains(auditText, credential) {
			t.Fatalf("audit events leaked credential %q", credential)
		}
	}
}

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
