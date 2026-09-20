// Package accounts is the one place DevKit keeps credentials.
//
// It owns two dimensions that used to be one. A *shared* account is an
// identity reused in several places — a GitHub PAT that git push, an API
// Client request and an agent opening a PR all need — and it is a vault item,
// so it syncs. A *scoped* account is the credential of exactly one connection
// (this database's password, this SSH key's passphrase) and never leaves the
// machine that made it, so it cannot be a vault item at all: everything in
// vault_items is sync-bound by construction, the same reason db_query_history
// lives in a table of its own.
//
// Both sit behind one Service, which is what "one store" has to mean in
// practice: one reveal rule, one audit path, one listing, one place to rotate.
//
// Not here, on purpose: the Cloud refresh token (the OS keychain is the right
// home for something that unlocks the vault) and Agent Hub's vendor sign-in
// (internal/agenthub/auth.go deliberately never touches the credential). Both
// appear as KindExternal rows so the list can answer "what am I signed in to"
// without claiming a secret it does not hold.
package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

// Kind is what shape of credential an account holds.
type Kind string

const (
	// KindToken is a PAT or API key: one opaque string sent as a bearer.
	KindToken Kind = "token"
	// KindBasic is a username and password pair.
	KindBasic Kind = "basic"
	// KindOAuth is an access token with a refresh token and an expiry.
	KindOAuth Kind = "oauth"
	// KindSecret is a single string that is not an identity on its own: a key
	// passphrase, a database password, a SASL secret.
	KindSecret Kind = "secret"
	// KindExternal holds nothing. It records that an identity exists and how
	// to sign in again, for the two credentials DevKit deliberately does not
	// keep.
	KindExternal Kind = "external"
)

// State is what the account is worth right now.
type State string

const (
	StateValid      State = "valid"
	StateExpiring   State = "expiring"
	StateExpired    State = "expired"
	StateRevoked    State = "revoked"
	StateUnverified State = "unverified"
	// StateNeedsLocalSecret is a scoped account that exists on another device
	// but not here. The profile synced; its secret did not. It is a sentence,
	// not a failure: the UI asks for the password for this machine.
	StateNeedsLocalSecret State = "needs_local_secret"
)

// ExpiringWithin is how long before expiry an account starts saying so.
const ExpiringWithin = 14 * 24 * time.Hour

var (
	ErrNotFound      = errors.New("accounts.notFound")
	ErrInvalid       = errors.New("accounts.invalid")
	ErrUnknownKind   = errors.New("accounts.unknownKind")
	ErrEnvNotAllowed = errors.New("accounts.envNotAllowed")
	// ErrAmbiguous is two or more accounts matching a host. Picking one would
	// be firing a request as the wrong identity; the caller must choose.
	ErrAmbiguous = errors.New("accounts.ambiguous")
	// ErrScopedCannotSync guards the one invariant that cannot be relaxed.
	ErrScopedCannotSync = errors.New("accounts.scopedCannotSync")
	// ErrNothingToReveal is an external account: DevKit recorded that the
	// identity exists and how to sign in again, and holds no value at all.
	ErrNothingToReveal = errors.New("accounts.nothingToReveal")
)

// Account is one credential, without its value. Nothing here is secret, which
// is why the whole type crosses the bridge and the payload never does.
type Account struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Kind     Kind   `json:"kind"`
	Label    string `json:"label"`
	// Subject is who the provider said this is ("syurodev"), empty until
	// verified.
	Subject string   `json:"subject"`
	Scopes  []string `json:"scopes"`
	// Env restricts the account to these environment tags. Empty means any.
	Env []string `json:"env"`
	// ScopeOwner is the profile id a scoped account belongs to; empty for a
	// shared one. It is the link, deliberately instead of the profile storing
	// an account id: an id pointing at a record that does not sync is a
	// dangling reference on the next device, and writing a new one back would
	// have the two devices overwrite each other forever.
	ScopeOwner string    `json:"scopeOwner"`
	State      State     `json:"state"`
	ExpiresAt  time.Time `json:"expiresAt"`
	VerifiedAt time.Time `json:"verifiedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	// Ref is how to sign in again, for KindExternal.
	Ref string `json:"ref"`
}

// Scoped reports whether the account belongs to a single connection, and so
// lives in the local table rather than in vault_items.
func (a Account) Scoped() bool { return a.ScopeOwner != "" }

// Credential is an account's value. It exists only inside Go, between a
// Resolve and the call that uses it; no field of it crosses the bridge.
type Credential struct {
	Kind   Kind
	User   string
	Secret string
}

// metadata is an account's plaintext half. It is deliberately enough to list,
// filter and warn about expiry without opening the vault.
type metadata struct {
	Provider   string   `json:"provider"`
	Kind       string   `json:"kind"`
	Label      string   `json:"label"`
	Subject    string   `json:"subject,omitempty"`
	Scopes     []string `json:"scopes,omitempty"`
	Env        []string `json:"env,omitempty"`
	State      string   `json:"state"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	VerifiedAt string   `json:"verified_at,omitempty"`
	Ref        string   `json:"ref,omitempty"`
}

// KeyID is the inner AAD key id for a shared account's payload.
const KeyID = "account-secret"

// ScopedRecordType is the AAD record type for a scoped account's envelope. It
// is NOT a vault item type, and that is the point: an envelope written for a
// local scoped account must not open as a shared one, and vice versa — the
// same separation db-query-history keeps from db-query.
const ScopedRecordType = "account-scoped"

func (m metadata) account(id string, created, updated time.Time) Account {
	return Account{
		ID:         id,
		Provider:   m.Provider,
		Kind:       Kind(m.Kind),
		Label:      m.Label,
		Subject:    m.Subject,
		Scopes:     append([]string(nil), m.Scopes...),
		Env:        append([]string(nil), m.Env...),
		State:      State(m.State),
		ExpiresAt:  parseTime(m.ExpiresAt),
		VerifiedAt: parseTime(m.VerifiedAt),
		CreatedAt:  created,
		UpdatedAt:  updated,
		Ref:        m.Ref,
	}
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func decodeMetadata(record vaultitem.Record) (Account, error) {
	var m metadata
	decoder := json.NewDecoder(strings.NewReader(string(record.Metadata)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return Account{}, ErrInvalid
	}
	return m.account(record.ID, record.CreatedAt, record.UpdatedAt), nil
}

// derivedState is what an account is worth at now, given what verification
// last said. A stored revoked or unverified state stands; expiry is recomputed
// because it passes on its own, with nobody writing anything.
func derivedState(stored State, expiresAt time.Time, now time.Time) State {
	switch stored {
	case StateRevoked, StateUnverified, StateNeedsLocalSecret:
		return stored
	}
	if expiresAt.IsZero() {
		return StateValid
	}
	switch {
	case !now.Before(expiresAt):
		return StateExpired
	case expiresAt.Sub(now) <= ExpiringWithin:
		return StateExpiring
	default:
		return StateValid
	}
}

// ValidKind reports whether k is one this package knows.
func ValidKind(k Kind) bool {
	switch k {
	case KindToken, KindBasic, KindOAuth, KindSecret, KindExternal:
		return true
	}
	return false
}
