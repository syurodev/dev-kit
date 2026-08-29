package vaultitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"devkit/internal/crypto"
)

func TestValidateRecordSchemaAcceptsSupportedModuleRecords(t *testing.T) {
	tests := []struct {
		name     string
		itemType string
		metadata string
		keyID    string
	}{
		{name: "note", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes"}`, keyID: "note-body"},
		{name: "note with org fields", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes","folder_id":"aabbccddeeff001122334455","tags":["ops"]}`, keyID: "note-body"},
		{name: "note with color", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes","color":"blue"}`, keyID: "note-body"},
		{name: "note-folder", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue","tags":["work"]}`, keyID: "note-folder"},
		// These two must mirror what internal/database and internal/ssh actually
		// persist, not a reduced shape. decodeMetadataObject disallows unknown
		// fields, so a validator missing one field makes the record unwritable —
		// which is exactly how env slipped through for both modules.
		{name: "database", itemType: ItemTypeDatabase, metadata: `{"name":"Primary","host":"db.internal","port":5432,"database":"app","username":"devkit","ssl_mode":"verify-full","env":"prod","read_only":false}`, keyID: "db-password"},
		{name: "database through a tunnel", itemType: ItemTypeDatabase, metadata: `{"name":"Primary","host":"db.internal","port":5432,"database":"app","username":"devkit","ssl_mode":"verify-full","env":"prod","read_only":false,"ssh_profile_id":"aabbccddeeff001122334455"}`, keyID: "db-password"},
		{name: "ssh", itemType: ItemTypeSSH, metadata: `{"name":"Bastion","host":"ssh.internal","port":22,"username":"devkit","auth_method":"key","host_key_fingerprint":"SHA256:test","env":"prod"}`, keyID: "ssh-secret"},
		{name: "db-query saved", itemType: ItemTypeDatabaseQuery, metadata: `{"profile_id":"aabbccddeeff001122334455","name":"monthly totals"}`, keyID: "db-query-body"},
		{name: "kafka", itemType: ItemTypeKafka, metadata: `{"name":"Events","bootstrap_servers":["kafka.internal:9092"],"security_protocol":"PLAINTEXT","env":"prod"}`, keyID: "kafka-secret"},
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
		{name: "note unknown color", itemType: ItemTypeNote, metadata: `{"title":"x","color":"neon"}`, payload: validSchemaEnvelope("note-body")},
		{name: "missing database host", itemType: ItemTypeDatabase, metadata: `{"name":"db","port":5432,"ssl_mode":"verify-full"}`, payload: validSchemaEnvelope("db-password")},
		{name: "database without env", itemType: ItemTypeDatabase, metadata: `{"name":"db","host":"db.internal","port":5432,"ssl_mode":"verify-full"}`, payload: validSchemaEnvelope("db-password")},
		{name: "ssh without env", itemType: ItemTypeSSH, metadata: `{"name":"b","host":"h","port":22,"username":"u","auth_method":"key"}`, payload: validSchemaEnvelope("ssh-secret")},
		{name: "saved query without profile", itemType: ItemTypeDatabaseQuery, metadata: `{"name":"totals"}`, payload: validSchemaEnvelope("db-query-body")},
		{name: "saved query carrying sql in metadata", itemType: ItemTypeDatabaseQuery, metadata: `{"profile_id":"aabbccddeeff001122334455","name":"t","sql":"SELECT 1"}`, payload: validSchemaEnvelope("db-query-body")},
		{name: "invalid ssh auth method", itemType: ItemTypeSSH, metadata: `{"name":"box","host":"host","port":22,"username":"me","auth_method":"agent"}`, payload: validSchemaEnvelope("ssh-secret")},
		{name: "missing encrypted payload", itemType: ItemTypeNote, metadata: `{"title":"x"}`},
		{name: "wrong payload key", itemType: ItemTypeNote, metadata: `{"title":"x"}`, payload: validSchemaEnvelope("db-password")},
		{name: "note-folder missing name", itemType: ItemTypeNoteFolder, metadata: `{"color":"blue"}`, payload: validSchemaEnvelope("note-folder")},
		{name: "note-folder unknown color", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"neon"}`, payload: validSchemaEnvelope("note-folder")},
		{name: "note-folder unknown field", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue","icon":"x"}`, payload: validSchemaEnvelope("note-folder")},
		{name: "note-folder wrong payload key", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue"}`, payload: validSchemaEnvelope("note-body")},
		{name: "note-folder missing payload", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRecordSchema(test.itemType, json.RawMessage(test.metadata), test.payload); !errors.Is(err, ErrInvalidRecordSchema) {
				t.Fatalf("ValidateRecordSchema() error = %v, want ErrInvalidRecordSchema", err)
			}
		})
	}
}

func TestValidateKafkaProfile(t *testing.T) {
	validPlain := `{"name":"Events","bootstrap_servers":["kafka.internal:9092"],"security_protocol":"PLAINTEXT","env":"prod"}`
	validSASL := `{"name":"Events","bootstrap_servers":["kafka.internal:9093"],"security_protocol":"SASL_SSL","sasl_mechanism":"SCRAM-SHA-256","username":"devkit","env":"staging"}`

	if err := ValidateRecordSchema(ItemTypeKafka, json.RawMessage(validPlain), validSchemaEnvelope("kafka-secret")); err != nil {
		t.Fatalf("valid PLAINTEXT profile: ValidateRecordSchema() error = %v", err)
	}
	if err := ValidateRecordSchema(ItemTypeKafka, json.RawMessage(validSASL), validSchemaEnvelope("kafka-secret")); err != nil {
		t.Fatalf("valid SASL profile: ValidateRecordSchema() error = %v", err)
	}

	invalid := []struct {
		name     string
		metadata string
		keyID    string
	}{
		{name: "missing name", metadata: `{"bootstrap_servers":["kafka:9092"],"security_protocol":"PLAINTEXT","env":"prod"}`, keyID: "kafka-secret"},
		{name: "missing bootstrap_servers", metadata: `{"name":"Events","security_protocol":"PLAINTEXT","env":"prod"}`, keyID: "kafka-secret"},
		{name: "empty bootstrap_servers", metadata: `{"name":"Events","bootstrap_servers":[],"security_protocol":"PLAINTEXT","env":"prod"}`, keyID: "kafka-secret"},
		{name: "missing env", metadata: `{"name":"Events","bootstrap_servers":["kafka:9092"],"security_protocol":"PLAINTEXT"}`, keyID: "kafka-secret"},
		{name: "invalid security_protocol", metadata: `{"name":"Events","bootstrap_servers":["kafka:9092"],"security_protocol":"UNKNOWN","env":"prod"}`, keyID: "kafka-secret"},
		{name: "SASL missing mechanism", metadata: `{"name":"Events","bootstrap_servers":["kafka:9093"],"security_protocol":"SASL_SSL","username":"devkit","env":"prod"}`, keyID: "kafka-secret"},
		{name: "SASL missing username", metadata: `{"name":"Events","bootstrap_servers":["kafka:9093"],"security_protocol":"SASL_PLAINTEXT","sasl_mechanism":"PLAIN","env":"prod"}`, keyID: "kafka-secret"},
		{name: "wrong payload key", metadata: validPlain, keyID: "ssh-secret"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRecordSchema(ItemTypeKafka, json.RawMessage(tc.metadata), validSchemaEnvelope(tc.keyID)); !errors.Is(err, ErrInvalidRecordSchema) {
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
