package bridge

import (
	"context"

	"devkit/internal/audit"
	"devkit/internal/vault"
)

type VaultStatusDTO struct {
	State string `json:"state"`
}

func (s *AppService) auditVaultLifecycle(event string) {
	s.container.Audit.Record(audit.Event{
		SubjectID:  "builtin.vault",
		Capability: "vault:lifecycle",
		Scope:      "vault",
		Outcome:    audit.OutcomeAllowed,
		Metadata: map[string]any{
			"event": event,
		},
	})
}

func (s *AppService) VaultStatus() (VaultStatusDTO, error) {
	result, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "status",
	}, func(context.Context) (any, error) {
		return s.container.Vault.Status()
	})
	if err != nil {
		return VaultStatusDTO{}, err
	}
	return VaultStatusDTO{State: string(result.(vault.State))}, nil
}

// SetupVault creates the vault with a mandatory Master Password (first run
// only) and returns the recovery key to show the user exactly once.
// SECURITY: This one-time return is the only exception; never log, audit,
// persist, or return the recovery key from any other bridge method.
func (s *AppService) SetupVault(masterPassword string) (string, error) {
	result, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "setup",
	}, func(context.Context) (any, error) {
		return s.container.Vault.Setup(masterPassword)
	})
	if err != nil {
		return "", err
	}
	s.auditVaultLifecycle("setup")
	// SECURITY: one-time return only; it is never logged, audited, or persisted.
	return result.(string), nil
}

func (s *AppService) UnlockVault(masterPassword string) error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "unlock",
	}, func(context.Context) (any, error) {
		return nil, s.container.Vault.Unlock(masterPassword)
	})
	if err == nil {
		s.auditVaultLifecycle("unlock")
	}
	return err
}

func (s *AppService) UnlockVaultWithRecovery(recoveryKey string) error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "recovery_unlock",
	}, func(context.Context) (any, error) {
		return nil, s.container.Vault.UnlockWithRecovery(recoveryKey)
	})
	if err == nil {
		s.auditVaultLifecycle("recovery_unlock")
	}
	return err
}

func (s *AppService) LockVault() error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "lock",
	}, func(context.Context) (any, error) {
		s.container.Vault.Lock()
		return nil, nil
	})
	if err == nil {
		s.auditVaultLifecycle("lock")
	}
	return err
}
