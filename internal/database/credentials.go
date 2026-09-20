package database

import "devkit/internal/vaultitem"

// CredentialStore keeps a profile's password outside the profile.
//
// Declared here rather than imported so this package never depends on
// internal/accounts — the same boundary it already keeps from environments.
// Nil means the legacy path: the password stays in the profile's own payload.
type CredentialStore interface {
	PutForOwner(ownerID, provider, label, user, secret string) error
	SecretForOwner(ownerID string) (user, secret string, found bool, err error)
	DeleteForOwner(ownerID string) error
}

// ProviderID is what a database credential is filed under in the credential
// store.
const ProviderID = "database"

// SetCredentialStore installs the store. Without one the service keeps using
// the profile payload, which is what an ephemeral container does.
func (s *Service) SetCredentialStore(store CredentialStore) { s.credentials = store }

// secretFor reads a profile's password.
//
// The credential store wins when it has one, because that is where new
// passwords go. The payload is the fallback for a profile written before the
// move, and for a machine where the migration has not run — it is a legacy
// path, not a second source of truth: nothing writes both.
func (s *Service) secretFor(record vaultitem.Record) ([]byte, error) {
	if s.credentials != nil {
		if _, secret, found, err := s.credentials.SecretForOwner(record.ID); err != nil {
			return nil, err
		} else if found {
			return []byte(secret), nil
		}
	}
	return s.store.OpenPayload(record, passwordKeyID)
}
