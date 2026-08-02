package vaultitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"changeme/internal/crypto"
)

func TestValidateRecordSchemaAcceptsSupportedModuleRecords(t *testing.T) {
	tests := []struct {
		name     string
		itemType string
		metadata string
		keyID    string
	}{
		{name: "note", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes"}`, keyID: "note-body"},
		{name: "database", itemType: ItemTypeDatabase, metadata: `{"name":"Primary","host":"db.internal","port":5432,"database":"app","username":"devkit","ssl_mode":"verify-full"}`, keyID: "db-password"},
		{name: "ssh", itemType: ItemTypeSSH, metadata: `{"name":"Bastion","host":"ssh.internal","port":22,"username":"devkit","auth_method":"key","host_key_fingerprint":"SHA256:test"}`, keyID: "ssh-secret"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRecordSchema(test.itemType, json.RawMessage(test.metadata), validSchemaEnvelope(test.keyID)); err != nil {
				t.Fatalf("ValidateRecordSchema() error = %v", err)
			}
		})
	}
}

func TestValidateRecordSchemaRejectsInvalidModuleRecords(t *testing.T) {
	tests := []struct {
		name     string
		itemType string
		metadata string
		payload  *crypto.Envelope
	}{
		{name: "unsupported type", itemType: "unknown", metadata: `{}`, payload: validSchemaEnvelope("unknown")},
		{name: "metadata is not object", itemType: ItemTypeNote, metadata: `[]`, payload: validSchemaEnvelope("note-body")},
		{name: "unknown metadata field", itemType: ItemTypeNote, metadata: `{"title":"x","admin":true}`, payload: validSchemaEnvelope("note-body")},
		{name: "missing note title", itemType: ItemTypeNote, metadata: `{}`, payload: validSchemaEnvelope("note-body")},
		{name: "missing database host", itemType: ItemTypeDatabase, metadata: `{"name":"db","port":5432,"ssl_mode":"verify-full"}`, payload: validSchemaEnvelope("db-password")},
		{name: "invalid ssh auth method", itemType: ItemTypeSSH, metadata: `{"name":"box","host":"host","port":22,"username":"me","auth_method":"agent"}`, payload: validSchemaEnvelope("ssh-secret")},
		{name: "missing encrypted payload", itemType: ItemTypeNote, metadata: `{"title":"x"}`},
		{name: "wrong payload key", itemType: ItemTypeNote, metadata: `{"title":"x"}`, payload: validSchemaEnvelope("db-password")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRecordSchema(test.itemType, json.RawMessage(test.metadata), test.payload); !errors.Is(err, ErrInvalidRecordSchema) {
				t.Fatalf("ValidateRecordSchema() error = %v, want ErrInvalidRecordSchema", err)
			}
		})
	}
}

func validSchemaEnvelope(keyID string) *crypto.Envelope {
	return &crypto.Envelope{
		Version:    crypto.EnvelopeVersionV2,
		Algorithm:  crypto.AlgorithmXChaCha20Poly1305,
		KeyID:      keyID,
		Nonce:      bytes.Repeat([]byte{1}, 24),
		Ciphertext: bytes.Repeat([]byte{2}, 16),
	}
}
