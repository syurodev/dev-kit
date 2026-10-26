package vault

import (
	"encoding/json"
	"errors"

	"devkit/internal/crypto"
)

const maxManagedRecordPlaintext = 1_000_000
const maxManagedRecordEnvelope = 1_500_000

// RecordBinding names the immutable identity supplied by a host-owned record.
// Applications should build RecordType from the record's complete scope and
// never accept these values directly from an untrusted client.
type RecordBinding struct {
	RecordType string
	RecordID   string
	KeyID      string
}

// SealRecord encrypts a bounded record body with the vault's V2 AEAD. The
// binding is authenticated as associated data and is not stored in the body.
func (s *Service) SealRecord(binding RecordBinding, plaintext []byte) ([]byte, error) {
	if s == nil || s.IsLocked() {
		return nil, errors.New("vault is locked")
	}
	if err := validateRecordBinding(binding); err != nil {
		return nil, err
	}
	if len(plaintext) == 0 || len(plaintext) > maxManagedRecordPlaintext {
		return nil, errors.New("vault record exceeds implementation bounds")
	}
	envelope, err := s.SealV2(recordAD(binding), plaintext)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxManagedRecordEnvelope {
		return nil, errors.New("vault record envelope exceeds implementation bounds")
	}
	return encoded, nil
}

// OpenRecord authenticates and decrypts a bounded record using the supplied
// host-owned binding. Any scope mismatch is rejected by the AEAD.
func (s *Service) OpenRecord(binding RecordBinding, encoded []byte) ([]byte, error) {
	if s == nil || s.IsLocked() {
		return nil, errors.New("vault is locked")
	}
	if err := validateRecordBinding(binding); err != nil {
		return nil, err
	}
	if len(encoded) == 0 || len(encoded) > maxManagedRecordEnvelope {
		return nil, errors.New("vault record envelope exceeds implementation bounds")
	}
	var envelope crypto.Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, errors.New("vault record envelope is invalid")
	}
	if err := envelope.Validate(); err != nil {
		return nil, errors.New("vault record envelope is invalid")
	}
	plain, err := s.OpenWithAD(envelope, recordAD(binding))
	if err != nil {
		return nil, err
	}
	if len(plain) == 0 || len(plain) > maxManagedRecordPlaintext {
		return nil, errors.New("vault record exceeds implementation bounds")
	}
	return plain, nil
}

func validateRecordBinding(binding RecordBinding) error {
	if binding.RecordType == "" || binding.RecordID == "" || binding.KeyID == "" {
		return errors.New("vault record binding is incomplete")
	}
	return nil
}

func recordAD(binding RecordBinding) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: binding.RecordType, RecordID: binding.RecordID, KeyID: binding.KeyID}
}
