package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"devkit/internal/gitssh"
)

// SetSSHIdentifier replaces how an SSH key is checked against its provider, so
// a test can answer without a network. Production uses ssh itself.
func (s *Service) SetSSHIdentifier(identify func(ctx context.Context, m gitssh.Material, host string) (string, error)) {
	s.identifySSH = identify
}

// ResolveSSHKey returns an SSH-key account's key and passphrase for the push
// about to use it. Like Resolve, nothing it returns crosses the bridge.
func (s *Service) ResolveSSHKey(id, env, by string) (gitssh.Material, error) {
	credential, err := s.Resolve(ResolveInput{AccountID: id, Env: env, By: by})
	if err != nil {
		return gitssh.Material{}, err
	}
	if credential.Kind != KindSSHKey {
		return gitssh.Material{}, ErrNotSSHKey
	}
	material, err := gitssh.Unpack(credential.Secret)
	if err != nil {
		return gitssh.Material{}, ErrKeyInvalid
	}
	return material, nil
}

// SSHKeyInfo describes an SSH-key account's key without revealing it: the
// fingerprint to match against what GitHub lists, and the public line to paste
// into a provider's settings.
func (s *Service) SSHKeyInfo(id string) (gitssh.KeyInfo, error) {
	material, err := s.ResolveSSHKey(id, "", "accounts")
	if err != nil {
		return gitssh.KeyInfo{}, err
	}
	return gitssh.Inspect(material)
}

// kindOf is the kind of an account of either store.
func (s *Service) kindOf(id string) (Kind, bool) {
	if record, err := s.items.Get(id); err == nil {
		if account, decodeErr := decodeMetadata(record); decodeErr == nil {
			return account.Kind, true
		}
		return "", false
	}
	if row, ok := s.localRow(id); ok {
		return row.Kind, true
	}
	return "", false
}

// verifySSHKey asks the provider, over SSH, whose key this is.
//
// It is the HTTPS verify's counterpart and keeps its rule: a provider saying
// no is an answer worth recording (the key was removed from the account), and
// a network failure is not — it says nothing about the key.
func (s *Service) verifySSHKey(ctx context.Context, id string) (Account, error) {
	account, err := s.Get(id)
	if err != nil {
		return Account{}, err
	}
	provider, ok := s.providers.Get(account.Provider)
	if !ok || provider.SSHHost == "" {
		return account, ErrNotVerifiable
	}
	material, err := s.ResolveSSHKey(id, "", "accounts")
	if err != nil {
		return account, err
	}
	identify := s.identifySSH
	if identify == nil {
		identify = gitssh.Identify
	}
	subject, probeErr := identify(ctx, material, provider.SSHHost)

	state := StateValid
	switch {
	case probeErr == nil:
	case errors.Is(probeErr, gitssh.ErrNotAuthenticated):
		state, subject = StateRevoked, ""
	default:
		return account, probeErr
	}
	return s.recordVerification(id, account, subject, state)
}

// recordVerification stores what a provider said about an SSH-key account, in
// whichever store holds it.
func (s *Service) recordVerification(id string, account Account, subject string, state State) (Account, error) {
	if account.Local() {
		if err := s.scoped.markVerified(id, subject, state); err != nil {
			return Account{}, err
		}
		return s.Get(id)
	}
	record, err := s.items.Get(id)
	if err != nil {
		return Account{}, ErrNotFound
	}
	m := metadataOf(account)
	m.Subject, m.State, m.VerifiedAt = subject, string(state), formatTime(time.Now())
	raw, err := json.Marshal(m)
	if err != nil {
		return Account{}, err
	}
	if _, err := s.items.UpdateMetadata(id, raw); err != nil {
		return Account{}, err
	}
	return s.decorate(m.account(id, record.CreatedAt, time.Now())), nil
}
