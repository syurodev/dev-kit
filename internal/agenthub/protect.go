package agenthub

import (
	"os"
	"path/filepath"
	"slices"
)

// dataDirSecrets are the entries of DevKit's data directory hidden even when
// they do not exist yet at session open: Seatbelt denies a path that is not
// there, and the database's WAL files come and go.
var dataDirSecrets = []string{
	"devkit.db", "devkit.db-wal", "devkit.db-shm", // the vault, sealed but offline-attackable
	"mcp.sock",    // the shared MCP socket, which carries no session identity
	"agents",      // every session's own MCP socket
	"checkpoints", // shadow copies of every project checkpointed
	"consoles",    // saved query consoles
}

// ProtectDataDir is what to hide of DevKit's data directory from an agent
// session: everything in it except keep. Not the directory itself, because
// what is kept — the agent runtimes DevKit installs there — has to stay
// reachable, and a session's own MCP socket inside "agents" is granted back
// on its own (sandbox.Policy lets a socket sit under a caller's deny path).
//
// With a private $HOME none of this is visible anyway; it matters once home
// is shared, which is the default.
func ProtectDataDir(dataDir string, keep ...string) []string {
	if dataDir == "" {
		return nil
	}
	names := append([]string(nil), dataDirSecrets...)
	if entries, err := os.ReadDir(dataDir); err == nil {
		for _, entry := range entries {
			if !slices.Contains(names, entry.Name()) {
				names = append(names, entry.Name())
			}
		}
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if slices.Contains(keep, name) {
			continue
		}
		out = append(out, filepath.Join(dataDir, name))
	}
	return out
}
