package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	TableSyncIdentity   = "sync_identity"
	localIdentityRowID  = "local"
)

// SQLIdentityStore persists the local sync account and device identifiers.
type SQLIdentityStore struct {
	db *sql.DB
}

func NewSQLIdentityStore(db *sql.DB) *SQLIdentityStore {
	return &SQLIdentityStore{db: db}
}

// EnsureLocal returns stable local account and device IDs, creating them on
// first use.
func (s *SQLIdentityStore) EnsureLocal(ctx context.Context) (accountID, deviceID string, err error) {
	if s == nil || s.db == nil {
		return "", "", errors.New("identity store is nil")
	}
	ctx = nonNilContext(ctx)

	var storedAccount, storedDevice string
	err = s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT account_id, device_id FROM %s WHERE id = ?", TableSyncIdentity,
	), localIdentityRowID).Scan(&storedAccount, &storedDevice)
	if err == nil {
		return storedAccount, storedDevice, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}

	accountID, err = newSyncIdentityID()
	if err != nil {
		return "", "", err
	}
	deviceID, err = newSyncIdentityID()
	if err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, account_id, device_id, created_at) VALUES (?, ?, ?, ?)",
		TableSyncIdentity,
	), localIdentityRowID, accountID, deviceID, formatTime(time.Now().UTC()))
	if err != nil {
		return "", "", err
	}
	return accountID, deviceID, nil
}

func newSyncIdentityID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
