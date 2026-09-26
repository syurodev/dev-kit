package workspace

import (
	"path/filepath"
	"strings"
)

// SessionOwns reports whether a session (identified by its working directory
// and granted read-write paths) "owns" the given set of workspace services:
// true when any service is covered by the granted paths or the session's
// working directory is within a service path.
func SessionOwns(services []WorkspaceService, cwd string, granted []string) bool {
	for _, svc := range services {
		if Covered(svc.Path, granted) || Within(cwd, svc.Path) {
			return true
		}
	}
	return false
}

// Covered reports whether path is one of roots or inside one.
func Covered(path string, roots []string) bool {
	for _, root := range roots {
		if root != "" && Within(path, root) {
			return true
		}
	}
	return false
}

// Within reports whether path is root or inside it — a prefix of a folder
// name (kong-plugin-v2 against kong-plugin) is not.
func Within(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
