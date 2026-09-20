package app

import (
	"encoding/json"

	"devkit/internal/vaultitem"
)

// profileTypesWithEnv are record types storing metadata.env as ONE tag name.
// Each must block Environments.DeleteTag while it still names the tag.
var profileTypesWithEnv = []string{
	vaultitem.ItemTypeSSH,
	vaultitem.ItemTypeDatabase,
	vaultitem.ItemTypeKafka,
	// An API environment is a tag plus variables, and the tag is what decides
	// whether sending asks first. Deleting it out from under one would leave a
	// send that nothing could gate.
	vaultitem.ItemTypeAPIEnvironment,
	// A guard is an env tag plus a branch pattern, and a remote binding is an
	// env tag plus an account. Deleting a tag either still names would turn
	// off a gate without saying so.
	vaultitem.ItemTypeGitGuard,
	vaultitem.ItemTypeGitRemote,
}

// listEnvTypes store metadata.env as a LIST: an account may be restricted to
// several tags, or to none at all.
var listEnvTypes = []string{vaultitem.ItemTypeAccount}

type itemLister interface {
	ListByType(itemType string) ([]vaultitem.Record, error)
}

// vaultItemEnvUsage satisfies environments.ProfileEnvUsage by scanning
// plaintext metadata for a matching env name. Plaintext is the point: this
// answers while the vault is locked, as deleting a tag must.
type vaultItemEnvUsage struct {
	items itemLister
}

func newVaultItemEnvUsage(items itemLister) vaultItemEnvUsage {
	return vaultItemEnvUsage{items: items}
}

func (u vaultItemEnvUsage) EnvInUse(name string) (bool, error) {
	if name == "" || u.items == nil {
		return false, nil
	}
	for _, typ := range profileTypesWithEnv {
		recs, err := u.items.ListByType(typ)
		if err != nil {
			return false, err
		}
		for _, rec := range recs {
			var meta struct {
				Env string `json:"env"`
			}
			if err := json.Unmarshal(rec.Metadata, &meta); err != nil {
				return false, err
			}
			if meta.Env == name {
				return true, nil
			}
		}
	}
	for _, typ := range listEnvTypes {
		recs, err := u.items.ListByType(typ)
		if err != nil {
			return false, err
		}
		for _, rec := range recs {
			var meta struct {
				Env []string `json:"env"`
			}
			if err := json.Unmarshal(rec.Metadata, &meta); err != nil {
				return false, err
			}
			for _, tag := range meta.Env {
				if tag == name {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
