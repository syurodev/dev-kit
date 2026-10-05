package accounts

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrIdentityInvalid is a name or email git would reject or misread as a
// different field.
var ErrIdentityInvalid = errors.New("accounts.identityInvalid")

// Identity is who commits are written as when an account is the one in use:
// the name and email git puts on a commit. A work account and a personal one
// differ here as much as in their tokens, and a commit pushed under the wrong
// email is one the person cannot take back.
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Complete says both halves are there. One half alone is not applied: git would
// take the other from whatever configuration is nearest, and a commit written
// as the work name with the personal email is worse than either.
func (i Identity) Complete() bool { return i.Name != "" && i.Email != "" }

var emailShape = regexp.MustCompile(`^[^\s<>@]+@[^\s<>@]+$`)

func (i Identity) normalised() (Identity, error) {
	i.Name, i.Email = strings.TrimSpace(i.Name), strings.TrimSpace(i.Email)
	if i.Name == "" && i.Email == "" {
		return i, nil
	}
	if i.Name == "" || i.Email == "" ||
		utf8.RuneCountInString(i.Name) > 200 || len(i.Email) > 254 ||
		strings.ContainsAny(i.Name, "<>") || !emailShape.MatchString(i.Email) {
		return i, ErrIdentityInvalid
	}
	// A newline or any control character in either would end the field or the
	// whole header when git writes it into a commit.
	for _, r := range i.Name + i.Email {
		if unicode.IsControl(r) {
			return i, ErrIdentityInvalid
		}
	}
	return i, nil
}

type identityStore struct {
	db    *sql.DB
	table string
}

// SetIdentities wires the local table that holds account identities. Without it
// accounts simply have none, and commits are written as git's own
// configuration says.
func (s *Service) SetIdentities(db *sql.DB, table string) {
	if db != nil && table != "" {
		s.identities = &identityStore{db: db, table: table}
	}
}

// SetIdentity records who commits are written as under an account. Both fields
// empty clears it.
func (s *Service) SetIdentity(id string, in Identity) (Account, error) {
	if s.identities == nil {
		return Account{}, ErrInvalid
	}
	if _, err := s.Get(id); err != nil {
		return Account{}, err
	}
	identity, err := in.normalised()
	if err != nil {
		return Account{}, err
	}
	if identity == (Identity{}) {
		_, err = s.identities.db.ExecContext(context.Background(),
			"DELETE FROM "+s.identities.table+" WHERE account_id = ?", id)
	} else {
		_, err = s.identities.db.ExecContext(context.Background(),
			`INSERT INTO `+s.identities.table+` (account_id, name, email, updated_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT(account_id) DO UPDATE SET name = excluded.name, email = excluded.email, updated_at = excluded.updated_at`,
			id, identity.Name, identity.Email, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return Account{}, err
	}
	return s.Get(id)
}

// IdentityOf is the identity recorded for an account, if one is complete.
func (s *Service) IdentityOf(id string) (Identity, bool) {
	if s.identities == nil || id == "" {
		return Identity{}, false
	}
	var identity Identity
	err := s.identities.db.QueryRowContext(context.Background(),
		"SELECT name, email FROM "+s.identities.table+" WHERE account_id = ?", id).Scan(&identity.Name, &identity.Email)
	if err != nil || !identity.Complete() {
		return Identity{}, false
	}
	return identity, true
}

// allIdentities is every recorded identity, for listing without a query each.
func (s *Service) allIdentities() map[string]Identity {
	out := map[string]Identity{}
	if s.identities == nil {
		return out
	}
	rows, err := s.identities.db.QueryContext(context.Background(),
		"SELECT account_id, name, email FROM "+s.identities.table)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var identity Identity
		if rows.Scan(&id, &identity.Name, &identity.Email) == nil {
			out[id] = identity
		}
	}
	return out
}

func (s *Service) forgetIdentity(id string) {
	if s.identities != nil {
		_, _ = s.identities.db.ExecContext(context.Background(),
			"DELETE FROM "+s.identities.table+" WHERE account_id = ?", id)
	}
}

// withIdentity fills the identity fields of an account that crosses the bridge.
func withIdentity(account Account, identity Identity, ok bool) Account {
	if ok {
		account.AuthorName, account.AuthorEmail = identity.Name, identity.Email
	}
	return account
}
