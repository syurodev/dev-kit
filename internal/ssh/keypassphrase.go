package ssh

import (
	"encoding/json"
	"errors"

	"devkit/internal/vaultitem"
)

const (
	// A passphrase lives in its own vault item rather than inside the key's
	// payload. A record carries exactly one payload, so packing two secrets
	// into it would mean inventing an encoding for a field that older builds —
	// and anything already synced — would read back as the private key itself.
	// A separate item costs a lookup and breaks nothing.
	passphraseItemType = "ssh-key-passphrase"
	passphraseKeyID    = "ssh-key-passphrase"
)

// passphraseMetadata is the plaintext half: which profile the secret belongs
// to. The passphrase itself is the encrypted payload.
type passphraseMetadata struct {
	ProfileID string `json:"profile_id"`
}

// storeKeyPassphrase saves (or replaces) the passphrase for a profile.
func (s *Service) storeKeyPassphrase(profileID, passphrase string) error {
	// Replace rather than accumulate: two items for one profile would make
	// "which one is current" a question with no answer.
	if err := s.deleteKeyPassphrase(profileID); err != nil {
		return err
	}
	meta, err := json.Marshal(passphraseMetadata{ProfileID: profileID})
	if err != nil {
		return err
	}
	_, err = s.store.Create(passphraseItemType, passphraseKeyID, meta, []byte(passphrase))
	return err
}

// keyPassphraseRecord finds the stored passphrase item for a profile, if any.
func (s *Service) keyPassphraseRecord(profileID string) (vaultitem.Record, bool) {
	records, err := s.store.ListByType(passphraseItemType)
	if err != nil {
		return vaultitem.Record{}, false
	}
	for _, record := range records {
		var meta passphraseMetadata
		if err := json.Unmarshal(record.Metadata, &meta); err != nil {
			continue
		}
		if meta.ProfileID == profileID {
			return record, true
		}
	}
	return vaultitem.Record{}, false
}

// loadKeyPassphrase returns the stored passphrase, or "" when none is saved.
// A vault that cannot be opened reads as "none": the caller then reports
// ErrKeyPassphraseRequired, which is the honest answer either way.
func (s *Service) loadKeyPassphrase(profileID string) string {
	record, ok := s.keyPassphraseRecord(profileID)
	if !ok {
		return ""
	}
	secret, err := s.store.OpenPayload(record, passphraseKeyID)
	if err != nil {
		return ""
	}
	defer zeroBytes(secret)
	return string(secret)
}

// hasKeyPassphrase reports whether one is saved, without decrypting it — so the
// UI can say "saved" while the vault is locked.
func (s *Service) hasKeyPassphrase(profileID string) bool {
	_, ok := s.keyPassphraseRecord(profileID)
	return ok
}

// deleteKeyPassphrase removes the stored passphrase. Absent is success: this
// runs on profile delete, where most profiles never had one.
func (s *Service) deleteKeyPassphrase(profileID string) error {
	record, ok := s.keyPassphraseRecord(profileID)
	if !ok {
		return nil
	}
	if err := s.store.Delete(record.ID); err != nil && !errors.Is(err, vaultitem.ErrNotFound) {
		return err
	}
	return nil
}
