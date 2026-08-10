package auth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	keyringServiceName = "io.synx.devkit"
	keyringTokenName   = "oidc-refresh-token"
)

// SystemRefreshTokenStore uses Keychain on macOS, Credential Manager on
// Windows and Secret Service on Linux. It deliberately has no file fallback:
// if the OS secret store is unavailable, the user signs in for that session.
type SystemRefreshTokenStore struct{}

func NewSystemRefreshTokenStore() *SystemRefreshTokenStore { return &SystemRefreshTokenStore{} }

func (*SystemRefreshTokenStore) Load() (string, bool, error) {
	token, err := keyring.Get(keyringServiceName, keyringTokenName)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (*SystemRefreshTokenStore) Save(token string) error {
	return keyring.Set(keyringServiceName, keyringTokenName, token)
}

func (*SystemRefreshTokenStore) Delete() error {
	err := keyring.Delete(keyringServiceName, keyringTokenName)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
