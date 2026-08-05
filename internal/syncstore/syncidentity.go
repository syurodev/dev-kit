package syncstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const localIdentityRowID = "local"

// SQLDeviceIdentityStore persists only the installation's stable device ID.
// Account identity belongs to the authenticated backend session and must never
// be minted locally or trusted from this store.
type SQLDeviceIdentityStore struct {
	db *sql.DB
}

func NewSQLDeviceIdentityStore(db *sql.DB) *SQLDeviceIdentityStore {
	return &SQLDeviceIdentityStore{db: db}
}

// EnsureLocalDevice returns the stable local device ID, creating it on first
// use. The legacy account_id column is deliberately written as empty so it
// cannot pass SyncContext validation or be mistaken for authenticated identity.
func (s *SQLDeviceIdentityStore) EnsureLocalDevice(ctx context.Context) (deviceID string, err error) {
	if s == nil || s.db == nil {
		return "", errors.New("device identity store is nil")
	}
	ctx = nonNilContext(ctx)

	var storedDevice string
	err = s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT device_id FROM %s WHERE id = ?", TableSyncIdentity,
	), localIdentityRowID).Scan(&storedDevice)
	if err == nil {
		return storedDevice, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	deviceID, err = newSyncIdentityID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, account_id, device_id, created_at) VALUES (?, ?, ?, ?)",
		TableSyncIdentity,
	), localIdentityRowID, "", deviceID, formatTime(time.Now().UTC()))
	if err != nil {
		return "", err
	}
	return deviceID, nil
}

func newSyncIdentityID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
