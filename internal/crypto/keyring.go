package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
)

const (
	keyringDEKKeyID       = "dek"
	keyringRecordType     = "keyring"
	keyringMasterWrapID   = "master"
	keyringRecoveryWrapID = "recovery"
)

func keyringMasterAssociatedData() AssociatedData {
	return AssociatedData{RecordType: keyringRecordType, RecordID: keyringMasterWrapID, KeyID: keyringDEKKeyID}
}

func keyringRecoveryAssociatedData() AssociatedData {
	return AssociatedData{RecordType: keyringRecordType, RecordID: keyringRecoveryWrapID, KeyID: keyringDEKKeyID}
}

// Keyring is the persisted key material for a vault. It never contains the
// plaintext DEK: the random DEK is wrapped once by the master-password key and
// once by the recovery key.
type Keyring struct {
	Version         int       `json:"version"`
	MasterKDF       KDFParams `json:"master_kdf"`
	MasterWrapped   Envelope  `json:"master_wrapped"`
	RecoveryKDF     KDFParams `json:"recovery_kdf"`
	RecoveryWrapped Envelope  `json:"recovery_wrapped"`
}

// NewKeyring generates a random DEK, wraps it with a key derived from
// masterPassword, and also wraps it with a freshly generated recovery key. The
// plaintext recovery key is returned once and is never stored.
func NewKeyring(masterPassword string) (*Keyring, string, error) {
	if masterPassword == "" {
		return nil, "", errors.New("master password is required")
	}

	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, "", err
	}

	masterKDF, err := DefaultKDFParams()
	if err != nil {
		return nil, "", err
	}
	masterKey, err := DeriveKey(masterPassword, masterKDF)
	if err != nil {
		return nil, "", err
	}
	masterWrapped, err := SealV2(keyringMasterAssociatedData(), masterKey, dek)
	if err != nil {
		return nil, "", err
	}

	recoveryKey, err := newRecoveryKey()
	if err != nil {
		return nil, "", err
	}
	recoveryKDF, err := DefaultKDFParams()
	if err != nil {
		return nil, "", err
	}
	recoveryDerived, err := DeriveKey(recoveryKey, recoveryKDF)
	if err != nil {
		return nil, "", err
	}
	recoveryWrapped, err := SealV2(keyringRecoveryAssociatedData(), recoveryDerived, dek)
	if err != nil {
		return nil, "", err
	}

	return &Keyring{
		Version:         1,
		MasterKDF:       masterKDF,
		MasterWrapped:   masterWrapped,
		RecoveryKDF:     recoveryKDF,
		RecoveryWrapped: recoveryWrapped,
	}, recoveryKey, nil
}

// UnlockWithPassword returns the plaintext DEK when masterPassword is correct.
func (k *Keyring) UnlockWithPassword(masterPassword string) ([]byte, error) {
	key, err := DeriveKey(masterPassword, k.MasterKDF)
	if err != nil {
		return nil, err
	}
	dek, err := OpenWithAD(key, k.MasterWrapped, keyringMasterAssociatedData())
	if err != nil {
		return nil, errors.New("invalid master password")
	}
	return dek, nil
}

// UnlockWithRecovery returns the plaintext DEK when recoveryKey is correct.
func (k *Keyring) UnlockWithRecovery(recoveryKey string) ([]byte, error) {
	key, err := DeriveKey(recoveryKey, k.RecoveryKDF)
	if err != nil {
		return nil, err
	}
	dek, err := OpenWithAD(key, k.RecoveryWrapped, keyringRecoveryAssociatedData())
	if err != nil {
		return nil, errors.New("invalid recovery key")
	}
	return dek, nil
}

// ChangePassword rewraps the existing DEK under a new master password. The DEK
// is unchanged, so existing records stay decryptable.
func (k *Keyring) ChangePassword(oldPassword string, newPassword string) error {
	if newPassword == "" {
		return errors.New("new master password is required")
	}
	dek, err := k.UnlockWithPassword(oldPassword)
	if err != nil {
		return err
	}
	kdf, err := DefaultKDFParams()
	if err != nil {
		return err
	}
	newKey, err := DeriveKey(newPassword, kdf)
	if err != nil {
		return err
	}
	wrapped, err := SealV2(keyringMasterAssociatedData(), newKey, dek)
	if err != nil {
		return err
	}
	k.MasterKDF = kdf
	k.MasterWrapped = wrapped
	return nil
}

func newRecoveryKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
