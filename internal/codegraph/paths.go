// internal/codegraph/paths.go
package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// ProjectDBPath derives the on-disk SQLite index path for a project folder.
// Same convention as internal/cloudconfig and internal/mcp: callers pass the
// raw OS config dir (os.UserConfigDir()), this appends "DevKit" and its own
// subpath. The project path is hashed rather than used as a filename since
// it may contain characters invalid in a path segment (and to keep the
// resulting filename length-bounded regardless of how deep the project
// folder is nested).
func ProjectDBPath(configDir, projectPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(projectPath)))
	name := hex.EncodeToString(sum[:]) + ".db"
	return filepath.Join(configDir, "DevKit", "codegraph", name)
}
