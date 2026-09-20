package kafka

import "devkit/internal/vaultitem"

// CredentialStore keeps a cluster's SASL secret outside the profile. Declared
// here rather than imported so this package never depends on
// internal/accounts. Nil means the legacy path.
type CredentialStore interface {
	PutForOwner(ownerID, provider, label, user, secret string) error
	SecretForOwner(ownerID string) (user, secret string, found bool, err error)
	DeleteForOwner(ownerID string) error
}

// ProviderID is what a Kafka credential is filed under.
const ProviderID = "kafka"

// SetCredentialStore installs the store.
func (s *Service) SetCredentialStore(store CredentialStore) { s.credentials = store }

// secretFor reads a cluster's secret: the credential store when it has one,
// the payload as the legacy fallback. Nothing writes both.
func (s *Service) secretFor(record vaultitem.Record) ([]byte, error) {
	if s.credentials != nil {
		if _, secret, found, err := s.credentials.SecretForOwner(record.ID); err != nil {
			return nil, err
		} else if found {
			return []byte(secret), nil
		}
	}
	return s.store.OpenPayload(record, secretKeyID)
}
