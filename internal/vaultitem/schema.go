package vaultitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"devkit/internal/crypto"
)

const (
	ItemTypeNote     = "note"
	ItemTypeDatabase = "db-profile"
	ItemTypeSSH      = "ssh-profile"
)

// ErrInvalidRecordSchema means a vault item does not satisfy the contract of
// its owning module. It is intentionally generic so callers do not expose
// details from untrusted remote snapshots.
var ErrInvalidRecordSchema = errors.New("vault item schema is invalid")

// IsSupportedItemType reports whether a record type has a schema owned by a
// built-in vault-backed module. Sync must reject unknown types for both live
// snapshots and tombstones so arbitrary records cannot enter its ledgers.
func IsSupportedItemType(itemType string) bool {
	switch itemType {
	case ItemTypeNote, ItemTypeDatabase, ItemTypeSSH:
		return true
	default:
		return false
	}
}

// ValidateRecordSchema validates the module-owned portion of a vault item at
// trust boundaries such as remote sync. Local module creation remains the
// source of truth for authoring records; this validator mirrors the persisted
// shapes those modules produce so malformed snapshots never reach storage.
func ValidateRecordSchema(itemType string, metadata json.RawMessage, payload *crypto.Envelope) error {
	if !IsSupportedItemType(itemType) || payload == nil || payload.Validate() != nil {
		return ErrInvalidRecordSchema
	}

	var expectedKeyID string
	switch itemType {
	case ItemTypeNote:
		var value struct {
			Title string `json:"title"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Title == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "note-body"

	case ItemTypeDatabase:
		var value struct {
			Name     string `json:"name"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			Database string `json:"database"`
			Username string `json:"username"`
			SSLMode  string `json:"ssl_mode"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || value.Host == "" || value.Port == 0 || value.SSLMode == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "db-password"

	case ItemTypeSSH:
		var value struct {
			Name               string `json:"name"`
			Host               string `json:"host"`
			Port               int    `json:"port"`
			Username           string `json:"username"`
			AuthMethod         string `json:"auth_method"`
			HostKeyFingerprint string `json:"host_key_fingerprint"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || value.Host == "" || value.Port == 0 || value.Username == "" ||
			(value.AuthMethod != "password" && value.AuthMethod != "key") {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "ssh-secret"

	default:
		return ErrInvalidRecordSchema
	}

	if payload.KeyID != expectedKeyID {
		return ErrInvalidRecordSchema
	}
	return nil
}

func decodeMetadataObject(metadata json.RawMessage, destination any) error {
	trimmed := bytes.TrimSpace(metadata)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ErrInvalidRecordSchema
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrInvalidRecordSchema
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidRecordSchema
	}
	return nil
}
