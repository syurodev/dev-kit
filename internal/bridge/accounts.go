package bridge

import (
	"context"

	"devkit/internal/accounts"
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
