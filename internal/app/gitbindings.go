package app

import (
	"encoding/json"
	"sort"
	"strings"

	gitservice "devkit/internal/git"
	"devkit/internal/vaultitem"
)

// GitBindings stores what the Git module knows about a repository but cannot
// hold itself: which branches are protected, and which account authenticates
// to which remote.
//
// It lives here rather than in internal/git because those are vault items and
// git must not import vaultitem — the module declares GuardStore and
// RemoteStore as interfaces so the composition root decides what satisfies
// them. Before this existed both interfaces were nil in production, which made
// two designed behaviours unreachable: commit gating never triggered, and a
// push never resolved a credential.
type GitBindings struct {
	items gitItemStore
}

// gitItemStore is the slice of vaultitem.Store used here.
type gitItemStore interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	ListByTypeAndMetaField(itemType, field, value string) ([]vaultitem.Record, error)
	Delete(id string) error
}

// NewGitBindings builds the store.
func NewGitBindings(items gitItemStore) *GitBindings {
	return &GitBindings{items: items}
}

type guardMetadata struct {
	ProjectKey string `json:"project_key"`
	Pattern    string `json:"pattern"`
	Env        string `json:"env"`
}

type remoteMetadata struct {
	ProjectKey string `json:"project_key"`
	Name       string `json:"name"`
	AccountID  string `json:"account_id,omitempty"`
	Env        string `json:"env,omitempty"`
}

// ListGuards satisfies gitservice.GuardStore.
func (b *GitBindings) ListGuards(projectKey string) ([]gitservice.Guard, error) {
	if b == nil || b.items == nil || projectKey == "" {
		return nil, nil
	}
	records, err := b.items.ListByTypeAndMetaField(vaultitem.ItemTypeGitGuard, "project_key", projectKey)
	if err != nil {
		return nil, err
	}
	guards := make([]gitservice.Guard, 0, len(records))
	for _, record := range records {
		var meta guardMetadata
		if json.Unmarshal(record.Metadata, &meta) != nil {
			continue
		}
		guards = append(guards, gitservice.Guard{
			ID: record.ID, ProjectKey: meta.ProjectKey, Pattern: meta.Pattern, Env: meta.Env,
		})
	}
	// Longest pattern first, so an exact branch name beats a glob that also
	// covers it and the more specific rule is the one that applies.
	sort.SliceStable(guards, func(i, j int) bool {
		return len(guards[i].Pattern) > len(guards[j].Pattern)
	})
	return guards, nil
}

// CreateGuard protects a branch pattern with an environment tag.
func (b *GitBindings) CreateGuard(projectKey, pattern, env string) (gitservice.Guard, error) {
	if projectKey == "" || strings.TrimSpace(pattern) == "" || env == "" {
		return gitservice.Guard{}, gitservice.ErrNotAllowed
	}
	raw, err := json.Marshal(guardMetadata{ProjectKey: projectKey, Pattern: pattern, Env: env})
	if err != nil {
		return gitservice.Guard{}, err
	}
	// A guard holds no secret, but a vault item must carry a valid envelope,
	// so it seals an empty one rather than becoming a special case in sync.
	record, err := b.items.Create(vaultitem.ItemTypeGitGuard, "git-guard", raw, []byte{})
	if err != nil {
		return gitservice.Guard{}, err
	}
	return gitservice.Guard{ID: record.ID, ProjectKey: projectKey, Pattern: pattern, Env: env}, nil
}

func (b *GitBindings) DeleteGuard(id string) error { return b.items.Delete(id) }

// ListRemotes satisfies gitservice.RemoteStore.
func (b *GitBindings) ListRemotes(projectKey string) ([]gitservice.Remote, error) {
	if b == nil || b.items == nil || projectKey == "" {
		return nil, nil
	}
	records, err := b.items.ListByTypeAndMetaField(vaultitem.ItemTypeGitRemote, "project_key", projectKey)
	if err != nil {
		return nil, err
	}
	remotes := make([]gitservice.Remote, 0, len(records))
	for _, record := range records {
		var meta remoteMetadata
		if json.Unmarshal(record.Metadata, &meta) != nil {
			continue
		}
		remotes = append(remotes, gitservice.Remote{
			Name: meta.Name, AccountID: meta.AccountID, Env: meta.Env,
		})
	}
	return remotes, nil
}

// BindRemote points a remote at an account and an environment tag. Passing an
// empty account clears the binding, which is how a repository goes back to
// whatever git's own configuration already does.
func (b *GitBindings) BindRemote(projectKey, name, accountID, env string) (gitservice.Remote, error) {
	if projectKey == "" || strings.TrimSpace(name) == "" {
		return gitservice.Remote{}, gitservice.ErrNotAllowed
	}
	// One binding per remote: two would make "which account authenticates to
	// origin" ambiguous, so an existing one is replaced rather than added to.
	existing, err := b.items.ListByTypeAndMetaField(vaultitem.ItemTypeGitRemote, "project_key", projectKey)
	if err != nil {
		return gitservice.Remote{}, err
	}
	for _, record := range existing {
		var meta remoteMetadata
		if json.Unmarshal(record.Metadata, &meta) == nil && meta.Name == name {
			if err := b.items.Delete(record.ID); err != nil {
				return gitservice.Remote{}, err
			}
		}
	}
	raw, err := json.Marshal(remoteMetadata{
		ProjectKey: projectKey, Name: name, AccountID: accountID, Env: env,
	})
	if err != nil {
		return gitservice.Remote{}, err
	}
	if _, err := b.items.Create(vaultitem.ItemTypeGitRemote, "git-remote", raw, []byte{}); err != nil {
		return gitservice.Remote{}, err
	}
	return gitservice.Remote{Name: name, AccountID: accountID, Env: env}, nil
}

// UsedBy satisfies accounts.Usage: a remote binding names an account, so
// deleting that account has to say which remote still needs it.
func (b *GitBindings) UsedBy(accountID string) []string {
	if b == nil || b.items == nil || accountID == "" {
		return nil
	}
	records, err := b.items.ListByType(vaultitem.ItemTypeGitRemote)
	if err != nil {
		return nil
	}
	var out []string
	for _, record := range records {
		var meta remoteMetadata
		if json.Unmarshal(record.Metadata, &meta) == nil && meta.AccountID == accountID {
			out = append(out, "git remote "+meta.Name)
		}
	}
	return out
}

// GuardEnvInUse reports whether a guard or a remote binding still names an
// environment tag, so DeleteTag cannot pull one out from under a gate.
func (b *GitBindings) GuardEnvInUse(name string) (bool, error) {
	if b == nil || b.items == nil || name == "" {
		return false, nil
	}
	for _, itemType := range []string{vaultitem.ItemTypeGitGuard, vaultitem.ItemTypeGitRemote} {
		records, err := b.items.ListByType(itemType)
		if err != nil {
			return false, err
		}
		for _, record := range records {
			var meta struct {
				Env string `json:"env"`
			}
			if json.Unmarshal(record.Metadata, &meta) == nil && meta.Env == name {
				return true, nil
			}
		}
	}
	return false, nil
}
