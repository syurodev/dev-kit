package agenthub_test

import (
	"slices"
	"testing"

	"devkit/internal/agenthub"
)

// carveOutAll is lexical: the accounts folder handed to Service.AccountsDir must be
// one of the deny entries ProtectDataDir builds from the same data directory, or an
// account's own home stays hidden from its session (or a sibling's is exposed).
func TestAccountsDirIsAnEntryOfTheProtectedList(t *testing.T) {
	for _, dataDir := range []string{t.TempDir(), "/data/DevKit", "/data/DevKit/../DevKit"} {
		dir := agenthub.AccountsDirOf(dataDir)
		if dir == "" {
			t.Fatalf("AccountsDirOf(%q) is empty", dataDir)
		}
		if !slices.Contains(agenthub.ProtectDataDir(dataDir, "runtimes"), dir) {
			t.Errorf("AccountsDirOf(%q) = %q is not in ProtectDataDir's list", dataDir, dir)
		}
	}
	if agenthub.AccountsDirOf("") != "" {
		t.Error("no data directory, no accounts directory")
	}
}
