package sync

import (
	"encoding/json"
	"errors"
	"fmt"

	"changeme/internal/crypto"
)

const (
	// ReplicationEnvelopeVersion identifies the wire contract for replication.
	// It is intentionally separate from crypto.EnvelopeVersionV2.
	ReplicationEnvelopeVersion uint32 = 1

	replicationAADNamespace = "dev-kit/sync/replication"
)

// Vault is the narrow encryption boundary required by replication. The
// implementation owns key material; sync only supplies authenticated record
// identity and never receives or persists plaintext keys.
type Vault interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}

// ReplicationEnvelope is the transport wrapper for an encrypted record.
// Record identity and key version are public routing metadata. Ciphertext is
// the serialized nested crypto.Envelope; record bytes never appear outside it.
type ReplicationEnvelope struct {
	RecordID        string `json:"record_id"`
	RecordType      string `json:"record_type"`
	KeyVersion      uint32 `json:"key_version"`
	EnvelopeVersion uint32 `json:"envelope_version"`
	Ciphertext      []byte `json:"ciphertext"`
}

// Validate checks the public contract and the nested crypto envelope without
// decrypting it. This rejects malformed or unsupported transport data before
// it reaches the vault boundary.
func (e ReplicationEnvelope) Validate() error {
	if err := validateIdentifier("record ID", e.RecordID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record type", e.RecordType, 128); err != nil {
		return err
	}
	if e.KeyVersion == 0 {
		return errors.New("key version must be positive")
	}
	if e.EnvelopeVersion != ReplicationEnvelopeVersion {
		return errors.New("replication envelope version is unsupported")
	}
	if len(e.Ciphertext) == 0 {
		return errors.New("replication ciphertext is required")
	}

	var nested crypto.Envelope
	if err := json.Unmarshal(e.Ciphertext, &nested); err != nil {
		return errors.New("replication ciphertext contains an invalid payload envelope")
	}
	if err := nested.Validate(); err != nil {
		return fmt.Errorf("payload envelope: %w", err)
	}
	return nil
}

// SealPayload encrypts record bytes into a replication envelope. The bytes may
// contain record metadata and a nested payload envelope, but they are treated
// as opaque by sync and are never serialized into public transport fields.
func SealPayload(vault Vault, recordID, recordType, keyID string, keyVersion uint32, plaintext []byte) (ReplicationEnvelope, error) {
	if vault == nil {
		return ReplicationEnvelope{}, errors.New("vault is required")
	}
	if err := validateReplicationInputs(recordID, recordType, keyID, keyVersion, plaintext); err != nil {
		return ReplicationEnvelope{}, err
	}

	nested, err := vault.SealV2(replicationAssociatedData(recordID, recordType, keyID, keyVersion), plaintext)
	if err != nil {
		return ReplicationEnvelope{}, err
	}
	serializedNested, err := json.Marshal(nested)
	if err != nil {
		return ReplicationEnvelope{}, errors.New("serialize payload envelope")
	}

	envelope := ReplicationEnvelope{
		RecordID:        recordID,
		RecordType:      recordType,
		KeyVersion:      keyVersion,
		EnvelopeVersion: ReplicationEnvelopeVersion,
		Ciphertext:      serializedNested,
	}
	if err := envelope.Validate(); err != nil {
		return ReplicationEnvelope{}, err
	}
	return envelope, nil
}

// OpenPayload validates public metadata, then asks the vault to authenticate
// and decrypt the nested envelope. Changed IDs, key IDs, key versions, or
// replication versions fail through the associated-data check.
func OpenPayload(vault Vault, envelope ReplicationEnvelope, keyID string) ([]byte, error) {
	if vault == nil {
		return nil, errors.New("vault is required")
	}
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	if err := validateIdentifier("key ID", keyID, 256); err != nil {
		return nil, err
	}

	var nested crypto.Envelope
	if err := json.Unmarshal(envelope.Ciphertext, &nested); err != nil {
		return nil, errors.New("replication ciphertext contains an invalid payload envelope")
	}
	return vault.OpenWithAD(nested, replicationAssociatedData(envelope.RecordID, envelope.RecordType, keyID, envelope.KeyVersion))
}

func validateReplicationInputs(recordID, recordType, keyID string, keyVersion uint32, plaintext []byte) error {
	if err := validateIdentifier("record ID", recordID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record type", recordType, 128); err != nil {
		return err
	}
	if err := validateIdentifier("key ID", keyID, 256); err != nil {
		return err
	}
	if keyVersion == 0 {
		return errors.New("key version must be positive")
	}
	if len(plaintext) == 0 {
		return errors.New("plaintext payload is required")
	}
	return nil
}

func replicationAssociatedData(recordID, recordType, keyID string, keyVersion uint32) crypto.AssociatedData {
	return crypto.AssociatedData{
		RecordType: fmt.Sprintf("%s/v%d/key-version/%d/%s", replicationAADNamespace, ReplicationEnvelopeVersion, keyVersion, recordType),
		RecordID:   recordID,
		KeyID:      keyID,
	}
}
