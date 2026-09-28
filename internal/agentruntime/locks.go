package agentruntime

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

// The tested releases carry their dependency graphs before installation.
// Experimental versions remain explicitly untested and use npm resolution.
//
//go:embed locks/*/package.json locks/*/package-lock.json
var runtimeLocks embed.FS

func prepareRuntimeLock(agent Agent, version, dir string) (bool, error) {
	if version != agent.TestedVersion {
		return false, nil
	}
	for _, name := range []string{"package.json", "package-lock.json"} {
		body, err := runtimeLocks.ReadFile("locks/" + agent.ID + "/" + name)
		if err != nil {
			return false, fmt.Errorf("missing runtime lock: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			return false, err
		}
	}
	return true, nil
}
