package bridge

type VaultStatusDTO struct {
	State string `json:"state"`
}

func (s *AppService) VaultStatus() (VaultStatusDTO, error) {
	state, err := s.container.Vault.Status()
	if err != nil {
		return VaultStatusDTO{}, err
	}
	return VaultStatusDTO{State: string(state)}, nil
}

// SetupVault creates the vault with a mandatory Master Password (first run
// only) and returns the recovery key to show the user exactly once.
func (s *AppService) SetupVault(masterPassword string) (string, error) {
	return s.container.Vault.Setup(masterPassword)
}

func (s *AppService) UnlockVault(masterPassword string) error {
	return s.container.Vault.Unlock(masterPassword)
}

func (s *AppService) UnlockVaultWithRecovery(recoveryKey string) error {
	return s.container.Vault.UnlockWithRecovery(recoveryKey)
}

func (s *AppService) LockVault() error {
	s.container.Vault.Lock()
	return nil
}
