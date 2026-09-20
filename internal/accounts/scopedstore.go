package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devkit/internal/crypto"
)

// scopedStore holds the credentials that never leave this machine.
//
// It is a plain table rather than a vault item because vault_items replicates
// unconditionally; the envelope is still sealed with the vault DEK, so "does
// not sync" costs nothing in encryption at rest. Its AAD record type is
// ScopedRecordType, which no vault item uses, so an envelope written here can
// never open as a shared account.
type scopedStore struct {
	db    *sql.DB
	table string
	vault Vault
}

// Vault is the sealing surface, declared here rather than imported so the
// package accepts an interface and stays testable without a real vault.
type Vault interface {
	SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, ad crypto.AssociatedData) ([]byte, error)
}

type scopedRow struct {
	ID         string
	ScopeOwner string
	Provider   string
	Kind       Kind
	Label      string
	Username   string
	Envelope   crypto.Envelope
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s *scopedStore) upsert(row scopedRow, secret string) error {
	ad := crypto.AssociatedData{RecordType: ScopedRecordType, RecordID: row.ID, KeyID: KeyID}
	envelope, err := s.vault.SealV2(ad, []byte(secret))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(fmt.Sprintf(
		`INSERT INTO %s (id, scope_owner, provider, kind, label, username, envelope, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(scope_owner) DO UPDATE SET
			provider = excluded.provider,
			kind = excluded.kind,
			label = excluded.label,
			username = excluded.username,
			envelope = excluded.envelope,
			updated_at = excluded.updated_at`, s.table),
		row.ID, row.ScopeOwner, row.Provider, string(row.Kind), row.Label, row.Username,
		string(raw), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *scopedStore) byOwner(owner string) (scopedRow, error) {
	return s.scanOne(s.db.QueryRow(fmt.Sprintf(
		`SELECT id, scope_owner, provider, kind, label, username, envelope, created_at, updated_at
		 FROM %s WHERE scope_owner = ?`, s.table), owner))
}

func (s *scopedStore) byID(id string) (scopedRow, error) {
	return s.scanOne(s.db.QueryRow(fmt.Sprintf(
		`SELECT id, scope_owner, provider, kind, label, username, envelope, created_at, updated_at
		 FROM %s WHERE id = ?`, s.table), id))
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *scopedStore) scanOne(row rowScanner) (scopedRow, error) {
	var (
		out                 scopedRow
		kind, envelope      string
		createdAt, updateAt string
	)
	err := row.Scan(&out.ID, &out.ScopeOwner, &out.Provider, &kind, &out.Label, &out.Username,
		&envelope, &createdAt, &updateAt)
	if errors.Is(err, sql.ErrNoRows) {
		return scopedRow{}, ErrNotFound
	}
	if err != nil {
		return scopedRow{}, err
	}
	out.Kind = Kind(kind)
	if err := json.Unmarshal([]byte(envelope), &out.Envelope); err != nil {
		return scopedRow{}, err
	}
	out.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	out.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updateAt)
	return out, nil
}

func (s *scopedStore) list() ([]scopedRow, error) {
	rows, err := s.db.Query(fmt.Sprintf(
		`SELECT id, scope_owner, provider, kind, label, username, envelope, created_at, updated_at
		 FROM %s ORDER BY created_at`, s.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scopedRow
	for rows.Next() {
		row, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *scopedStore) open(row scopedRow) (string, error) {
	ad := crypto.AssociatedData{RecordType: ScopedRecordType, RecordID: row.ID, KeyID: KeyID}
	plaintext, err := s.vault.OpenWithAD(row.Envelope, ad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *scopedStore) deleteByOwner(owner string) error {
	_, err := s.db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE scope_owner = ?`, s.table), owner)
	return err
}

func (s *scopedStore) deleteByID(id string) error {
	_, err := s.db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.table), id)
	return err
}

// orphans are rows whose owning profile is gone. A profile deleted on another
// device syncs its deletion here, and the local secret it owned has nothing
// left to belong to.
func (s *scopedStore) orphans(owners map[string]bool) ([]scopedRow, error) {
	all, err := s.list()
	if err != nil {
		return nil, err
	}
	var out []scopedRow
	for _, row := range all {
		if !owners[row.ScopeOwner] {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r scopedRow) account(now time.Time) Account {
	return Account{
		ID:         r.ID,
		Provider:   r.Provider,
		Kind:       r.Kind,
		Label:      r.Label,
		ScopeOwner: r.ScopeOwner,
		// A scoped credential is not an identity a provider can confirm; the
		// owning module's Test Connection is what checks it.
		State:     StateUnverified,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
