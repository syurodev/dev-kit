package vaultitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"devkit/internal/crypto"
)

const (
	ItemTypeNote       = "note"
	ItemTypeNoteFolder = "note-folder"
	// ItemTypeNoteVersion is a full title+body snapshot of one note. It is a
	// vault item, so it syncs with the note it belongs to — that is a decided
	// trade, not an accident: nothing filters the sync pipeline by type.
	ItemTypeNoteVersion = "note-version"
	ItemTypeDatabase    = "db-profile"
	ItemTypeSSH         = "ssh-profile"
	ItemTypeKafka       = "kafka-profile"
	// ItemTypeDatabaseQuery covers *saved* queries only. Query history is
	// deliberately not a vault item: everything in vault_items is sync-bound
	// by construction, and history must never leave the device.
	ItemTypeDatabaseQuery = "db-query"
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
	case ItemTypeNote, ItemTypeNoteFolder, ItemTypeNoteVersion, ItemTypeDatabase,
		ItemTypeSSH, ItemTypeKafka, ItemTypeDatabaseQuery:
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
			Title    string   `json:"title"`
			FolderID string   `json:"folder_id"`
			Tags     []string `json:"tags"`
			Color    string   `json:"color"`
			// RFC3339, empty for a live note. decodeMetadataObject rejects
			// unknown fields, so a note in the trash is unwritable until this
			// is declared here — the failure that already caught db-profile.
			DeletedAt string `json:"deleted_at"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Title == "" ||
			(value.Color != "" && !ValidNoteFolderColor(value.Color)) {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "note-body"

	case ItemTypeNoteVersion:
		var value struct {
			NoteID string `json:"note_id"`
			// Display and debugging only — no logic reads it.
			Trigger string `json:"trigger"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.NoteID == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "note-version-body"

	case ItemTypeNoteFolder:
		var value struct {
			Name  string   `json:"name"`
			Color string   `json:"color"`
			Tags  []string `json:"tags"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || !ValidNoteFolderColor(value.Color) {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "note-folder"

	case ItemTypeDatabase:
		var value struct {
			Name          string            `json:"name"`
			Host          string            `json:"host"`
			Port          int               `json:"port"`
			Database      string            `json:"database"`
			Username      string            `json:"username"`
			SSLMode       string            `json:"ssl_mode"`
			Env           string            `json:"env"`
			ReadOnly      bool              `json:"read_only"`
			SSHProfileID  string            `json:"ssh_profile_id"`
			Engine        string            `json:"engine"`
			DriverOptions map[string]string `json:"driver_options"`
			// The metadata carries it whenever a profile has a CA bundle, and
			// unknown fields are refused: without it such a profile failed
			// validation on every write and every sync (#45).
			CACert string `json:"ca_cert"`
			// Declared for the same reason: a profile that says it is reached
			// over Tailscale or a VPN carries it.
			TrustedNetwork bool `json:"trusted_network"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || value.Host == "" || value.Port == 0 || value.SSLMode == "" || value.Env == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "db-password"

	case ItemTypeDatabaseQuery:
		var value struct {
			ProfileID string `json:"profile_id"`
			Name      string `json:"name"`
		}
		// The SQL itself lives in the payload — a saved query can carry
		// literals. Only the profile it belongs to and the name the user gave
		// it are safe as plaintext metadata.
		if decodeMetadataObject(metadata, &value) != nil || value.ProfileID == "" || value.Name == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "db-query-body"

	case ItemTypeSSH:
		var value struct {
			Name               string `json:"name"`
			Host               string `json:"host"`
			Port               int    `json:"port"`
			Username           string `json:"username"`
			AuthMethod         string `json:"auth_method"`
			HostKeyFingerprint string `json:"host_key_fingerprint"`
			Env                string `json:"env"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || value.Host == "" || value.Port == 0 || value.Username == "" || value.Env == "" ||
			(value.AuthMethod != "password" && value.AuthMethod != "key") {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "ssh-secret"

	case ItemTypeKafka:
		var value struct {
			Name             string   `json:"name"`
			BootstrapServers []string `json:"bootstrap_servers"`
			SecurityProtocol string   `json:"security_protocol"`
			Env              string   `json:"env"`
			SASLMechanism    string   `json:"sasl_mechanism"`
			Username         string   `json:"username"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || len(value.BootstrapServers) == 0 || value.Env == "" {
			return ErrInvalidRecordSchema
		}
		switch value.SecurityProtocol {
		case "PLAINTEXT", "SSL":
		case "SASL_PLAINTEXT", "SASL_SSL":
			switch value.SASLMechanism {
			case "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
				if value.Username == "" {
					return ErrInvalidRecordSchema
				}
			default:
				return ErrInvalidRecordSchema
			}
		default:
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "kafka-secret"

	default:
		return ErrInvalidRecordSchema
	}

	if payload.KeyID != expectedKeyID {
		return ErrInvalidRecordSchema
	}
	return nil
}

func ValidNoteFolderColor(color string) bool {
	switch color {
	case "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray":
		return true
	default:
		return false
	}
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
