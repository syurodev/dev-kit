package app

import (
	"context"

	"devkit/internal/agentwork"
	"devkit/internal/vault"
)

// managedVaultPayloadCodec adapts the application's unlocked vault to the
// managed-payload interface. The AD is constructed from the complete immutable
// scope by agentwork; callers cannot supply arbitrary crypto identity fields.
type managedVaultPayloadCodec struct {
	vault interface {
		IsLocked() bool
		SealRecord(vault.RecordBinding, []byte) ([]byte, error)
		OpenRecord(vault.RecordBinding, []byte) ([]byte, error)
	}
}

func (c managedVaultPayloadCodec) IsLocked() bool { return c.vault == nil || c.vault.IsLocked() }

func (c managedVaultPayloadCodec) Seal(ctx context.Context, binding agentwork.PayloadBinding, plain []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.IsLocked() {
		return nil, agentwork.ErrManagedPayloadLocked
	}
	if len(plain) == 0 || len(plain) > agentwork.ManagedArtifactBodyMaxBytes {
		return nil, agentwork.ErrManagedPayloadLimit
	}
	ad, err := managedPayloadRecordBinding(binding)
	if err != nil {
		return nil, err
	}
	encoded, err := c.vault.SealRecord(ad, plain)
	if err != nil {
		if c.IsLocked() {
			return nil, agentwork.ErrManagedPayloadLocked
		}
		return nil, err
	}
	if len(encoded) > 1_500_000 {
		return nil, agentwork.ErrManagedPayloadLimit
	}
	return encoded, nil
}

func (c managedVaultPayloadCodec) Open(ctx context.Context, binding agentwork.PayloadBinding, sealed []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.IsLocked() {
		return nil, agentwork.ErrManagedPayloadLocked
	}
	if len(sealed) == 0 || len(sealed) > 1_500_000 {
		return nil, agentwork.ErrManagedPayloadLimit
	}
	ad, err := managedPayloadRecordBinding(binding)
	if err != nil {
		return nil, err
	}
	plain, err := c.vault.OpenRecord(ad, sealed)
	if err != nil {
		if c.IsLocked() {
			return nil, agentwork.ErrManagedPayloadLocked
		}
		return nil, agentwork.ErrManagedPayloadScope
	}
	if len(plain) == 0 || len(plain) > agentwork.ManagedArtifactBodyMaxBytes {
		return nil, agentwork.ErrManagedPayloadLimit
	}
	return plain, nil
}

func managedPayloadRecordBinding(binding agentwork.PayloadBinding) (vault.RecordBinding, error) {
	typ, err := binding.RecordType()
	if err != nil {
		return vault.RecordBinding{}, err
	}
	key, err := binding.KeyID()
	if err != nil {
		return vault.RecordBinding{}, err
	}
	return vault.RecordBinding{RecordType: typ, RecordID: binding.RecordID, KeyID: key}, nil
}
