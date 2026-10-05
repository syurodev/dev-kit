package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// remoteChoiceStore remembers which account answered "which of these" for the
// owner of a repository on a host. Local and non-syncing: see
// storage.TableAccountRemoteChoices.
type remoteChoiceStore struct {
	db    *sql.DB
	table string
}

func (c *remoteChoiceStore) get(host, owner string) (string, error) {
	var id string
	err := c.db.QueryRowContext(context.Background(),
		"SELECT account_id FROM "+c.table+" WHERE host = ? AND owner = ?", host, owner).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (c *remoteChoiceStore) put(host, owner, accountID string) error {
	_, err := c.db.ExecContext(context.Background(),
		`INSERT INTO `+c.table+` (host, owner, account_id, chosen_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(host, owner) DO UPDATE SET account_id = excluded.account_id, chosen_at = excluded.chosen_at`,
		host, owner, accountID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (c *remoteChoiceStore) forget(host, owner string) error {
	_, err := c.db.ExecContext(context.Background(),
		"DELETE FROM "+c.table+" WHERE host = ? AND owner = ?", host, owner)
	return err
}

// SetRemoteChoices wires the table that remembers which account a repository
// owner on a host was answered with.
//
// Optional for the same reason SetHostChoices is: without it ForRemote still
// decides correctly and simply asks every time.
func (s *Service) SetRemoteChoices(db *sql.DB, table string) {
	if db != nil && table != "" {
		s.remoteChoices = &remoteChoiceStore{db: db, table: table}
	}
}

func normalizeOwner(owner string) string { return strings.ToLower(strings.TrimSpace(owner)) }

// ForRemote answers which account authenticates a git remote, given the host
// and the owner (the first path segment: the user or organisation).
//
// It is ForHost with a finer question. Two GitHub accounts are a personal one
// and a company one, and they split by who owns the repository: acme-corp/*
// is the company account and everything else is the personal one. A remembered
// answer is therefore keyed by (host, owner) — asking once per organisation
// rather than once per repository, and never letting "company" for one
// organisation stand for the person's own repositories on the same host.
//
// The rules ForHost keeps hold here: one match is chosen and not remembered,
// two or more is a question and never a guess, and a remembered answer that no
// longer matches is dropped rather than ignored for ever.
func (s *Service) ForRemote(host, owner, env string) (HostMatch, error) {
	host, owner = normalizeHost(host), normalizeOwner(owner)
	options, err := s.hostCandidates(host, env)
	if err != nil {
		return HostMatch{}, err
	}
	switch len(options) {
	case 0:
		return HostMatch{}, nil
	case 1:
		return HostMatch{Decided: true, Account: options[0], Options: options}, nil
	}
	if s.remoteChoices == nil || owner == "" {
		return HostMatch{Options: options}, nil
	}
	id, err := s.remoteChoices.get(host, owner)
	if err != nil {
		return HostMatch{}, err
	}
	for _, option := range options {
		if option.ID == id {
			return HostMatch{Decided: true, Account: option, Options: options, Remembered: true}, nil
		}
	}
	if id != "" {
		if err := s.remoteChoices.forget(host, owner); err != nil {
			return HostMatch{}, err
		}
	}
	return HostMatch{Options: options}, nil
}

// ChooseForRemote records the answer for a (host, owner) pair.
//
// The account has to be one ForRemote would actually offer, so a caller cannot
// pin an arbitrary account to an owner — the credential misdirection this
// whole rule exists to prevent, just written down rather than guessed.
func (s *Service) ChooseForRemote(host, owner, env, accountID string) error {
	if s.remoteChoices == nil {
		return nil
	}
	host, owner = normalizeHost(host), normalizeOwner(owner)
	if owner == "" {
		return ErrInvalid
	}
	options, err := s.hostCandidates(host, env)
	if err != nil {
		return err
	}
	for _, option := range options {
		if option.ID == accountID {
			return s.remoteChoices.put(host, owner, accountID)
		}
	}
	return ErrInvalid
}

// ForgetRemoteChoice drops a remembered answer, so the next call asks again.
func (s *Service) ForgetRemoteChoice(host, owner string) error {
	if s.remoteChoices == nil {
		return nil
	}
	return s.remoteChoices.forget(normalizeHost(host), normalizeOwner(owner))
}
