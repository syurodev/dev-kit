package bridge

import (
	"context"

	"changeme/internal/vault"
)

type VaultStatusDTO struct {
	State string `json:"state"`
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
func (s *AppService) SetupVault(masterPassword string) (string, error) {
	result, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "setup",
	}, func(context.Context) (any, error) {
		return s.container.Vault.Setup(masterPassword)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) UnlockVault(masterPassword string) error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "unlock",
	}, func(context.Context) (any, error) {
		return nil, s.container.Vault.Unlock(masterPassword)
	})
	return err
}

func (s *AppService) UnlockVaultWithRecovery(recoveryKey string) error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "recovery_unlock",
	}, func(context.Context) (any, error) {
		return nil, s.container.Vault.UnlockWithRecovery(recoveryKey)
	})
	return err
}

func (s *AppService) LockVault() error {
	_, err := s.call(context.Background(), "builtin.vault", "vault:manage", "vault", map[string]any{
		"action": "lock",
	}, func(context.Context) (any, error) {
		s.container.Vault.Lock()
		return nil, nil
	})
	return err
}
