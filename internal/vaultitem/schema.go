package vaultitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

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
	// ItemTypeAccount covers *shared* accounts only — an identity reused in
	// several places, which is exactly what should follow the user between
	// devices. A scoped account (one connection's password) is deliberately
	// not a vault item: everything here is sync-bound by construction, so a
	// credential that must stay on one machine cannot live here at all. It
	// goes to storage.TableAccountSecrets, the same separation query history
	// keeps from saved queries.
	ItemTypeAccount = "account"

	// The Tasks module's three. A task's title and status are plaintext
	// metadata for the same reason a note's title is: the board must render,
	// filter and sort while the vault is locked, and only the description
	// needs the DEK.
	ItemTypeTaskBoard   = "task-board"
	ItemTypeTask        = "task"
	ItemTypeTaskComment = "task-comment"

	// The API Client's three. History is deliberately absent: a response can
	// carry anything the server sent, and vault_items replicates.
	ItemTypeAPICollection  = "api-collection"
	ItemTypeAPIRequest     = "api-request"
	ItemTypeAPIEnvironment = "api-environment"

	// The Git module's two bindings. Neither holds a secret: a guard names a
	// branch pattern and an environment tag, a remote names an account by id.
	// They sync because they are properties of the repository rather than of
	// the laptop — a rule about which branch is protected should follow the
	// user between machines.
	ItemTypeGitGuard  = "git-guard"
	ItemTypeGitRemote = "git-remote"
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
		ItemTypeSSH, ItemTypeKafka, ItemTypeDatabaseQuery, ItemTypeAccount,
		ItemTypeTaskBoard, ItemTypeTask, ItemTypeTaskComment,
		ItemTypeAPICollection, ItemTypeAPIRequest, ItemTypeAPIEnvironment,
		ItemTypeGitGuard, ItemTypeGitRemote:
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

	case ItemTypeGitGuard:
		var value struct {
			ProjectKey string `json:"project_key"`
			Pattern    string `json:"pattern"`
			Env        string `json:"env"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.ProjectKey == "" ||
			value.Pattern == "" || value.Env == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "git-guard"

	case ItemTypeGitRemote:
		var value struct {
			ProjectKey string `json:"project_key"`
			Name       string `json:"name"`
			// AccountID is plaintext because it is an id, not a secret — the
			// same reason api-request stores one.
			AccountID string `json:"account_id"`
			Env       string `json:"env"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.ProjectKey == "" || value.Name == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "git-remote"

	case ItemTypeAPICollection:
		var value struct {
			Name     string `json:"name"`
			ParentID string `json:"parent_id"`
			Order    int    `json:"order"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "api-collection"

	case ItemTypeAPIRequest:
		var value struct {
			CollectionID string `json:"collection_id"`
			Name         string `json:"name"`
			Method       string `json:"method"`
			URLTemplate  string `json:"url_template"`
			// AccountID is plaintext because it is an ID, not a secret — and
			// that is the point: rotating the account leaves every request
			// bound to it working, with nothing here to edit.
			AccountID string `json:"account_id"`
			OriginRef string `json:"origin_ref"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" ||
			value.Method == "" || value.URLTemplate == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "api-request"

	case ItemTypeAPIEnvironment:
		var value struct {
			Name string            `json:"name"`
			Env  string            `json:"env"`
			Vars map[string]string `json:"vars"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || value.Env == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "api-environment"

	case ItemTypeTaskBoard:
		var value struct {
			Name           string          `json:"name"`
			KeyPrefix      string          `json:"key_prefix"`
			Color          string          `json:"color"`
			Workflow       json.RawMessage `json:"workflow"`
			Verify         json.RawMessage `json:"verify"`
			BranchTemplate string          `json:"branch_template"`
			ProjectPath    string          `json:"project_path"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Name == "" ||
			value.KeyPrefix == "" || len(value.Workflow) == 0 {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "task-board"

	case ItemTypeTask:
		var value struct {
			BoardID    string          `json:"board_id"`
			Key        string          `json:"key"`
			Title      string          `json:"title"`
			Status     string          `json:"status"`
			Priority   string          `json:"priority"`
			Labels     []string        `json:"labels"`
			Assignee   string          `json:"assignee"`
			ParentID   string          `json:"parent_id"`
			BlockedBy  []string        `json:"blocked_by"`
			PlanNodeID string          `json:"plan_node_id"`
			SessionID  string          `json:"session_id"`
			Branch     string          `json:"branch"`
			ArchivedAt string          `json:"archived_at"`
			History    json.RawMessage `json:"history"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.BoardID == "" ||
			value.Key == "" || value.Title == "" || value.Status == "" {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "task-body"

	case ItemTypeTaskComment:
		var value struct {
			TaskID string `json:"task_id"`
			Author string `json:"author"`
			Kind   string `json:"kind"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.TaskID == "" ||
			value.Author == "" || !validCommentKind(value.Kind) {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "task-comment-body"

	case ItemTypeAccount:
		var value struct {
			Provider   string   `json:"provider"`
			Kind       string   `json:"kind"`
			Label      string   `json:"label"`
			Subject    string   `json:"subject"`
			Scopes     []string `json:"scopes"`
			Env        []string `json:"env"`
			State      string   `json:"state"`
			ExpiresAt  string   `json:"expires_at"`
			VerifiedAt string   `json:"verified_at"`
			Ref        string   `json:"ref"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Provider == "" ||
			value.Label == "" || !validAccountKind(value.Kind) || !validAccountState(value.State) {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "account-secret"

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

// NoteFolderColors are the preset colors a note or a folder can pick: names
// the UI maps to theme tokens, so they follow light and dark mode. They are
// presets, not the whole set — any "#rrggbb" is accepted too.
var NoteFolderColors = []string{"red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray"}

// ValidNoteFolderColor accepts a preset name or a lowercase "#rrggbb". The
// hex form is exactly that — six digits, lowercase — because the UI puts the
// value straight into a style: a looser check (any CSS color, "#abc",
// "rgb(...)") would be a string the frontend has to re-validate, and the
// stored form should be one canonical spelling per color so two devices do
// not keep "#ABC" and "#aabbcc" as different values. NormalizeNoteFolderColor
// turns what a caller types into that form.
func ValidNoteFolderColor(color string) bool {
	for _, c := range NoteFolderColors {
		if color == c {
			return true
		}
	}
	return isHexColor(color)
}

func isHexColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, r := range color[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// NormalizeNoteFolderColor trims and lower-cases color and expands the short
// hex form ("#ABC" → "#aabbcc"). It does not validate; ValidNoteFolderColor
// does, on the result.
func NormalizeNoteFolderColor(color string) string {
	color = strings.ToLower(strings.TrimSpace(color))
	if len(color) == 4 && color[0] == '#' {
		color = string([]byte{'#', color[1], color[1], color[2], color[2], color[3], color[3]})
	}
	return color
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

// validAccountKind mirrors accounts.Kind. It is duplicated rather than
// imported because vaultitem is the substrate every module builds on and must
// not depend on any of them — the same reason the note and profile shapes are
// restated here instead of imported.
//
// "scoped" is absent on purpose: a scoped account is never a vault item, so a
// record claiming to be one is a record that must not enter the ledger.
func validAccountKind(kind string) bool {
	switch kind {
	case "token", "basic", "oauth", "secret", "external":
		return true
	}
	return false
}

func validAccountState(state string) bool {
	switch state {
	case "valid", "expiring", "expired", "revoked", "unverified":
		return true
	}
	return false
}

// validCommentKind mirrors tasks.CommentKind. A system comment is written by
// the service, never by a caller claiming to be one.
func validCommentKind(kind string) bool {
	switch kind {
	case "human", "agent", "system":
		return true
	}
	return false
}
