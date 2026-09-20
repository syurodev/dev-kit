package bridge

import (
	"context"

	"devkit/internal/accounts"
	"devkit/internal/app"
	"devkit/internal/preferences"
)

// Accounts crosses the bridge as metadata only. There is no method here that
// returns a credential, and accounts:reveal is a capability the MCP subject is
// never granted — an agent cannot read a value because there is no door, not
// because a check says no.

func (s *AppService) AccountsList() ([]accounts.Account, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:list", map[string]any{
		"action": "list",
	}, func(context.Context) (any, error) {
		return s.container.Accounts.List()
	})
	if err != nil {
		return nil, err
	}
	return result.([]accounts.Account), nil
}

func (s *AppService) AccountsGet(id string) (accounts.Account, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:"+opaqueID, map[string]any{
		"action":     "get",
		"account_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Accounts.Get(id)
	})
	if err != nil {
		return accounts.Account{}, err
	}
	return result.(accounts.Account), nil
}

func (s *AppService) AccountsProviders() ([]accounts.Provider, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:list", map[string]any{
		"action": "providers",
	}, func(context.Context) (any, error) {
		return s.container.Accounts.Providers(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]accounts.Provider), nil
}

// AccountsCreate stores a credential. The value arrives once, here, and is
// never handed back by any method on this service.
func (s *AppService) AccountsCreate(provider, kind, label, user, secret string, env []string, scopeOwner, ref string) (accounts.Account, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:new", map[string]any{
		"action":   "create",
		"provider": provider,
		"kind":     kind,
		// Deliberately not the label: a user naming an account after the thing
		// it unlocks is common enough that the audit line should not carry it.
	}, func(context.Context) (any, error) {
		return s.container.Accounts.Create(accounts.CreateInput{
			Provider: provider, Kind: accounts.Kind(kind), Label: label,
			User: user, Secret: secret, Env: env, ScopeOwner: scopeOwner, Ref: ref,
		})
	})
	if err != nil {
		return accounts.Account{}, err
	}
	return result.(accounts.Account), nil
}

// AccountsRotate replaces a value in place, which is why every binding holds
// an id: nothing bound to this account needs editing afterwards.
func (s *AppService) AccountsRotate(id, user, secret string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:"+opaqueID, map[string]any{
		"action":     "rotate",
		"account_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Accounts.Rotate(id, user, secret)
	})
	return err
}

func (s *AppService) AccountsPromote(id, label string) (accounts.Account, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:"+opaqueID, map[string]any{
		"action":     "promote",
		"account_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Accounts.Promote(id, label)
	})
	if err != nil {
		return accounts.Account{}, err
	}
	return result.(accounts.Account), nil
}

func (s *AppService) AccountsDelete(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:"+opaqueID, map[string]any{
		"action":     "delete",
		"account_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Accounts.Delete(id)
	})
	return err
}

// AccountsUsedBy names what still binds an account, so a refusal to delete is
// actionable rather than a flat no.
func (s *AppService) AccountsUsedBy(id string) ([]string, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:"+opaqueID, map[string]any{
		"action":     "used_by",
		"account_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Accounts.UsedBy(id), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

// AccountsVerify asks the provider who a credential belongs to.
//
// It is the module's headline action, and deliberately not on the MCP surface:
// it spends a real request against a real provider, and an agent that can
// trigger it can probe which credentials exist by watching what changes.
func (s *AppService) AccountsVerify(id string) (accounts.Account, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:"+opaqueID, map[string]any{
		"action":     "verify",
		"account_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		return s.container.Accounts.Verify(ctx, id)
	})
	if err != nil {
		return accounts.Account{}, err
	}
	return result.(accounts.Account), nil
}

// AccountsMigrateProfiles copies database, SSH and Kafka secrets out of their
// profile payloads and into Accounts, leaving the payloads alone.
//
// Copying is what makes it safe to run anywhere, in any order: a payload syncs
// and a scoped account does not, so emptying one here would leave every other
// device holding a profile it cannot use. Clearing is a second, explicit call.
func (s *AppService) AccountsMigrateProfiles() (app.MigrationReport, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:new", map[string]any{
		"action": "migrate_profiles",
	}, func(context.Context) (any, error) {
		return app.MigrateLegacyCredentials(s.container)
	})
	if err != nil {
		return app.MigrationReport{}, err
	}
	return result.(app.MigrationReport), nil
}

