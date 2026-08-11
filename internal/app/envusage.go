package app

import (
	"encoding/json"

	"devkit/internal/vaultitem"
)

// profileTypesWithEnv are vaultitem record types that store metadata.env and
// must block Environments.DeleteTag while any profile still references the tag.
var profileTypesWithEnv = []string{"ssh-profile", "db-profile", "kafka-profile"}

type itemLister interface {
	ListByType(itemType string) ([]vaultitem.Record, error)
}

// vaultItemEnvUsage satisfies environments.ProfileEnvUsage by scanning
// plaintext profile metadata for a matching env name.
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
	return false, nil
}
