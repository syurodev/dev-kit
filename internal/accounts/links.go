package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// An SSH key and an API token that belong to one login.
//
// To a person a GitLab key and a GitLab token are one account: the key moves
// the code, the token reads the merge requests. DevKit keeps them as two
// accounts because they are two credentials (a key is not an identity a bearer
// header can carry; a token cannot sign git over SSH), each with its own value,
// verification and way to be replaced. A link is the person saying "this token
// goes with that key": the Accounts list draws them as one, and the pull
// request card — which needs the token — finds it from the key a remote is
// bound to instead of guessing by host.
//
// It is a declaration, never inferred. A token may belong to a bot or another
// login than the key; those are simply two accounts. One key has at most one
// token; one token may serve several keys (two keys of the same login).
//
// The table is local, like identities: it says how this machine groups its
// accounts, it is not secret, and a row pointing at an account another device
// does not have is ignored there.

var (
	// ErrLinkMismatch is a link between accounts that cannot go together: the
	// first is not an SSH key, the second is not a token, they are for different
	// providers, or one belongs to a connection.
	ErrLinkMismatch = errors.New("accounts.linkMismatch")
	// ErrLabelInvalid is a label that is empty, too long or has a line break,
	// or an account that cannot be renamed (an external sign-in).
	ErrLabelInvalid = errors.New("accounts.labelInvalid")
)

type linkStore struct {
	db    *sql.DB
	table string
}

// SetLinks wires the local table that holds the key→token links. Without it
// accounts simply have none.
func (s *Service) SetLinks(db *sql.DB, table string) {
	if db != nil && table != "" {
		s.links = &linkStore{db: db, table: table}
	}
}

// LinkToken says tokenID is the API token of key keyID, replacing any earlier
// choice for that key. Returns the key, with the link on it.
func (s *Service) LinkToken(keyID, tokenID string) (Account, error) {
	if s.links == nil {
		return Account{}, ErrInvalid
	}
	key, err := s.get(keyID)
	if err != nil {
		return Account{}, err
	}
	token, err := s.get(tokenID)
	if err != nil {
		return Account{}, err
	}
	// A connection's password belongs to its connection, never to a git login.
	if key.Kind != KindSSHKey || token.Kind != KindToken || key.Provider != token.Provider ||
		(token.Scoped() && !token.Local()) || (key.Scoped() && !key.Local()) {
		return Account{}, ErrLinkMismatch
	}
	if _, err := s.links.db.ExecContext(context.Background(),
		`INSERT INTO `+s.links.table+` (account_id, token_id, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET token_id = excluded.token_id, updated_at = excluded.updated_at`,
		keyID, tokenID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Account{}, err
	}
	s.changed()
	return s.Get(keyID)
}

// UnlinkToken forgets the token of a key. The token account stays.
func (s *Service) UnlinkToken(keyID string) (Account, error) {
	if s.links == nil {
		return Account{}, ErrInvalid
	}
	if _, err := s.get(keyID); err != nil {
		return Account{}, err
	}
	if _, err := s.links.db.ExecContext(context.Background(),
		"DELETE FROM "+s.links.table+" WHERE account_id = ?", keyID); err != nil {
		return Account{}, err
	}
	s.changed()
	return s.Get(keyID)
}

// LinkedTokenOf is the API token linked to a key account, when there is one
// this machine can see.
func (s *Service) LinkedTokenOf(keyID string) (Account, bool) {
	if s.links == nil || keyID == "" {
		return Account{}, false
	}
	var tokenID string
	if err := s.links.db.QueryRowContext(context.Background(),
		"SELECT token_id FROM "+s.links.table+" WHERE account_id = ?", keyID).Scan(&tokenID); err != nil {
		return Account{}, false
	}
	token, err := s.get(tokenID)
	if err != nil || token.Kind != KindToken {
		return Account{}, false
	}
	return token, true
}

// allLinks is every link, key id → token id, for listing without a query each.
func (s *Service) allLinks() map[string]string {
	out := map[string]string{}
	if s.links == nil {
		return out
	}
	rows, err := s.links.db.QueryContext(context.Background(),
		"SELECT account_id, token_id FROM "+s.links.table)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var keyID, tokenID string
		if rows.Scan(&keyID, &tokenID) == nil {
			out[keyID] = tokenID
		}
	}
	return out
}

// withLinks fills the link fields of an account that crosses the bridge: a key
// says which token it has, a token says which keys it serves. A link to an
// account that is not in the list (deleted elsewhere, another device's) is not
// shown.
func withLinks(account Account, links map[string]string, known map[string]bool) Account {
	if tokenID, ok := links[account.ID]; ok && known[tokenID] {
		account.LinkedTokenID = tokenID
	}
	for keyID, tokenID := range links {
		if tokenID == account.ID && known[keyID] {
			account.LinkedKeyIDs = append(account.LinkedKeyIDs, keyID)
		}
	}
	sort.Strings(account.LinkedKeyIDs)
	return account
}

// forgetLinks drops every link an account is part of, as the key or as the token.
func (s *Service) forgetLinks(id string) {
	if s.links != nil {
		_, _ = s.links.db.ExecContext(context.Background(),
			"DELETE FROM "+s.links.table+" WHERE account_id = ? OR token_id = ?", id, id)
	}
}

// moveLinks carries an account's links to its new id: a connection-local
// account that is promoted to a shared one gets a new id.
func (s *Service) moveLinks(oldID, newID string) {
	if s.links == nil || oldID == newID {
		return
	}
	ctx := context.Background()
	_, _ = s.links.db.ExecContext(ctx, "UPDATE "+s.links.table+" SET account_id = ? WHERE account_id = ?", newID, oldID)
	_, _ = s.links.db.ExecContext(ctx, "UPDATE "+s.links.table+" SET token_id = ? WHERE token_id = ?", newID, oldID)
}

// maxLabelRunes is the longest label a person may give an account.
const maxLabelRunes = 120

// Rename changes an account's label. The id stays, so every binding, choice and
// link to it keeps working: they hold the id, never the label. An external
// account is not renamed (its label is how the sign-in it records is found),
// and neither is a connection's password (its label comes from the connection).
func (s *Service) Rename(id, label string) (Account, error) {
	label = strings.TrimSpace(label)
	if label == "" || utf8.RuneCountInString(label) > maxLabelRunes {
		return Account{}, ErrLabelInvalid
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return Account{}, ErrLabelInvalid
		}
	}
	record, err := s.items.Get(id)
	if err == nil {
		account, decodeErr := decodeMetadata(record)
		if decodeErr != nil {
			return Account{}, decodeErr
		}
		if account.Kind == KindExternal {
			return Account{}, ErrLabelInvalid
		}
		m := metadataOf(account)
		m.Label = label
		raw, marshalErr := json.Marshal(m)
		if marshalErr != nil {
			return Account{}, marshalErr
		}
		if _, err := s.items.UpdateMetadata(id, raw); err != nil {
			return Account{}, err
		}
		s.changed()
		return s.Get(id)
	}
	if s.scoped == nil {
		return Account{}, ErrNotFound
	}
	row, err := s.scoped.byID(id)
	if err != nil {
		return Account{}, err
	}
	if !strings.HasPrefix(row.ScopeOwner, LocalOwnerPrefix) {
		return Account{}, ErrLabelInvalid
	}
	if err := s.scoped.setLabel(id, label); err != nil {
		return Account{}, err
	}
	s.changed()
	return s.Get(id)
}
