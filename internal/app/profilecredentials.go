package app

import (
	"encoding/json"
	"fmt"

	"devkit/internal/accounts"
	"devkit/internal/vaultitem"
)

// ProfileCredentials is the credential store the connection modules use.
//
// It is the adapter that lets a database, SSH or Kafka profile keep its
// secret in Accounts instead of in its own vault-item payload — one store for
// every credential DevKit holds, which is what A1 asked for. The modules
// declare a narrow interface and never import accounts; this satisfies it.
type ProfileCredentials struct{ accounts *accounts.Service }

// NewProfileCredentials builds the adapter. A nil service leaves the modules
// on their legacy payloads rather than failing: an ephemeral container has no
// local table, and a profile with no credential store still works.
func NewProfileCredentials(service *accounts.Service) *ProfileCredentials {
	if service == nil {
		return nil
	}
	return &ProfileCredentials{accounts: service}
}

func (p *ProfileCredentials) PutForOwner(ownerID, provider, label, user, secret string) error {
	return p.accounts.PutForOwner(ownerID, provider, label, user, secret)
}

func (p *ProfileCredentials) SecretForOwner(ownerID string) (string, string, bool, error) {
	return p.accounts.SecretForOwner(ownerID)
}

func (p *ProfileCredentials) DeleteForOwner(ownerID string) error {
	return p.accounts.DeleteForOwner(ownerID)
}

// legacyKind names one connection type's records and how its secret is sealed.
type legacyKind struct {
	itemType string
	keyID    string
	provider string
	// label reads a human name out of the profile's plaintext metadata, so the
	// account list says "staging" rather than a record id.
	label func(json.RawMessage) string
}

// legacyKinds is every profile type whose secret used to live in its own
// payload. ssh-key-passphrase is here too: it is a credential belonging to one
// key, which is the same shape as a connection's password.
func legacyKinds() []legacyKind {
	name := func(raw json.RawMessage) string {
		var meta struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &meta) == nil {
			return meta.Name
		}
		return ""
	}
	return []legacyKind{
		{vaultitem.ItemTypeDatabase, "db-password", accounts.ProviderDatabase, name},
		{vaultitem.ItemTypeSSH, "ssh-secret", accounts.ProviderSSH, name},
		{vaultitem.ItemTypeKafka, "kafka-secret", accounts.ProviderKafka, name},
		{"ssh-key-passphrase", "ssh-key-passphrase", accounts.ProviderSSH, func(json.RawMessage) string {
			return "SSH key passphrase"
		}},
	}
}

// MigrationReport says what a pass did, so the UI can report it rather than
// claiming success silently.
type MigrationReport struct {
	// Copied is how many profile secrets now also live in Accounts.
	Copied int `json:"copied"`
	// AlreadyThere is how many were already migrated — the normal answer on
	// every run after the first.
	AlreadyThere int `json:"alreadyThere"`
	// Cleared is how many payloads were emptied, which only ClearLegacy does.
	Cleared int `json:"cleared"`
}

// MigrateLegacyCredentials copies each profile's payload secret into Accounts
// and leaves the payload alone.
//
// Copying rather than moving is the whole safety of this. A profile's payload
// syncs; a scoped account does not. If this emptied the payload, every other
// device would receive a profile with no secret and no local copy — and it
// cannot extract its own first, because that needs the vault open and sync
// does not wait for a person. Copying can be run on every device in any order
// and destroys nothing.
//
// Idempotent: a profile that already has a scoped account is left alone, so
// re-running it after entering a new password does not overwrite the new one
// with the stale payload.
func MigrateLegacyCredentials(c *Container) (MigrationReport, error) {
	var report MigrationReport
	if c == nil || c.Accounts == nil || c.Items == nil {
		return report, nil
	}
	for _, kind := range legacyKinds() {
		records, err := c.Items.ListByType(kind.itemType)
		if err != nil {
			return report, fmt.Errorf("app: list %s: %w", kind.itemType, err)
		}
		for _, record := range records {
			if _, _, found, err := c.Accounts.SecretForOwner(record.ID); err != nil {
				return report, err
			} else if found {
				report.AlreadyThere++
				continue
			}
			secret, err := c.Items.OpenPayload(record, kind.keyID)
			if err != nil {
				// A payload this build cannot open is not a reason to abandon
				// the rest: the profile keeps working on its legacy path.
				continue
			}
			if len(secret) == 0 {
				continue
			}
			if err := c.Accounts.PutForOwner(record.ID, kind.provider, kind.label(record.Metadata), "", string(secret)); err != nil {
				return report, err
			}
			report.Copied++
		}
	}
	return report, nil
}

// ClearLegacyCredentialPayloads empties the profile payloads whose secret is
// now in Accounts.
//
// This is the step that actually stops connection passwords syncing, and it is
// separate and explicit because that is a decision with a consequence: every
// other device will ask for the password again. Nothing calls it
// automatically.
//
// A profile with no scoped account here is skipped — clearing that would
// destroy the only copy this machine has.
func ClearLegacyCredentialPayloads(c *Container) (MigrationReport, error) {
	var report MigrationReport
	if c == nil || c.Accounts == nil || c.Items == nil {
		return report, nil
	}
	for _, kind := range legacyKinds() {
		records, err := c.Items.ListByType(kind.itemType)
		if err != nil {
			return report, fmt.Errorf("app: list %s: %w", kind.itemType, err)
		}
		for _, record := range records {
			_, _, found, err := c.Accounts.SecretForOwner(record.ID)
			if err != nil {
				return report, err
			}
			if !found {
				// No local copy: clearing the payload would leave this machine
				// with no way to use the profile at all.
				continue
			}
			payload, err := c.Items.OpenPayload(record, kind.keyID)
			if err != nil || len(payload) == 0 {
				continue
			}
			if _, err := c.Items.Update(record.ID, kind.keyID, record.Metadata, []byte{}); err != nil {
				return report, err
			}
			report.Cleared++
		}
	}
	return report, nil
}
