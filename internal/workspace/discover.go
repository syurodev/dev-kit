// Package workspace discovers the services a workspace root holds.
//
// A leaf: it only reads a directory tree, nothing else devkit knows about.
// See docs/dev-kit/workspace.mdx § Auto-discovery for the design this
// implements (step 1, "tìm service" — step 2's config-based naming is not
// built yet, so every discovered service is named after its folder).
package workspace

import (
	"os"
	"path/filepath"
)

// DiscoveredService is one (name, path) pair a workspace root resolved to,
// before it has an ID or a workspace to belong to — see WorkspaceService for
// the persisted shape.
type DiscoveredService struct {
	Name string
	Path string
}

// DiscoverServices reads root at depth 1 and keeps every entry that is a git
// checkout — a plain repo (".git" directory) or a linked worktree (".git" is
// a file there, pointing at the primary repo). No recursion: a nested repo
// two levels down is not this workspace's concern, and a full-tree walk is
// the one thing that could make opening a large root slow.
//
// A root with no such entries is not an error — it is a single-service
// workspace. The caller picked *some* folder; if nothing inside it looks
// like a second repo, the folder itself is the one service, same as if they
// had picked a repo directly. This is what keeps "add workspace" a single
// action regardless of whether the folder holds one project or ten.
func DiscoverServices(root string) ([]DiscoveredService, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var services []DiscoveredService
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !hasGitCheckout(path) {
			continue
		}
		services = append(services, DiscoveredService{Name: entry.Name(), Path: path})
	}

	if len(services) == 0 {
		return []DiscoveredService{{Name: filepath.Base(root), Path: root}}, nil
	}
	return services, nil
}

// hasGitCheckout reports whether dir/.git exists as either a directory (a
// normal repo) or a regular file (a linked worktree — see layout.mdx's note
// on worktree .git files pointing at <primary>/.git/worktrees/<name>).
func hasGitCheckout(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}
