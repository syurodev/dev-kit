package accounts

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// choiceStore remembers which account answered "which of these" for a host on
// an environment. Local and non-syncing: see storage.TableAccountHostChoices.
type choiceStore struct {
	db    *sql.DB
	table string
}

func (c *choiceStore) get(host, env string) (string, error) {
	var id string
	err := c.db.QueryRowContext(context.Background(),
		"SELECT account_id FROM "+c.table+" WHERE host = ? AND env = ?", host, env).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (c *choiceStore) put(host, env, accountID string) error {
	_, err := c.db.ExecContext(context.Background(),
		`INSERT INTO `+c.table+` (host, env, account_id, chosen_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(host, env) DO UPDATE SET account_id = excluded.account_id, chosen_at = excluded.chosen_at`,
		host, env, accountID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (c *choiceStore) forget(host, env string) error {
	_, err := c.db.ExecContext(context.Background(),
		"DELETE FROM "+c.table+" WHERE host = ? AND env = ?", host, env)
	return err
}

// SetHostChoices wires the table that remembers a host's answer.
//
// It is optional on purpose: without it ForHost still decides correctly, it
// simply asks every time. A missing memory is an inconvenience; a wrong
// automatic answer is a request sent as the wrong identity.
func (s *Service) SetHostChoices(db *sql.DB, table string) {
	if db != nil && table != "" {
		s.choices = &choiceStore{db: db, table: table}
	}
}

// HostMatch is what the accounts for a host come to.
type HostMatch struct {
	// Decided is true when there is nothing to ask: exactly one account
	// matches, or a previous answer for this pair still does.
	Decided bool `json:"decided"`
	// Account is the one to use when Decided. Zero otherwise.
	Account Account `json:"account"`
	// Options is every match, for the caller to offer when Decided is false.
	// It is also filled when Decided is true by a remembered answer, so the UI
	// can show that there was a choice and offer to change it.
	Options []Account `json:"options"`
	// Remembered says Decided came from a stored answer rather than from
	// there being only one candidate.
	Remembered bool `json:"remembered"`
}

// ForHost answers which account authenticates to a host on an environment.
//
// One match is chosen and nothing is remembered — remembering a forced answer
// is noise, and it would go stale the moment a second account is added.
//
// Two or more is a question, never a guess. Two GitHub accounts are usually a
// personal one and a company one, and firing a request as the wrong identity
// is worse than asking once. There is deliberately no "the one you used last"
// heuristic: that is a guess wearing a memory's clothes.
func (s *Service) ForHost(host, env string) (HostMatch, error) {
	host = normalizeHost(host)
	options, err := s.hostCandidates(host, env, ViaHTTPS)
	if err != nil {
		return HostMatch{}, err
	}

	switch len(options) {
	case 0:
		return HostMatch{}, nil
	case 1:
		return HostMatch{Decided: true, Account: options[0], Options: options}, nil
	}

	if s.choices != nil {
		id, err := s.choices.get(host, env)
		if err != nil {
			return HostMatch{}, err
		}
		for _, option := range options {
			if option.ID == id {
				return HostMatch{Decided: true, Account: option, Options: options, Remembered: true}, nil
			}
		}
		if id != "" {
			// The remembered account is gone, or no longer matches this host
			// or this environment. Keeping the row would make the same stale
			// answer be ignored on every call for ever.
			if err := s.choices.forget(host, env); err != nil {
				return HostMatch{}, err
			}
		}
	}
	return HostMatch{Options: options}, nil
}

// ChooseForHost records the answer for a pair.
//
// The account has to be one ForHost would actually offer. Without that check
// any caller could pin an arbitrary account to a host, which is the same
// credential-misdirection this whole rule exists to prevent — just written
// down rather than guessed.
func (s *Service) ChooseForHost(host, env, accountID string) error {
	if s.choices == nil {
		return nil
	}
	match, err := s.ForHost(host, env)
	if err != nil {
		return err
	}
	for _, option := range match.Options {
		if option.ID == accountID {
			return s.choices.put(normalizeHost(host), env, accountID)
		}
	}
	return ErrInvalid
}

// ForgetHostChoice drops a remembered answer, so the next call asks again.
func (s *Service) ForgetHostChoice(host, env string) error {
	if s.choices == nil {
		return nil
	}
	return s.choices.forget(normalizeHost(host), env)
}

// normalizeHost matches what the provider registry does, so a choice is stored
// under the same key the lookup uses.
func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

// Via is how a git remote is reached, which decides what kind of account can
// authenticate it: a token or password for HTTPS, a key for SSH.
type Via string

const (
	ViaHTTPS Via = "https"
	ViaSSH   Via = "ssh"
)

func (v Via) accepts(kind Kind) bool {
	if v == ViaSSH {
		return kind == KindSSHKey
	}
	// HTTPS, and the API client's ForHost: anything that is sent as a
	// credential on a request. A key is not one.
	return kind != KindSSHKey && kind != KindExternal
}

// hostCandidates is every account that could authenticate to host on env over
// via: the provider's hosts cover it, the account is shared or local (not one
// connection's), it is the right kind for the transport, and its environment
// list allows env. Sorted by label so a list shown to the person is stable.
func (s *Service) hostCandidates(host, env string, via Via) ([]Account, error) {
	if host == "" {
		return nil, nil
	}
	providers := s.providers.ProvidersForHost(host)
	if len(providers) == 0 {
		return nil, nil
	}
	wanted := map[string]bool{}
	for _, id := range providers {
		wanted[id] = true
	}

	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var options []Account
	for _, account := range all {
		// A connection's scoped account belongs to that connection, never to a
		// host. A local account is the person's own and is offered like a
		// shared one.
		if account.Scoped() && !account.Local() {
			continue
		}
		if !via.accepts(account.Kind) {
			continue
		}
		if !wanted[account.Provider] || !account.AllowsEnv(env) {
			continue
		}
		options = append(options, account)
	}
	sort.SliceStable(options, func(i, j int) bool { return options[i].Label < options[j].Label })
	return options, nil
}
