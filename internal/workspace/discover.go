// Package workspace discovers the services a workspace root holds.
//
// A leaf: it only reads a directory tree, nothing else devkit knows about.
// See docs/dev-kit/workspace.mdx § Auto-discovery for the design this
// implements: step 1 ("tìm service") here, step 2 (the name a service's own
// config gives it) in springconfig.go.
package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoveredService is one (name, path) pair a workspace root resolved to,
// before it has an ID or a workspace to belong to — see WorkspaceService for
// the persisted shape.
type DiscoveredService struct {
	Name string
	Path string
}

// maxDiscoveryDepth is how far below root DiscoverServices looks for a
// repository. 2 covers the grouped layout (root/backend/billing,
// root/frontend/web) without turning into a tree walk: a directory is only
// ever descended into when it is NOT a repository itself, so the cost is one
// readdir per plain directory at depth 1, and the search never enters a repo.
const maxDiscoveryDepth = 2

// skipDiscoveryDirs are never descended into looking for repositories: build
// output and dependency caches, which are large, never hold a service of
// their own, and are exactly what could make opening a root slow. They are
// only skipped as a place to look — a repository that happens to be called
// "build" is still found, since it is recognised before this list applies.
var skipDiscoveryDirs = map[string]bool{
	"node_modules": true,
	"target":       true,
	"build":        true,
	"dist":         true,
	"out":          true,
	"vendor":       true,
}

// DiscoverServices finds every git checkout under root, down to
// maxDiscoveryDepth — a plain repo (".git" directory) or a linked worktree
// (".git" is a file there, pointing at the primary repo). Search stops at
// the first repository on each branch: a repository nested inside another
// one (a submodule, a vendored checkout) is that repository's business, not
// a second service of this workspace. Hidden directories and
// skipDiscoveryDirs are not searched.
//
// When root is itself a checkout, only its direct children are looked at,
// as before depth 2 existed: a single repository is the common case, and
// searching two levels into it would turn a vendored checkout under, say,
// third_party/ into a service and drop the repository the user picked.
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

	depth := maxDiscoveryDepth
	if hasGitCheckout(root) {
		depth = 1
	}
	var services []DiscoveredService
	discoverIn(root, entries, depth, &services)

	if len(services) == 0 {
		return []DiscoveredService{{Name: filepath.Base(root), Path: root}}, nil
	}
	return services, nil
}

// discoverIn appends every checkout among dir's entries, descending into the
// ones that are not checkouts while depth allows. os.ReadDir returns entries
// sorted, so the result order is stable.
func discoverIn(dir string, entries []os.DirEntry, depth int, out *[]DiscoveredService) {
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if hasGitCheckout(path) {
			*out = append(*out, DiscoveredService{Name: entry.Name(), Path: path})
			continue
		}
		if depth <= 1 || strings.HasPrefix(entry.Name(), ".") || skipDiscoveryDirs[entry.Name()] {
			continue
		}
		children, err := os.ReadDir(path)
		if err != nil {
			// An unreadable directory is a place with no services in it, not
			// a reason to fail the whole workspace.
			continue
		}
		discoverIn(path, children, depth-1, out)
	}
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