// AccountsStopSyncingProfileSecrets empties the profile payloads whose secret
// is already in Accounts.
//
// This is the moment connection passwords stop syncing, and every other device
// will ask for them again. Nothing calls it automatically, and a profile with
// no local copy is skipped rather than emptied.
func (s *AppService) AccountsStopSyncingProfileSecrets() (app.MigrationReport, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:*", map[string]any{
		"action": "stop_syncing_profile_secrets",
	}, func(context.Context) (any, error) {
		return app.ClearLegacyCredentialPayloads(s.container)
	})
	if err != nil {
		return app.MigrationReport{}, err
	}
	return result.(app.MigrationReport), nil
}

// RevealedCredential is the one credential shape that crosses this bridge.
//
// Every other path resolves inside Go at the moment of use. This one exists
// because the alternative is worse: somebody who cannot read back the token
// they saved keeps a second copy of it in a note somewhere DevKit did not
// seal. It is reachable only from the UI — accounts:reveal is a capability the
// MCP subject is never granted, so an agent cannot get here because there is
// no door, not because a check says no.
type RevealedCredential struct {
	User   string `json:"user"`
	Secret string `json:"secret"`
	Kind   string `json:"kind"`
}

// AccountsRevealRequiresPassword reports whether revealing asks for the Master
// Password again. It is read under accounts:read rather than a preferences
// capability because the answer changes what the reveal dialog must collect.
func (s *AppService) AccountsRevealRequiresPassword() (bool, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:list", map[string]any{
		"action": "reveal_policy",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return preferences.DefaultRevealRequiresPassword, nil
		}
		return s.container.Preferences.RevealRequiresPassword()
	})
	if err != nil {
		return true, err
	}
	return result.(bool), nil
}

func (s *AppService) AccountsSetRevealRequiresPassword(required bool) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "accounts_reveal", map[string]any{
		"action":   "set_reveal_policy",
		"required": required,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return nil, s.container.Preferences.SetRevealRequiresPassword(required)
	})
	return err
}

// AccountsReveal shows one account's value.
//
// The Master Password is re-checked here when the preference says so, and
// checking it is not unlocking: VerifyMasterPassword derives, compares and
// throws the key away, so a mistyped answer in this dialog cannot lock a vault
// that is open.
//
// The audit line carries the provider and the subject and never the value —
// an audit trail that records what it was guarding is not a guard.
func (s *AppService) AccountsReveal(id, masterPassword string) (RevealedCredential, error) {
	opaqueID := recordID(id)
	account, err := s.container.Accounts.Get(id)
	if err != nil {
		return RevealedCredential{}, err
	}
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:reveal", "account:"+opaqueID, map[string]any{
		"action":     "reveal",
		"account_id": opaqueID,
		"provider":   account.Provider,
		"subject":    account.Subject,
	}, func(context.Context) (any, error) {
		required := preferences.DefaultRevealRequiresPassword
		if s.container.Preferences != nil {
			required, err = s.container.Preferences.RevealRequiresPassword()
			if err != nil {
				return nil, err
			}
		}
		if required {
			if s.container.Vault == nil {
				return nil, errPreferencesUnavailable
			}
			if err := s.container.Vault.VerifyMasterPassword(masterPassword); err != nil {
				return nil, err
			}
		}
		return s.container.Accounts.Reveal(id)
	})
	if err != nil {
		return RevealedCredential{}, err
	}
	credential := result.(accounts.Credential)
	return RevealedCredential{
		User: credential.User, Secret: credential.Secret, Kind: string(credential.Kind),
	}, nil
}

// AccountsForHost answers which account authenticates to a host, and says when
// it cannot answer. Two accounts matching one host is a question, never a
// guess: two GitHub accounts are usually a personal one and a company one.
func (s *AppService) AccountsForHost(host, env string) (accounts.HostMatch, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:list", map[string]any{
		"action": "for_host",
		"host":   host,
		"env":    env,
	}, func(context.Context) (any, error) {
		return s.container.Accounts.ForHost(host, env)
	})
	if err != nil {
		return accounts.HostMatch{}, err
	}
	return result.(accounts.HostMatch), nil
}

// AccountsChooseForHost records the answer, so the pair is asked once.
func (s *AppService) AccountsChooseForHost(host, env, accountID string) error {
	_, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:*", map[string]any{
		"action": "choose_for_host",
		"host":   host,
		"env":    env,
	}, func(context.Context) (any, error) {
		return nil, s.container.Accounts.ChooseForHost(host, env, accountID)
	})
	return err
}

// AccountsForgetHostChoice puts the question back.
func (s *AppService) AccountsForgetHostChoice(host, env string) error {
	_, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:*", map[string]any{
		"action": "forget_host_choice",
		"host":   host,
		"env":    env,
	}, func(context.Context) (any, error) {
		return nil, s.container.Accounts.ForgetHostChoice(host, env)
	})
	return err
}
